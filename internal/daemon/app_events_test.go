package daemon

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/events"
)

func drain(s *events.Subscription) []events.Event {
	var out []events.Event
	for {
		select {
		case e := <-s.C:
			out = append(out, e)
		default:
			return out
		}
	}
}

func topics(es []events.Event) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Topic
	}
	return out
}

func TestTaskEventsFollowStatusTransitions(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe("*", 64)
	defer sub.Close()
	te := newTaskEvents(bus, nil)
	s := agents.Session{ID: "t1", Title: "Fix login", Status: agents.StatusWorking, Task: "fix it", Chain: []string{"auto-1"}}

	te.created(s)
	te.observe("agents.updated", s) // same status: nothing new
	s.Status = agents.StatusNeedsYou
	s.Pending = []agents.PermissionRequest{{}}
	te.observe("agents.updated", s)
	te.observe("agents.updated", s) // repeated publish of the same state
	s.Status = agents.StatusDone
	te.finished(s)
	te.observe("agents.updated", s)
	s.PRURL, s.PRNumber = "https://github.com/o/r/pull/3", 3
	te.observe("agents.updated", s)
	te.observe("agents.updated", s)

	got := drain(sub)
	want := []string{events.TopicTaskCreated, events.TopicTaskStatus, events.TopicAgentWaiting, events.TopicAgentFinished, events.TopicTaskStatus, events.TopicPROpened}
	if len(got) != len(want) {
		t.Fatalf("topics = %v, want %v", topics(got), want)
	}
	for i := range want {
		if got[i].Topic != want[i] {
			t.Fatalf("topics = %v, want %v", topics(got), want)
		}
		if len(got[i].Chain) != 1 || got[i].Chain[0] != "auto-1" {
			t.Fatalf("%s lost provenance: %v", got[i].Topic, got[i].Chain)
		}
	}
	if got[0].Data["task"] != "fix it" || got[1].Data["from"] != "working" || got[1].Data["status"] != "needs-you" {
		t.Fatalf("payloads: %v / %v", got[0].Data, got[1].Data)
	}
	if got[2].Data["pending"] != 1 || got[5].Data["prNumber"] != 3 {
		t.Fatalf("waiting/pr payloads: %v / %v", got[2].Data, got[5].Data)
	}
}

func TestTaskEventsUnseenSessionReportsWaitingAndFailureOnly(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe("*", 64)
	defer sub.Close()
	te := newTaskEvents(bus, nil)
	// A session from before this daemon started: no baseline, so no status-change event,
	// but a session that is failing or waiting still reports it.
	te.observe("agents.updated", agents.Session{ID: "old", Status: agents.StatusFailed, Error: "boom", PRURL: "https://x"})
	te.observe("agents.updated", agents.Session{ID: "old2", Status: agents.StatusIdle})
	te.observe("agents.removed", map[string]string{"id": "old"})
	te.observe("agents.updated", agents.Session{ID: "old", Status: agents.StatusFailed})
	got := drain(sub)
	if len(got) != 2 || got[0].Topic != events.TopicAgentFailed || got[0].Data["error"] != "boom" || got[1].Topic != events.TopicAgentFailed {
		t.Fatalf("got %v", topics(got))
	}
}

func TestTaskEventsReportOwnedTasksToTheirBot(t *testing.T) {
	var got []string
	te := newTaskEvents(nil, func(topic string, s agents.Session) { got = append(got, topic+" "+s.ID) })
	mine := agents.Session{ID: "mine", Owner: "bot:b1", Status: agents.StatusWorking}
	user := agents.Session{ID: "user", Status: agents.StatusWorking}
	for _, s := range []agents.Session{mine, user} {
		te.created(s)
		s.Status = agents.StatusNeedsYou
		te.observe("agents.updated", s)
		s.Status = agents.StatusDone
		te.finished(s)
		te.observe("agents.updated", s)
		s.Status = agents.StatusFailed
		te.observe("agents.updated", s)
	}
	want := []string{"agent.waiting mine", "agent.finished mine", "agent.failed mine"}
	if !slices.Equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}

// A completed turn settles before it counts as finished: a session that is working again by
// then (the harness delivering background results) reports nothing; the later turn does.
func TestTaskEventsFinishedWaitsForTheTaskToSettle(t *testing.T) {
	var mu sync.Mutex
	var got []string
	te := newTaskEvents(nil, func(topic string, s agents.Session) {
		mu.Lock()
		got = append(got, topic+" "+s.Result)
		mu.Unlock()
	})
	te.settle = 20 * time.Millisecond
	current := agents.Session{ID: "mine", Owner: "bot:b1", Status: agents.StatusWorking, Subagents: 1}
	te.lookup = func(string) (agents.Session, error) {
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}
	first := current
	first.Status, first.Result = agents.StatusDone, "progress"
	te.finished(first) // the session is working again (lookup) by the time the settle fires
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("unsettled turn reported: %v", got)
	}
	current.Status, current.Subagents, current.Result = agents.StatusDone, 0, "final"
	mu.Unlock()
	te.finished(current)
	te.finished(current) // a repeated completion coalesces into one settle
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(got, []string{"agent.finished final"}) {
		t.Fatalf("reports = %v", got)
	}
	failed := current
	failed.Status = agents.StatusFailed
	te.finished(failed) // failed turns are agent.failed through observe, never agent.finished
	if len(got) != 1 {
		t.Fatalf("failed turn reported as finished: %v", got)
	}
}

// A finished task's report carries its complete result; status reports keep the last message.
func TestTaskReportTextCarriesTheWholeResult(t *testing.T) {
	long := strings.Repeat("section ", 1000) // 8000 runes: past the event clip, within the bot bound
	s := agents.Session{ID: "t", Title: "Research", Kind: "omp", Status: agents.StatusDone, Turns: 3, Result: long, LastMessage: "last"}
	text := taskReportText("agent.finished", s)
	if !strings.Contains(text, long) || !strings.Contains(text, "Result (complete)") || !strings.Contains(text, "3 turn(s) completed") || strings.Contains(text, "…") {
		t.Fatalf("finished report clipped or mislabelled: %.200s", text)
	}
	s.Status = agents.StatusNeedsYou
	s.Questions = []agents.QuestionRequest{{Questions: []agents.Question{{Prompt: "Which repo?"}}}}
	text = taskReportText("agent.waiting", s)
	if !strings.Contains(text, "Waiting for:\nquestion: Which repo?") || !strings.Contains(text, "Last message:\nlast") || strings.Contains(text, long) {
		t.Fatalf("waiting report: %.200s", text)
	}
	if info := taskInfo(s, true); info.Result != long || info.Turns != 3 {
		t.Fatalf("task_status clipped the result: %d runes", len([]rune(info.Result)))
	}
	if info := taskInfo(s, false); len([]rune(info.Result)) > appEventText+1 {
		t.Fatalf("list entry not clipped: %d runes", len([]rune(info.Result)))
	}
}

func TestBotRepliesPublishOnceForCompletedAssistantReplies(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe(events.TopicBotMessage, 64)
	defer sub.Close()
	b := newBotReplies(bus)
	text := []chat.MsgPart{{Type: chat.PartText, Text: "All done"}}
	b.observe("chat.message", chat.Message{ID: "m1", BotID: "b", Role: "assistant", Status: chat.StatusRunning, Parts: text})
	b.observe("chat.message", chat.Message{ID: "m1", BotID: "b", Role: "assistant", Status: chat.StatusComplete, Parts: text})
	b.observe("chat.message", chat.Message{ID: "m1", BotID: "b", Role: "assistant", Status: chat.StatusComplete, Parts: text})
	b.observe("chat.message", chat.Message{ID: "u1", BotID: "b", Role: "user", Status: chat.StatusComplete, Parts: text})
	b.observe("chat.message", chat.Message{ID: "d1", BotID: "draft", Role: "assistant", Author: "bot:draft", Status: chat.StatusComplete, Parts: text})
	b.observe("chat.message.delta", chat.Message{ID: "m2", Role: "assistant", Status: chat.StatusComplete})
	got := drain(sub)
	if len(got) != 1 || got[0].Data["botId"] != "b" || got[0].Data["messageId"] != "m1" || got[0].Data["text"] != "All done" {
		t.Fatalf("got %+v", got)
	}
}

func TestBotRepliesKeepTheAutomationChain(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe(events.TopicBotMessage, 8)
	defer sub.Close()
	text := []chat.MsgPart{{Type: chat.PartText, Text: "ok"}}
	newBotReplies(bus).observe("chat.message", chat.Message{ID: "m1", BotID: "b", Role: "assistant", Status: chat.StatusComplete, Parts: text, Automations: []string{"a1"}})
	got := drain(sub)
	if len(got) != 1 || !slices.Equal(got[0].Chain, []string{"a1"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestEveryCatalogTopicIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, topic := range events.Catalog() {
		if seen[topic.Topic] || !events.ValidPattern(topic.Topic) || topic.Label == "" {
			t.Fatalf("bad topic %+v", topic)
		}
		seen[topic.Topic] = true
	}
}

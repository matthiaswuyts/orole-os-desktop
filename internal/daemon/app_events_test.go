package daemon

import (
	"slices"
	"testing"

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

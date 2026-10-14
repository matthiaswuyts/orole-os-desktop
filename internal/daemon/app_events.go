package daemon

import (
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/projects"
)

// appEventText bounds free text copied into an event. Events are facts for filters
// and templates, not transcripts; the full text stays with the task or message.
const appEventText = 2000

// taskEvents turns the agent runtime's client updates into application events on the
// bus: status transitions, waiting for the user, failure and a newly opened pull
// request. It remembers only the last status and PR URL per session it has seen.
// agent.finished, agent.waiting and agent.failed also go to owned, for sessions a bot
// started; that path does not depend on the bus, which may drop events.
//
// agent.finished is published only once a completed turn has settled: the runtime reports
// each turn's end, but a harness that delivers subagent or background results starts the
// next turn right after, so the event waits settle and is dropped when the session is
// working again by then (the later turn's end publishes instead).
type taskEvents struct {
	bus     *events.Bus
	owned   func(topic string, s agents.Session)
	notices *appNotices                             // notification center; nil reports nothing
	settle  time.Duration                           // 0 publishes agent.finished at once
	lookup  func(id string) (agents.Session, error) // current state at settle time; nil uses the reported one
	mu      sync.Mutex
	seen    map[string]taskMark
	timers  map[string]*time.Timer // pending settles per session
}

type taskMark struct {
	status agents.Status
	prURL  string
}

func newTaskEvents(bus *events.Bus, owned func(topic string, s agents.Session)) *taskEvents {
	return &taskEvents{bus: bus, owned: owned, seen: map[string]taskMark{}, timers: map[string]*time.Timer{}}
}

// observe inspects one agent runtime client event. It never blocks: the bus drops
// events for a full subscriber rather than wait.
func (t *taskEvents) observe(event string, data any) {
	if t == nil {
		return
	}
	switch event {
	case "agents.removed":
		if m, ok := data.(map[string]string); ok {
			t.mu.Lock()
			delete(t.seen, m["id"])
			if timer := t.timers[m["id"]]; timer != nil {
				timer.Stop()
				delete(t.timers, m["id"])
			}
			t.mu.Unlock()
			t.notices.taskResolved(m["id"])
		}
		return
	case "agents.updated":
	default:
		return
	}
	s, ok := data.(agents.Session)
	if !ok {
		return
	}
	t.mu.Lock()
	prev, known := t.seen[s.ID]
	t.seen[s.ID] = taskMark{status: s.Status, prURL: s.PRURL}
	t.mu.Unlock()

	if known && prev.status != s.Status {
		d := taskData(s)
		d["from"] = string(prev.status)
		t.publish(events.TopicTaskStatus, s, d)
	}
	if prev.status != s.Status {
		if prev.status == agents.StatusNeedsYou {
			t.notices.taskResolved(s.ID)
		}
		switch s.Status {
		case agents.StatusNeedsYou:
			d := taskData(s)
			d["pending"], d["questions"] = len(s.Pending), len(s.Questions)
			t.publish(events.TopicAgentWaiting, s, d)
		case agents.StatusFailed:
			t.publish(events.TopicAgentFailed, s, taskData(s))
		}
	}
	if known && prev.prURL == "" && s.PRURL != "" {
		d := taskData(s)
		d["prNumber"] = s.PRNumber
		t.publish(events.TopicPROpened, s, d)
	}
}

// created publishes task.created and starts tracking the session's status.
func (t *taskEvents) created(s agents.Session) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.seen[s.ID] = taskMark{status: s.Status, prURL: s.PRURL}
	t.mu.Unlock()
	d := taskData(s)
	d["task"] = clip(s.Task)
	t.publish(events.TopicTaskCreated, s, d)
}

// finished publishes agent.finished for a completed turn once it has settled (see taskEvents).
// A failed turn is not finished: observe already published agent.failed for it.
func (t *taskEvents) finished(s agents.Session) {
	if t == nil || s.Status != agents.StatusDone {
		return
	}
	if t.settle <= 0 {
		t.publishFinished(s)
		return
	}
	t.mu.Lock()
	if old := t.timers[s.ID]; old != nil {
		old.Stop()
	}
	t.timers[s.ID] = time.AfterFunc(t.settle, func() {
		t.mu.Lock()
		delete(t.timers, s.ID)
		t.mu.Unlock()
		if t.lookup != nil {
			if cur, err := t.lookup(s.ID); err == nil {
				s = cur
			}
		}
		if s.Status == agents.StatusDone && s.Subagents == 0 {
			t.publishFinished(s)
		}
	})
	t.mu.Unlock()
}

func (t *taskEvents) publishFinished(s agents.Session) {
	d := taskData(s)
	d["result"] = clip(s.Result)
	t.publish(events.TopicAgentFinished, s, d)
}

func (t *taskEvents) publish(topic string, s agents.Session, data map[string]any) {
	if t.bus != nil {
		t.bus.Publish(events.Event{Topic: topic, Data: data, Chain: s.Chain})
	}
	if topic == events.TopicAgentFinished || topic == events.TopicAgentWaiting || topic == events.TopicAgentFailed {
		t.notices.task(topic, s)
		if t.owned != nil && s.Owner != "" {
			t.owned(topic, s)
		}
	}
}

// taskData is the payload every task.* and agent.* topic shares (events.Catalog).
func taskData(s agents.Session) map[string]any {
	return map[string]any{
		"id": s.ID, "title": s.Title, "kind": s.Kind, "status": string(s.Status),
		"projectId": s.ProjectID, "project": s.Project, "branch": s.Branch,
		"activity": clip(s.Activity), "lastMessage": clip(s.LastMessage), "error": clip(s.Error),
		"prUrl": s.PRURL,
	}
}

// projectCreated publishes project.created for a project added or created by the user.
func projectCreated(bus *events.Bus, p projects.Project, chain []string) {
	if bus == nil {
		return
	}
	bus.Publish(events.Event{Topic: events.TopicProjectCreated, Chain: chain, Data: map[string]any{
		"id": p.ID, "name": p.Name, "path": p.Path, "git": p.IsGit,
	}})
}

// botReplies publishes bot.message once per completed assistant reply. The chat runtime
// republishes a message as it streams and checkpoints, so completed IDs are remembered in
// a small ring.
type botReplies struct {
	bus  *events.Bus
	mu   sync.Mutex
	done recentIDs
}

func newBotReplies(bus *events.Bus) *botReplies {
	return &botReplies{bus: bus, done: newRecentIDs(512)}
}

func (b *botReplies) observe(event string, payload any) {
	if b == nil || b.bus == nil || event != "chat.message" {
		return
	}
	m, ok := payload.(chat.Message)
	// Drafts are the bot builder's preview conversation, not a bot's reply.
	if !ok || m.Role != "assistant" || m.Status != chat.StatusComplete || m.Author == "bot:draft" {
		return
	}
	b.mu.Lock()
	fresh := b.done.add(m.ID)
	b.mu.Unlock()
	if !fresh {
		return
	}
	// The chain bounds flows triggered by this reply exactly like the flow that caused it.
	b.bus.Publish(events.Event{Topic: events.TopicBotMessage, Chain: m.Automations, Data: map[string]any{
		"botId": m.BotID, "messageId": m.ID, "text": clip(m.Text()),
	}})
}

// clip bounds s to appEventText runes.
func clip(s string) string { return clipTo(s, appEventText) }

// clipTo bounds s to max runes, marking the cut.
func clipTo(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

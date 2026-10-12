package daemon

import (
	"sync"
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
type taskEvents struct {
	bus  *events.Bus
	mu   sync.Mutex
	seen map[string]taskMark
}

type taskMark struct {
	status agents.Status
	prURL  string
}

func newTaskEvents(bus *events.Bus) *taskEvents {
	return &taskEvents{bus: bus, seen: map[string]taskMark{}}
}

// observe inspects one agent runtime client event. It never blocks: the bus drops
// events for a full subscriber rather than wait.
func (t *taskEvents) observe(event string, data any) {
	if t == nil || t.bus == nil {
		return
	}
	switch event {
	case "agents.removed":
		if m, ok := data.(map[string]string); ok {
			t.mu.Lock()
			delete(t.seen, m["id"])
			t.mu.Unlock()
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
	if t == nil || t.bus == nil {
		return
	}
	t.mu.Lock()
	t.seen[s.ID] = taskMark{status: s.Status, prURL: s.PRURL}
	t.mu.Unlock()
	d := taskData(s)
	d["task"] = clip(s.Task)
	t.publish(events.TopicTaskCreated, s, d)
}

// finished publishes agent.finished for a completed turn.
func (t *taskEvents) finished(s agents.Session) {
	if t == nil || t.bus == nil {
		return
	}
	d := taskData(s)
	d["result"] = clip(s.Result)
	t.publish(events.TopicAgentFinished, s, d)
}

func (t *taskEvents) publish(topic string, s agents.Session, data map[string]any) {
	t.bus.Publish(events.Event{Topic: topic, Data: data, Chain: s.Chain})
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
	done map[string]struct{}
	ring []string
	next int
}

func newBotReplies(bus *events.Bus) *botReplies {
	return &botReplies{bus: bus, done: map[string]struct{}{}, ring: make([]string, 512)}
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
	if _, dup := b.done[m.ID]; dup {
		b.mu.Unlock()
		return
	}
	if old := b.ring[b.next]; old != "" {
		delete(b.done, old)
	}
	b.ring[b.next] = m.ID
	b.next = (b.next + 1) % len(b.ring)
	b.done[m.ID] = struct{}{}
	b.mu.Unlock()
	b.bus.Publish(events.Event{Topic: events.TopicBotMessage, Data: map[string]any{
		"botId": m.BotID, "messageId": m.ID, "text": clip(m.Text()),
	}})
}

// clip bounds s to appEventText runes.
func clip(s string) string {
	if utf8.RuneCountInString(s) <= appEventText {
		return s
	}
	r := []rune(s)
	return string(r[:appEventText]) + "…"
}

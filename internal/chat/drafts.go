package chat

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxDraftTurns = 100
	draftPrefix   = "draft:"
)

var draftIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// DraftSend runs an unsaved bot configuration against one message (bot-builder preview).
//
// Nothing is stored: the exchange exists only as chat.message events with BotID "draft:<DraftID>",
// the returned message is the (unstored) user message, and the assistant reply streams as events.
// A draft has no identity, so persistence is off: the memory snapshot and recall are not used, the
// memory tools (memory writes, search_history, ask_bot, propose_skill, ...) are not offered, and the
// RunContext handed to the remaining tools carries an empty Bot.ID and BotID. Cancel the draft run
// with Cancel("draft:<DraftID>").
func (m *Manager) DraftSend(ctx context.Context, req DraftRequest) (Message, error) {
	if !draftIDRe.MatchString(req.DraftID) {
		return Message{}, invalidf("The preview id is not valid.")
	}
	if strings.TrimSpace(req.Text) == "" || !utf8.ValidString(req.Text) {
		return Message{}, invalidf("Write a message to preview.")
	}
	if len(req.Text) > MaxMessageTextLen {
		return Message{}, invalidf("Messages are limited to %d KiB of text.", MaxMessageTextLen>>10)
	}
	if len(req.History) > maxDraftTurns {
		return Message{}, invalidf("The preview history is too long.")
	}
	history := make([]Message, 0, len(req.History))
	for _, t := range req.History {
		if (t.Role != "user" && t.Role != "assistant") || len(t.Text) > MaxMessageTextLen || !utf8.ValidString(t.Text) {
			return Message{}, invalidf("The preview history is not valid.")
		}
		history = append(history, Message{Role: t.Role, Status: StatusComplete, Parts: []MsgPart{{Type: PartText, Text: t.Text}}})
	}
	in := req.Bot
	in.ID = ""
	in, err := m.store.validate(ctx, in)
	if err != nil {
		return Message{}, err
	}
	bot := Bot{
		Name: in.Name, Avatar: in.Avatar, Color: in.Color, Description: in.Description, Instructions: in.Instructions,
		Provider: in.Provider, Model: in.Model, Reasoning: in.Reasoning, Config: in.Config,
	}

	key := draftPrefix + req.DraftID
	runCtx, cancel := context.WithCancel(m.ctx)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		return Message{}, ErrClosed
	}
	if m.runs[key] != nil {
		m.mu.Unlock()
		cancel()
		return Message{}, busyf("The preview is still replying.")
	}
	st := &runState{cancel: cancel, done: make(chan struct{})}
	m.runs[key] = st
	m.wg.Add(1)
	m.mu.Unlock()

	now := m.nowMs()
	user := Message{ID: newID(), BotID: key, Role: "user", Author: "user", Status: StatusComplete, CreatedAt: now,
		Parts: []MsgPart{{Type: PartText, Text: req.Text}}}
	m.publishMessage(user)
	go func() {
		defer m.wg.Done()
		defer func() {
			cancel()
			m.mu.Lock()
			delete(m.runs, key)
			close(st.done)
			m.mu.Unlock()
			m.emit("chat.run", RunState{BotID: key})
		}()
		asst := Message{ID: newID(), BotID: key, Role: "assistant", Author: "bot:draft", Status: StatusRunning, CreatedAt: m.nowMs(), Parts: []MsgPart{}}
		s := newSink(asst, nil, m.publishMessage)
		m.emit("chat.run", RunState{BotID: key, Running: true, MessageID: asst.ID})
		s.checkpoint()
		rc := RunContext{Bot: bot, MessageID: asst.ID, Actor: "bot:draft", Emit: s.upsertTool}
		status, errText := m.runDraft(runCtx, bot, rc, history, req.Text, s)
		if status == StatusCanceled && m.ctx.Err() != nil {
			status = StatusIncomplete
		}
		_ = s.finish(status, errText)
	}()
	return user, nil
}

func (m *Manager) runDraft(ctx context.Context, bot Bot, rc RunContext, history []Message, text string, s *sink) (string, string) {
	env, err := m.prepare(ctx, prep{bot: bot, rc: rc, sink: s, text: text, history: historyMessages(history), draft: true})
	if err != nil {
		if ctx.Err() != nil {
			return StatusCanceled, ""
		}
		return StatusError, describeError(err)
	}
	return m.loop(ctx, env)
}

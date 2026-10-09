package chat

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// turn is one unit of work for a bot's worker: answer a stored user message.
type turn struct {
	userMsgID string
	mode      string
	// asstID is set when regenerating: the assistant message is reset and reused in place.
	asstID string
}

// runState exists in Manager.runs while a bot (or draft) has an active run. Further sends are
// queued here and processed in order after the current run, one run per bot at a time.
type runState struct {
	cancel context.CancelFunc // cancels the active turn; nil between turns
	queue  []turn
	done   chan struct{} // closed when the worker exits
}

// Send stores the user message immediately and starts a run, or queues the message behind the
// active run (it is stored with status pending until its turn starts). A retried ClientNonce
// returns the stored message instead of sending twice.
func (m *Manager) Send(ctx context.Context, req SendRequest) (Message, error) {
	if !utf8.ValidString(req.Text) {
		return Message{}, invalidf("The message is not valid text.")
	}
	if len(req.Text) > MaxMessageTextLen {
		return Message{}, invalidf("Messages are limited to %d KiB of text.", MaxMessageTextLen>>10)
	}
	text := strings.TrimRight(req.Text, " \t\r\n")
	if strings.TrimSpace(text) == "" && len(req.AttachmentIDs) == 0 {
		return Message{}, invalidf("Write a message or attach a file.")
	}
	if len(req.AttachmentIDs) > MaxAttachmentsPerMsg {
		return Message{}, invalidf("A message can have at most %d attachments.", MaxAttachmentsPerMsg)
	}
	switch req.Mode {
	case ModeChat:
	case ModeResearch:
		if strings.TrimSpace(text) == "" {
			return Message{}, invalidf("Research needs a question.")
		}
		if len(req.AttachmentIDs) > 0 {
			return Message{}, invalidf("Research takes a question only; remove the attachments or send a normal message.")
		}
	default:
		return Message{}, invalidf("Unknown mode %q.", req.Mode)
	}
	ids := dedupe(req.AttachmentIDs)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Message{}, ErrClosed
	}
	if req.ClientNonce != "" {
		if id, ok := m.nonces.get(req.BotID, req.ClientNonce); ok {
			if msg, err := m.store.getMessage(ctx, id); err == nil {
				return msg, nil
			}
		}
	}
	if _, err := m.store.getBot(ctx, req.BotID, false); err != nil {
		return Message{}, err
	}
	st := m.runs[req.BotID]
	if st != nil && len(st.queue) >= maxQueued {
		return Message{}, busyf("Too many messages are waiting. Wait for the bot to catch up.")
	}
	msg := Message{
		ID: newID(), BotID: req.BotID, Role: "user", Author: "user",
		Status: StatusComplete, CreatedAt: m.nowMs(),
	}
	if st != nil {
		msg.Status = StatusPending
	}
	if err := m.store.insertUserMessage(ctx, &msg, text, ids); err != nil {
		return Message{}, err
	}
	if req.ClientNonce != "" {
		m.nonces.put(req.BotID, req.ClientNonce, msg.ID)
	}
	m.cancelIdleLocked(req.BotID)
	m.publishMessage(msg)
	t := turn{userMsgID: msg.ID, mode: req.Mode}
	if st != nil {
		st.queue = append(st.queue, t)
	} else {
		m.startLocked(req.BotID, t)
	}
	return msg, nil
}

func dedupe(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !containsString(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// startLocked launches the worker for a bot. The caller holds m.mu and has verified there is no run.
func (m *Manager) startLocked(botID string, first turn) {
	st := &runState{done: make(chan struct{}), queue: []turn{first}}
	m.runs[botID] = st
	m.wg.Add(1)
	go m.pump(botID, st)
}

// pump processes the bot's queue in FIFO order, then exits.
func (m *Manager) pump(botID string, st *runState) {
	defer m.wg.Done()
	ran := false
	for {
		m.mu.Lock()
		if len(st.queue) == 0 || m.closed {
			delete(m.runs, botID)
			st.cancel = nil
			close(st.done)
			m.mu.Unlock()
			m.emit("chat.run", RunState{BotID: botID})
			if ran {
				m.armIdle(botID)
			}
			return
		}
		t := st.queue[0]
		st.queue = st.queue[1:]
		ctx, cancel := context.WithCancel(m.ctx)
		st.cancel = cancel
		m.mu.Unlock()
		m.runTurn(ctx, botID, t)
		cancel()
		ran = true
	}
}

// Regenerate resets the latest assistant reply in place (same message id) and runs it again.
func (m *Manager) Regenerate(ctx context.Context, req RegenerateRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.runs[req.BotID] != nil {
		return busyf("The bot is still replying. Stop it first, then regenerate.")
	}
	if _, err := m.store.getBot(ctx, req.BotID, false); err != nil {
		return err
	}
	last, err := m.store.lastMessage(ctx, req.BotID)
	if err != nil {
		return err
	}
	if last.Role != "assistant" || last.ID != req.MessageID {
		return invalidf("Only the latest reply can be regenerated.")
	}
	user, err := m.store.userMessageBefore(ctx, req.BotID, last.Seq)
	if err != nil {
		return err
	}
	mode := ModeChat
	for _, p := range last.Parts {
		if p.Type == PartReport {
			mode = ModeResearch
		}
	}
	m.cancelIdleLocked(req.BotID)
	m.startLocked(req.BotID, turn{userMsgID: user.ID, mode: mode, asstID: last.ID})
	return nil
}

// Cancel stops the active run of a bot (or draft "draft:<id>") and drops messages still queued
// behind it: stopping means stopping, so queued messages are marked canceled rather than sent.
func (m *Manager) Cancel(ctx context.Context, botID string) error {
	m.mu.Lock()
	st := m.runs[botID]
	if st == nil {
		m.mu.Unlock()
		return nil
	}
	dropped, cancel := st.queue, st.cancel
	st.queue = nil
	m.mu.Unlock()
	m.dropQueued(ctx, dropped)
	if cancel != nil {
		cancel()
	}
	return nil
}

func (m *Manager) dropQueued(ctx context.Context, turns []turn) {
	for _, t := range turns {
		if t.userMsgID == "" {
			continue
		}
		if err := m.store.setStatus(ctx, t.userMsgID, StatusCanceled, "Not sent: the reply before it was stopped."); err != nil {
			m.log.Warn("chat: cancel queued message", "err", err)
			continue
		}
		if msg, err := m.store.getMessage(ctx, t.userMsgID); err == nil {
			m.publishMessage(msg)
		}
	}
}

// whenIdle stops the bot's run (dropping its queue), then runs fn while no new run can start.
func (m *Manager) whenIdle(ctx context.Context, botID string, fn func() error) error {
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return ErrClosed
		}
		st := m.runs[botID]
		if st == nil {
			defer m.mu.Unlock()
			if _, err := m.store.getBot(ctx, botID, false); err != nil {
				return err
			}
			return fn()
		}
		dropped, cancel, done := st.queue, st.cancel, st.done
		st.queue = nil
		m.mu.Unlock()
		m.dropQueued(ctx, dropped)
		if cancel != nil {
			cancel()
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ---- idle consolidation ----

type idleJob struct{ timer *time.Timer }

// armIdle schedules one Memory.Consolidate call after the bot has been idle for the configured time.
func (m *Manager) armIdle(botID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.memory == nil {
		return
	}
	m.cancelIdleLocked(botID)
	j := &idleJob{}
	j.timer = time.AfterFunc(m.cfg.IdleConsolidateAfter, func() { m.consolidate(botID, j) })
	m.idle[botID] = j
}

func (m *Manager) cancelIdleLocked(botID string) {
	if j := m.idle[botID]; j != nil {
		j.timer.Stop()
		delete(m.idle, botID)
	}
}

func (m *Manager) cancelIdleTimer(botID string) {
	m.mu.Lock()
	m.cancelIdleLocked(botID)
	m.mu.Unlock()
}

func (m *Manager) consolidate(botID string, j *idleJob) {
	m.mu.Lock()
	if m.idle[botID] != j {
		m.mu.Unlock()
		return
	}
	delete(m.idle, botID)
	mem := m.memory
	if m.closed || mem == nil || m.runs[botID] != nil {
		m.mu.Unlock()
		return
	}
	m.wg.Add(1)
	m.mu.Unlock()
	defer m.wg.Done()
	if err := mem.Consolidate(m.ctx, botID); err != nil && m.ctx.Err() == nil {
		m.log.Warn("chat: memory consolidation failed", "bot", botID, "err", err)
	}
}

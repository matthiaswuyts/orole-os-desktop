package chat

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

const (
	voiceMaxSteps     = 6
	voiceMaxToolCalls = 10
	callPrefix        = "call:"
	composeMaxTokens  = 200
	callConnected     = "(The phone call has connected.)"
)

// voiceReasoning picks the cheapest reasoning level the model offers; "" when it has none to choose.
func voiceReasoning(levels []string) string {
	for _, l := range []string{"none", "minimal", "low"} {
		if slices.Contains(levels, l) {
			return l
		}
	}
	return ""
}

// VoiceLine is one earlier utterance of a call. Role is "user" (the owner) or "assistant" (the bot).
type VoiceLine struct{ Role, Text string }

// VoiceRequest is one spoken utterance to answer.
type VoiceRequest struct {
	BotID, CallID, Text string
	Brief               string      // call context; untrusted parts already fenced by the caller
	History             []VoiceLine // earlier turns of this call, oldest first
	OnText              func(string)
	OnTool              func(name string)
}

// VoiceTurn answers one spoken utterance with the bot's own model and tools. It runs under its own run key, so a
// phone call never waits behind the bot's chat queue, and it stores the utterance and reply in the bot's thread like
// a typed exchange (message sequence numbers are assigned atomically by the store, so the two interleave safely).
// It returns everything the bot said, as streamed to OnText. When ctx is canceled (the owner interrupts) the partial
// reply is returned with the context error.
func (m *Manager) VoiceTurn(ctx context.Context, req VoiceRequest) (string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return "", invalidf("Nothing was said.")
	}
	if req.CallID == "" {
		return "", invalidf("A voice turn needs a call id.")
	}
	bot, err := m.Bot(ctx, req.BotID)
	if err != nil {
		return "", err
	}
	key := callPrefix + req.CallID
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel) // daemon shutdown ends the turn
	defer stop()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrClosed
	}
	if m.runs[key] != nil {
		m.mu.Unlock()
		return "", busyf("Still answering the previous sentence.")
	}
	st := &runState{cancel: cancel, done: make(chan struct{})}
	m.runs[key] = st
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.runs, key)
		close(st.done)
		m.mu.Unlock()
		m.wg.Done()
	}()

	bg := context.WithoutCancel(ctx)
	now := m.nowMs()
	user := Message{ID: newID(), BotID: bot.ID, Role: "user", Author: "user", Status: StatusComplete, CreatedAt: now, FinishedAt: now,
		Parts: []MsgPart{{Type: PartText, Text: req.Text}}}
	if err := m.store.insertMessage(bg, &user); err != nil {
		return "", err
	}
	m.publishMessage(user)
	asst := Message{ID: newID(), BotID: bot.ID, Role: "assistant", Author: "bot:" + bot.ID, Status: StatusRunning, CreatedAt: m.nowMs(), Parts: []MsgPart{}}
	if err := m.store.insertMessage(bg, &asst); err != nil {
		return "", err
	}
	persist := func(msg Message) error {
		pc, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		return m.store.updateMessage(pc, msg)
	}
	s := newSink(asst, persist, m.publishMessage, m.publishDelta)
	s.clock = m.cfg.Clock
	s.checkpoint()
	var spoken strings.Builder
	onText := func(t string) {
		spoken.WriteString(t)
		if req.OnText != nil {
			req.OnText(t)
		}
	}
	rc := RunContext{BotID: bot.ID, Bot: bot, MessageID: asst.ID, Actor: "bot:" + bot.ID, Author: "user", CallID: req.CallID, Emit: s.upsertTool}
	hist := make([]Message, 0, len(req.History))
	for _, l := range req.History {
		hist = append(hist, Message{Role: l.Role, Status: StatusComplete, Parts: []MsgPart{{Type: PartText, Text: l.Text}}})
	}
	if len(hist) > 0 && hist[0].Role == "assistant" {
		// The bot usually speaks first (the opener); historyMessages drops a leading assistant message, so give it a cue.
		hist = slices.Insert(hist, 0, Message{Role: "user", Status: StatusComplete, Parts: []MsgPart{{Type: PartText, Text: callConnected}}})
	}
	env, err := m.prepare(runCtx, prep{bot: bot, convo: bot.ID, rc: rc, sink: s, text: req.Text, history: historyMessages(hist),
		memory: true, voice: true, extra: req.Brief, onText: onText, onTool: req.OnTool})
	if err != nil {
		status, text := StatusError, describeError(err)
		if runCtx.Err() != nil {
			status, text = StatusCanceled, ""
		}
		_ = s.finish(status, text)
		return "", err
	}
	status, errText := m.loop(runCtx, env)
	if status == StatusCanceled && m.ctx.Err() != nil {
		status = StatusIncomplete // the daemon is shutting down
	}
	_ = s.finish(status, errText)
	switch status {
	case StatusError:
		return spoken.String(), errors.New(errText)
	case StatusCanceled, StatusIncomplete:
		return spoken.String(), context.Canceled
	}
	return spoken.String(), nil
}

// Compose asks the bot's model for one short spoken line (the call opener). Nothing is stored and no tools run.
func (m *Manager) Compose(ctx context.Context, botID, instruction string) (string, error) {
	bot, err := m.Bot(ctx, botID)
	if err != nil {
		return "", err
	}
	asst := Message{ID: newID(), BotID: callPrefix + "compose", Role: "assistant", Parts: []MsgPart{}}
	s := newSink(asst, nil, func(Message) {}, func(MessageDelta) {})
	s.clock = m.cfg.Clock
	env, err := m.prepare(ctx, prep{bot: bot, rc: RunContext{BotID: bot.ID, Bot: bot, Actor: "bot:" + bot.ID}, sink: s, text: instruction, draft: true, voice: true})
	if err != nil {
		return "", err
	}
	ch, err := env.provider.Stream(ctx, llm.Request{Model: env.modelID, System: env.system,
		Messages:  []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: instruction}}}},
		Reasoning: env.reasoning, MaxOutputTokens: composeMaxTokens})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for d := range ch {
		switch d.Kind {
		case llm.DeltaText:
			b.WriteString(d.Text)
		case llm.DeltaError:
			go drain(ch)
			if d.Err == nil {
				return "", errors.New("the model reported an error")
			}
			return "", d.Err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

// PostCallCard stores a phone call card in the bot's thread and returns its message id.
func (m *Manager) PostCallCard(ctx context.Context, botID string, part MsgPart) (string, error) {
	if _, err := m.store.getBot(ctx, botID, false); err != nil {
		return "", err
	}
	part.Type = PartCall
	now := m.nowMs()
	msg := Message{ID: newID(), BotID: botID, Role: "assistant", Author: "bot:" + botID, Status: StatusComplete, CreatedAt: now, FinishedAt: now, Parts: []MsgPart{part}}
	if err := m.store.insertMessage(ctx, &msg); err != nil {
		return "", err
	}
	m.publishMessage(msg)
	return msg.ID, nil
}

// UpdateCallCard replaces the call card in a message posted by PostCallCard.
func (m *Manager) UpdateCallCard(ctx context.Context, msgID string, part MsgPart) error {
	msg, err := m.store.getMessage(ctx, msgID)
	if err != nil {
		return err
	}
	if len(msg.Parts) != 1 || msg.Parts[0].Type != PartCall {
		return invalidf("That message is not a call card.")
	}
	part.Type = PartCall
	msg.Parts = []MsgPart{part}
	if err := m.store.updateMessage(ctx, msg); err != nil {
		return err
	}
	m.publishMessage(msg)
	return nil
}

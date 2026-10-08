package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// QueuedPrompt is a durable follow-up, not a promise of delivery. A sending entry
// left behind by a crash becomes uncertain and requires removal, never replay.
type QueuedPrompt struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Model     string    `json:"model,omitempty"`
	Reasoning string    `json:"reasoning,omitempty"`
	State     string    `json:"state"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"createdAt"`
}

const (
	queueQueued      = "queued"
	queueSending     = "sending"
	queuePaused      = "paused"
	queueUncertain   = "uncertain"
	maxQueuedPrompts = 100
)

func validPrompt(text string) error {
	if strings.TrimSpace(text) == "" || len(text) > 100<<10 || strings.ContainsRune(text, 0) || !utf8.ValidString(text) {
		return errors.New("prompt must be valid text of 1-102400 bytes without NUL")
	}
	return nil
}

func pauseQueue(s *Session, reason string) {
	s.Queue = append([]QueuedPrompt(nil), s.Queue...)
	for i := range s.Queue {
		switch s.Queue[i].State {
		case queueSending:
			s.Queue[i].State, s.Queue[i].Error = queueUncertain, "Delivery could not be confirmed. Remove this entry before sending it again."
		case queueQueued:
			s.Queue[i].State, s.Queue[i].Error = queuePaused, reason
		}
	}
}

func queueBlocked(s Session) bool {
	for _, q := range s.Queue {
		if q.State != queueQueued {
			return true
		}
	}
	return false
}

func (m *Manager) controlSession(id string) *liveSess {
	m.mu.Lock()
	ls := m.live[id]
	m.mu.Unlock()
	return ls
}

// QueuePrompt enqueues a follow-up without launching or steering an agent.
func (m *Manager) QueuePrompt(ctx context.Context, id, text string) (Session, error) {
	text = strings.TrimSpace(text)
	if err := validPrompt(text); err != nil {
		return Session{}, err
	}
	if err := m.authorize(ctx, "agent.prompt", id, "Queue a task for this agent; it may execute tools under its configured permissions."); err != nil {
		return Session{}, err
	}
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	ls := m.controlSession(id)
	if ls != nil {
		ls.controlMu.Lock()
		defer ls.controlMu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	return m.enqueueControlled(id, ls, text, "", "")
}

// enqueueControlled requires launchMu and, for a live session, controlMu.
func (m *Manager) enqueueControlled(id string, ls *liveSess, text, model, reasoning string) (Session, error) {
	m.mu.Lock()
	var s Session
	var err error
	if current := m.live[id]; current != nil {
		s = current.s
	} else {
		s, err = m.st.GetSession(id)
	}
	if err == nil && (m.closing || (ls != nil && (m.live[id] != ls || ls.stopping))) {
		err = errors.New("agent is stopping")
	}
	if err == nil && (!localMachine(s.Machine) || s.Mode != ModeStructured) {
		err = errors.New("follow-up queues require a local structured agent")
	}
	if err == nil && len(s.Queue) >= maxQueuedPrompts {
		err = errors.New("follow-up queue is full (100 entries)")
	}
	if err != nil {
		m.mu.Unlock()
		return Session{}, err
	}
	q := QueuedPrompt{ID: uuid.NewString(), Text: text, Model: model, Reasoning: reasoning, State: queueQueued, CreatedAt: time.Now().UTC()}
	if ls == nil || (ls != nil && ls.deliveryUncertain) || s.Status == StatusFailed || s.Status == StatusStopped || s.Status == StatusLost || queueBlocked(s) {
		q.State, q.Error = queuePaused, "Queue is paused; resume it explicitly when the agent is ready."
	}
	s, err = m.mutateLocked(id, func(s *Session) {
		queue := make([]QueuedPrompt, len(s.Queue)+1)
		copy(queue, s.Queue)
		queue[len(s.Queue)] = q
		s.Queue = queue
	})
	m.mu.Unlock()
	if err != nil {
		return s, err
	}
	m.cfg.Publish("agents.updated", s)
	if ls != nil {
		if err := m.dispatchQueueControlled(ls); err != nil {
			return m.queueResult(id, err)
		}
	}
	return m.Get(id)
}

// CancelQueuedPrompt removes an undelivered or uncertain entry. It cannot revoke
// a prompt that has already been accepted by the process.
func (m *Manager) CancelQueuedPrompt(ctx context.Context, id, queueID string) (Session, error) {
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	ls := m.controlSession(id)
	if ls != nil {
		ls.controlMu.Lock()
		defer ls.controlMu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	var s Session
	var err error
	if current := m.live[id]; current != nil {
		s = current.s
	} else {
		s, err = m.st.GetSession(id)
	}
	index := -1
	for i, q := range s.Queue {
		if q.ID == queueID {
			index = i
			if q.State == queueSending {
				err = errors.New("prompt delivery is already in progress")
			}
			break
		}
	}
	if err == nil && index < 0 {
		err = errors.New("no such queued prompt")
	}
	if err == nil {
		s, err = m.mutateLocked(id, func(s *Session) {
			queue := append([]QueuedPrompt(nil), s.Queue...)
			s.Queue = append(queue[:index], queue[index+1:]...)
		})
	}
	m.mu.Unlock()
	if err == nil {
		m.cfg.Publish("agents.updated", s)
	}
	return s, err
}

// ResumeQueue explicitly releases paused follow-ups on an existing live process.
// Uncertain deliveries must be removed first, even when they are not at the head.
func (m *Manager) ResumeQueue(ctx context.Context, id string) (Session, error) {
	if err := m.authorize(ctx, "agent.prompt", id, "Resume queued tasks under this agent's configured permissions."); err != nil {
		return Session{}, err
	}
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	ls := m.controlSession(id)
	if ls == nil {
		return Session{}, errors.New("resume the agent explicitly before resuming its queue")
	}
	ls.controlMu.Lock()
	defer ls.controlMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	if m.live[id] != ls || ls.stopping || m.closing || ls.deliveryUncertain || !localMachine(ls.s.Machine) || ls.s.Mode != ModeStructured {
		m.mu.Unlock()
		return Session{}, errors.New("queue requires a live local structured agent with no uncertain delivery; stop and restart after an uncertain send")
	}
	if len(ls.s.Pending) != 0 || len(ls.s.Questions) != 0 || (ls.s.Status != StatusIdle && ls.s.Status != StatusDone && ls.s.Status != StatusFailed) {
		m.mu.Unlock()
		return Session{}, errors.New("finish the current turn and pending requests before resuming the queue")
	}
	for _, q := range ls.s.Queue {
		if q.State == queueUncertain || q.State == queueSending {
			m.mu.Unlock()
			return Session{}, errors.New("remove uncertain deliveries before resuming the queue; they will not be resent")
		}
	}
	s, err := m.mutateLocked(id, func(s *Session) {
		s.Queue = append([]QueuedPrompt(nil), s.Queue...)
		for i := range s.Queue {
			s.Queue[i].State, s.Queue[i].Error = queueQueued, ""
		}
		if s.Status == StatusFailed && len(s.Queue) > 0 {
			s.Status = StatusIdle
		}
	})
	m.mu.Unlock()
	if err != nil {
		return s, err
	}
	m.cfg.Publish("agents.updated", s)
	return m.queueResult(id, m.dispatchQueueControlled(ls))
}

func (m *Manager) queueResult(id string, dispatchErr error) (Session, error) {
	s, err := m.Get(id)
	if dispatchErr != nil {
		return s, dispatchErr
	}
	return s, err
}

// dispatchQueueControlled requires controlMu. Persisting sending before touching
// the process makes a crash fail closed, including a crash after a successful write.
func (m *Manager) dispatchQueueControlled(ls *liveSess) error {
	m.mu.Lock()
	if m.live[ls.id] != ls || ls.stopping || m.closing || ls.deliveryUncertain || len(ls.s.Queue) == 0 || queueBlocked(ls.s) ||
		!localMachine(ls.s.Machine) || ls.s.Mode != ModeStructured || len(ls.s.Pending) != 0 || len(ls.s.Questions) != 0 ||
		(ls.s.Status != StatusIdle && ls.s.Status != StatusDone) {
		m.mu.Unlock()
		return nil
	}
	q := ls.s.Queue[0]
	s, err := m.mutateLocked(ls.id, func(s *Session) {
		s.Queue = append([]QueuedPrompt(nil), s.Queue...)
		s.Queue[0].State = queueSending
	})
	m.mu.Unlock()
	if err != nil {
		return m.pauseUnsentQueueControlled(ls, q.ID, err)
	}
	m.cfg.Publish("agents.updated", s)
	err = m.sendPromptControlled(ls, q.Text, q.ID, false, q.Model, q.Reasoning)
	if err != nil {
		m.mu.Lock()
		uncertain := ls.deliveryUncertain
		m.mu.Unlock()
		if !uncertain {
			return m.pauseUnsentQueueControlled(ls, q.ID, err)
		}
	}
	return err
}

// A pre-dispatch persistence failure is known not to have delivered a prompt.
// Pause in memory even if the store is unavailable; recovery also fails closed.
func (m *Manager) pauseUnsentQueueControlled(ls *liveSess, queueID string, cause error) error {
	m.mu.Lock()
	if len(ls.s.Queue) > 0 && ls.s.Queue[0].ID == queueID {
		pauseQueue(&ls.s, "Queue dispatch failed. Resume explicitly.")
		ls.s.Queue[0].State, ls.s.Queue[0].Error = queuePaused, "The prompt was not dispatched: "+cause.Error()
	}
	s, err := m.mutateLocked(ls.id, func(*Session) {})
	m.mu.Unlock()
	m.cfg.Publish("agents.updated", s)
	return errors.Join(cause, err)
}

func cloneQuestions(requests []QuestionRequest) []QuestionRequest {
	out := append([]QuestionRequest(nil), requests...)
	for i := range out {
		if out[i].ExpiresAt != nil {
			expires := *out[i].ExpiresAt
			out[i].ExpiresAt = &expires
		}
		out[i].Questions = append([]Question(nil), out[i].Questions...)
		for j := range out[i].Questions {
			out[i].Questions[j].Options = append([]QuestionOption(nil), out[i].Questions[j].Options...)
		}
	}
	return out
}

func validateQuestionRequest(req QuestionRequest) error {
	if req.ID == "" || len(req.ID) > 256 || len(req.Questions) == 0 || len(req.Questions) > 32 || len(jraw(req)) > 64<<10 {
		return errors.New("invalid question request shape or size")
	}
	ids := make(map[string]bool, len(req.Questions))
	for _, q := range req.Questions {
		if q.ID == "" || len(q.ID) > 256 || ids[q.ID] || len(q.Options) > 64 || (len(q.Options) == 0 && !q.AllowText) {
			return errors.New("invalid or duplicate question")
		}
		ids[q.ID] = true
		options := make(map[string]bool, len(q.Options))
		for _, option := range q.Options {
			if option.ID == "" || len(option.ID) > 8192 || options[option.ID] {
				return errors.New("invalid or duplicate question option")
			}
			options[option.ID] = true
		}
	}
	return nil
}

func validateAnswers(req QuestionRequest, answers []QuestionAnswer, cancel bool, now time.Time) error {
	if err := validateQuestionRequest(req); err != nil {
		return err
	}
	if req.ExpiresAt != nil && !now.Before(*req.ExpiresAt) {
		return errors.New("question request expired")
	}
	if cancel {
		if len(answers) != 0 {
			return errors.New("cancelled requests must not include answers")
		}
		return nil
	}
	if len(answers) != len(req.Questions) || len(jraw(answers)) > 64<<10 {
		return errors.New("answer every pending question exactly once (maximum 64 KiB)")
	}
	byID := make(map[string]Question, len(req.Questions))
	for _, q := range req.Questions {
		byID[q.ID] = q
	}
	seen := make(map[string]bool, len(answers))
	for _, answer := range answers {
		q, ok := byID[answer.QuestionID]
		if !ok || seen[answer.QuestionID] || len(answer.Values) == 0 || len(answer.Values) > 64 || (!q.Multiple && len(answer.Values) != 1) {
			return errors.New("answers contain unknown, duplicate or incorrectly shaped questions")
		}
		seen[answer.QuestionID] = true
		values := make(map[string]bool, len(answer.Values))
		for _, value := range answer.Values {
			if strings.TrimSpace(value) == "" || len(value) > 8192 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || values[value] {
				return errors.New("answer values must be unique nonempty text of at most 8192 bytes")
			}
			values[value] = true
			option := false
			for _, o := range q.Options {
				option = option || o.ID == value
			}
			if !option && !q.AllowText {
				return errors.New("answer is not one of the pending question's option IDs")
			}
		}
	}
	return nil
}

func removeQuestion(s *Session, requestID string) {
	rest := make([]QuestionRequest, 0, len(s.Questions))
	for _, req := range s.Questions {
		if req.ID != requestID {
			rest = append(rest, req)
		}
	}
	s.Questions = rest
	if len(s.Pending) == 0 && len(s.Questions) == 0 && s.Status == StatusNeedsYou {
		s.Status, s.Activity = StatusWorking, "Working"
	}
}

// AnswerInput answers only a typed question request, never a tool permission.
func (m *Manager) AnswerInput(ctx context.Context, id, requestID string, answers []QuestionAnswer, cancel bool) (Session, error) {
	ls := m.controlSession(id)
	if ls == nil {
		return Session{}, errors.New("agent is not running")
	}
	ls.controlMu.Lock()
	defer ls.controlMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	var req *QuestionRequest
	if m.live[id] == ls && !ls.stopping && !m.closing {
		for _, r := range ls.s.Questions {
			if r.ID == requestID {
				copy := r
				req = &copy
				break
			}
		}
	}
	m.mu.Unlock()
	if req == nil {
		m.cfg.Audit("agent.input", id, "denied", "No pending question request")
		return Session{}, fmt.Errorf("no pending question request %q for this session", requestID)
	}
	if err := validateAnswers(*req, answers, cancel, time.Now().UTC()); err != nil {
		m.cfg.Audit("agent.input", id, "denied", err.Error())
		return Session{}, err
	}
	responder, ok := ls.proc.(InputResponder)
	if !ok {
		m.cfg.Audit("agent.input", id, "denied", "Structured question responses are unsupported")
		return Session{}, errors.New("this agent does not support structured question responses")
	}
	// Claim before dispatch; even an error may mean the response reached the wire.
	if _, err := m.mutate(id, func(s *Session) { removeQuestion(s, requestID) }); err != nil {
		return Session{}, err
	}
	if err := responder.AnswerInput(requestID, answers, cancel); err != nil {
		m.mu.Lock()
		ls.deliveryUncertain = true
		m.mu.Unlock()
		m.cfg.Audit("agent.input", id, "error", "Question response delivery is uncertain")
		m.record(id, Event{Kind: EvQuestionCancel, Data: jraw(map[string]string{"id": requestID}), Text: "Question response delivery is uncertain; it will not be resent."})
		_, _ = m.mutate(id, func(s *Session) { pauseQueue(s, "Question response delivery is uncertain.") })
		return m.queueResult(id, err)
	}
	// Secrets go to the harness, not the persisted transcript or audit log.
	recorded := make([]QuestionAnswer, len(answers))
	for i, answer := range answers {
		recorded[i] = QuestionAnswer{QuestionID: answer.QuestionID, Values: append([]string(nil), answer.Values...)}
		for _, q := range req.Questions {
			if q.ID == answer.QuestionID && q.Secret {
				recorded[i].Values = []string{"[redacted]"}
			}
		}
	}
	outcome := "answered"
	if cancel {
		outcome = "cancelled"
	}
	m.cfg.Audit("agent.input", id, outcome, fmt.Sprintf("%d question(s)", len(req.Questions)))
	m.record(id, Event{Kind: EvAnswer, Data: jraw(map[string]any{"id": requestID, "answers": recorded, "cancelled": cancel})})
	return m.Get(id)
}

func cloneUsage(usage *ContextUsage) *ContextUsage {
	if usage == nil {
		return nil
	}
	copy := *usage
	clone := func(v *int64) *int64 {
		if v == nil {
			return nil
		}
		n := *v
		return &n
	}
	copy.InputTokens = clone(copy.InputTokens)
	copy.OutputTokens = clone(copy.OutputTokens)
	copy.CachedTokens = clone(copy.CachedTokens)
	copy.ContextTokens = clone(copy.ContextTokens)
	copy.ContextWindow = clone(copy.ContextWindow)
	return &copy
}

func (m *Manager) applyChatEvent(id string, e Event) bool {
	switch e.Kind {
	case EvUsage:
		var usage ContextUsage
		if json.Unmarshal(e.Data, &usage) == nil {
			if usage.At.IsZero() {
				usage.At = e.At
			}
			_, _ = m.mutate(id, func(s *Session) {
				merged := cloneUsage(s.Usage)
				if merged == nil {
					merged = &ContextUsage{}
				}
				if usage.Source != "" {
					merged.Source = usage.Source
				}
				if usage.InputTokens != nil {
					merged.InputTokens = usage.InputTokens
				}
				if usage.OutputTokens != nil {
					merged.OutputTokens = usage.OutputTokens
				}
				if usage.CachedTokens != nil {
					merged.CachedTokens = usage.CachedTokens
				}
				if usage.ContextTokens != nil {
					merged.ContextTokens = usage.ContextTokens
				}
				if usage.ContextWindow != nil {
					merged.ContextWindow = usage.ContextWindow
				}
				merged.At = usage.At
				s.Usage = merged
			})
		}
	case EvQuestion:
		var req QuestionRequest
		if json.Unmarshal(e.Data, &req) != nil || validateQuestionRequest(req) != nil {
			return true
		}
		if req.At.IsZero() {
			req.At = e.At
		}
		duplicate := false
		_, _ = m.mutate(id, func(s *Session) {
			for _, old := range s.Questions {
				duplicate = duplicate || old.ID == req.ID
			}
			removeQuestion(s, req.ID)
			if !duplicate && (req.ExpiresAt == nil || time.Now().Before(*req.ExpiresAt)) {
				s.Questions = append(s.Questions, req)
				s.Status, s.Activity = StatusNeedsYou, "Waiting for your answer"
			}
		})
		if duplicate {
			m.record(id, Event{Kind: EvQuestionCancel, Text: "Duplicate question request rejected.", Data: jraw(map[string]string{"id": req.ID})})
		}
	case EvQuestionCancel, EvAnswer:
		var ref struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(e.Data, &ref) == nil && ref.ID != "" {
			_, _ = m.mutate(id, func(s *Session) { removeQuestion(s, ref.ID) })
		}
	default:
		return false
	}
	return true
}

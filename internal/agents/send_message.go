package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// FollowupCapabilities describe this conversation, not a provider's launch options.
// They are runtime-derived; clients must not infer live controls from CLI flags.
type FollowupCapabilities struct {
	Attachments bool   `json:"attachments"`
	Model       bool   `json:"model"`
	Reasoning   bool   `json:"reasoning"`
	Reason      string `json:"reason"`
}

type configuredProcess interface {
	FollowupOptions() (model, reasoning bool)
	SendConfigured(prompt, model, reasoning string) error
}

func (m *Manager) followupCapabilities(s Session, ls *liveSess) FollowupCapabilities {
	if !localMachine(s.Machine) || s.Mode != ModeStructured {
		return FollowupCapabilities{Reason: "Follow-ups require a local structured conversation; continue this agent in its terminal."}
	}
	if ls == nil {
		if !m.adapterCanResume(s.Kind) || s.AgentSessionID == "" {
			return FollowupCapabilities{Reason: "The agent has no resumable conversation. Start a new task explicitly."}
		}
		return FollowupCapabilities{Attachments: true, Model: true, Reasoning: len(reasoningValues(s.Kind)) > 0}
	}
	c := FollowupCapabilities{Attachments: true}
	if p, ok := ls.proc.(configuredProcess); ok {
		c.Model, c.Reasoning = p.FollowupOptions()
	}
	if !c.Model || !c.Reasoning {
		c.Reason = "This running harness does not support changing these settings. They can be changed when continuing an ended conversation."
		if s.Kind == "claude" && c.Model {
			c.Reason = "Claude supports live model changes, but effort can only change when continuing an ended conversation."
		}
	}
	return c
}

func validateFollowupOptions(s Session, c FollowupCapabilities, model, reasoning string) error {
	if err := checkToken("model", model); err != nil {
		return err
	}
	if err := validateReasoning(s.Kind, reasoning); err != nil {
		return err
	}
	if model != "" && model != s.Model && !c.Model {
		return errors.New(c.Reason)
	}
	if reasoning != "" && reasoning != s.Reasoning && !c.Reasoning {
		return errors.New(c.Reason)
	}
	if s.Kind == "omp" && model != "" && model != s.Model {
		provider, name, ok := strings.Cut(model, "/")
		if !ok || provider == "" || name == "" {
			return errors.New("Oh My Pi follow-up models must use provider/model selectors")
		}
	}
	return nil
}

// SendMessage chooses delivery under the daemon's control locks. Upload IDs are
// resolved and copied before accepting a queue entry, so expiry or discard of
// staging cannot change an accepted follow-up. A stopped agent must resume the
// same CLI conversation; this operation never silently starts a fresh task.
func (m *Manager) SendMessage(ctx context.Context, id, text string, attachmentIDs []string, model, reasoning string) (Session, error) {
	text = strings.TrimSpace(text)
	if text != "" || len(attachmentIDs) == 0 {
		if err := validPrompt(text); err != nil {
			return Session{}, err
		}
	}
	if err := m.authorize(ctx, "agent.prompt", id, "Send a follow-up with selected files and model settings under this agent's configured permissions."); err != nil {
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
	s, err := m.Get(id)
	if err != nil {
		return Session{}, err
	}
	if !s.Followup.Attachments {
		return Session{}, errors.New(s.Followup.Reason)
	}
	if err := validateFollowupOptions(s, s.Followup, model, reasoning); err != nil {
		return Session{}, err
	}
	if ls != nil {
		m.mu.Lock()
		stopping := m.closing || m.live[id] != ls || ls.stopping
		uncertain := ls.deliveryUncertain
		m.mu.Unlock()
		if stopping || uncertain {
			return Session{}, errors.New("agent is stopping or its previous delivery is uncertain")
		}
	}
	if len(s.Queue) >= maxQueuedPrompts {
		return Session{}, errors.New("follow-up queue is full (100 entries)")
	}
	// Re-sending the current value is not a request for an unsupported live setter.
	if !s.Followup.Model && model == s.Model {
		model = ""
	}
	if !s.Followup.Reasoning && reasoning == s.Reasoning {
		reasoning = ""
	}
	if len(attachmentIDs) > 0 {
		m.attachmentMu.Lock()
		defer m.attachmentMu.Unlock()
		attachments, err := m.resolveAttachments(attachmentIDs)
		if err != nil {
			return Session{}, err
		}
		input := s
		input.ID += "-" + uuid.NewString()
		input.Task = text
		text, err = m.handoffAttachments(ctx, input, attachments)
		if err != nil {
			return Session{}, fmt.Errorf("prepare follow-up attachments: %w", err)
		}
	}
	if err := validPrompt(text); err != nil {
		return Session{}, err
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if ls != nil && (len(s.Queue) > 0 || s.Status == StatusWorking || s.Status == StatusNeedsYou || len(s.Pending) > 0 || len(s.Questions) > 0) {
		return m.enqueueControlled(id, ls, text, model, reasoning)
	}
	if ls == nil {
		// Continuation is checked above while launchMu pins the recipient. Launch
		// uses the same native CLI session ID, with no fallback to a fresh task.
		if _, err := m.mutate(id, func(s *Session) {
			if model != "" {
				s.Model = model
			}
			if reasoning != "" {
				s.Reasoning = reasoning
			}
		}); err != nil {
			return Session{}, err
		}
		return m.relaunchLocked(ctx, id, text, false)
	}
	return m.queueResult(id, m.sendPromptControlled(ls, text, "", false, model, reasoning))
}

package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// PromptRevision pins a recipient's configuration, process generation and turn.
// It intentionally excludes output text and names; no prompt content is hashed.
func PromptRevision(s Session) string {
	v := struct {
		ID, Kind, Machine, Dir, Model, Reasoning, Command, PermissionMode, AgentSessionID string
		Mode                                                                              Mode
		Status                                                                            Status
		Live                                                                              bool
		Pid, Prompts, Turns, Pending                                                      int
		StartedAt, UpdatedAt                                                              time.Time
	}{s.ID, s.Kind, s.Machine, s.Dir, s.Model, s.Reasoning, s.Command, s.PermissionMode, s.AgentSessionID,
		s.Mode, s.Status, s.Live, s.Pid, s.Prompts, s.Turns, len(s.Pending), s.StartedAt, s.UpdatedAt}
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// CanPromptScoped limits sharing to idle structured sessions or true local
// continuations. Terminal injection and fresh restarts are not content handoffs.
func (m *Manager) CanPromptScoped(s Session) bool {
	if !localMachine(s.Machine) || s.Mode != ModeStructured || len(s.Pending) != 0 || len(s.Questions) != 0 || len(s.Queue) != 0 || s.Status == StatusWorking || s.Status == StatusNeedsYou {
		return false
	}
	if s.Live {
		return s.Status == StatusIdle || s.Status == StatusDone
	}
	return m.adapterCanResume(s.Kind) && s.AgentSessionID != ""
}

// PromptScoped uses the normal prompt authorization and delivery/continuation
// paths, but rejects recipient changes across approval. beforeSend revalidates
// the source authority, outside manager locks; it must not deliver content.
func (m *Manager) PromptScoped(ctx context.Context, id, text, revision string, beforeSend func(context.Context) error) error {
	if strings.TrimSpace(text) == "" || len(text) > 100<<10 || strings.ContainsRune(text, 0) || revision == "" || beforeSend == nil {
		return errors.New("invalid scoped prompt")
	}
	if err := m.authorize(ctx, "agent.prompt", id, "Send a task to the agent; it may execute tools under its configured permissions."); err != nil {
		return err
	}
	if err := beforeSend(ctx); err != nil {
		return err
	}
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := m.Get(id)
	if err != nil {
		return err
	}
	if !m.CanPromptScoped(s) || PromptRevision(s) != revision {
		return errors.New("agent destination changed; choose the recipient again")
	}
	m.mu.Lock()
	ls := m.live[id]
	closing := m.closing
	stopping := ls != nil && ls.stopping
	m.mu.Unlock()
	if closing || stopping {
		return errors.New("agent destination is stopping")
	}
	if ls == nil {
		_, err = m.relaunchLocked(ctx, id, text, false)
		return err
	}
	return m.sendPrompt(ls, text)
}

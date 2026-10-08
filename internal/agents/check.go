package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RunShell runs a user-written command line in dir (cmd.exe on Windows, sh elsewhere) and
// returns its exit code and the last 16 KiB of combined output. The whole process tree is
// killed when ctx ends or timeout passes.
func RunShell(ctx context.Context, dir, command string, timeout time.Duration) (code int, output string, err error) {
	if strings.TrimSpace(command) == "" {
		return -1, "", errors.New("empty command")
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shellCommand(cctx, command)
	cmd.Dir = dir
	cmd.Env = childEnv()
	cmd.WaitDelay = 2 * time.Second
	out := &tailBuf{max: 16 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	j, err := newJob()
	if err != nil {
		return -1, "", err
	}
	defer j.close()
	if err := cmd.Start(); err != nil {
		return -1, "", err
	}
	if err := j.assign(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		return -1, "", err
	}
	go func() { <-cctx.Done(); j.kill() }()
	werr := cmd.Wait()
	if cctx.Err() != nil {
		if ctx.Err() != nil {
			return -1, out.String(), ctx.Err()
		}
		return -1, out.String(), fmt.Errorf("timed out after %s", timeout)
	}
	if werr != nil {
		if ee, ok := werr.(interface{ ExitCode() int }); ok {
			return ee.ExitCode(), out.String(), nil
		}
		return -1, out.String(), werr
	}
	return 0, out.String(), nil
}

// Check runs command in the session's working directory in the background and records the
// result as the session's check state (passing/failing) for the Review list.
func (m *Manager) Check(ctx context.Context, id, command string) (Session, error) {
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 2000 {
		return Session{}, errors.New("check command must be 1-2000 characters")
	}
	s, err := m.Get(id)
	if err != nil {
		return Session{}, err
	}
	if !localMachine(s.Machine) {
		return Session{}, errRemoteOperation
	}
	if err := m.authorize(ctx, "agent.check", s.Dir, command); err != nil {
		return Session{}, err
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if s.CheckState == CheckRunning {
		return Session{}, errors.New("a check is already running for this session")
	}
	m.cfg.Audit("agent.check", id, "allowed", truncate(command, 300))
	s, err = m.mutate(id, func(s *Session) { s.CheckState, s.CheckCommand, s.CheckOutput = CheckRunning, command, "" })
	if err != nil {
		return Session{}, err
	}
	if err := ctx.Err(); err != nil {
		_, _ = m.mutate(id, func(s *Session) { s.CheckState, s.CheckOutput = CheckFailing, err.Error() })
		return Session{}, err
	}
	if !m.background(func() {
		code, out, err := RunShell(m.bg, s.Dir, command, 10*time.Minute)
		state := CheckPassing
		if err != nil {
			state, out = CheckFailing, strings.TrimSpace(out+"\n"+err.Error())
		} else if code != 0 {
			state = CheckFailing
		}
		_, _ = m.mutate(id, func(s *Session) { s.CheckState, s.CheckOutput = state, truncate(out, 8<<10) })
		m.record(id, Event{Kind: EvSystem, Text: fmt.Sprintf("Check %s: %s", state, truncate(command, 200))})
	}) {
		return Session{}, errors.New("daemon is shutting down")
	}
	return s, nil
}

// Review records the reviewer's decision on a session's work.
//
//	approve   marks it approved (the branch/worktree is left for the user to merge)
//	send-back resumes the agent with the note as feedback
//	discard   stops the agent and, for a worktree the daemon created, removes it
func (m *Manager) Review(ctx context.Context, id, decision, note string) (Session, error) {
	note = strings.TrimSpace(note)
	s, err := m.Get(id)
	if err != nil {
		return Session{}, err
	}
	if !localMachine(s.Machine) {
		return Session{}, errRemoteOperation
	}
	switch decision {
	case "approve":
		m.cfg.Audit("agent.review", id, "allowed", "approve")
		m.record(id, Event{Kind: EvSystem, Text: "Approved" + noteSuffix(note)})
		return m.mutate(id, func(s *Session) { s.Review, s.ReviewNote = ReviewApproved, note })
	case "send-back":
		if note == "" {
			return Session{}, errors.New("say what to change when sending work back")
		}
		m.cfg.Audit("agent.review", id, "allowed", "send-back")
		m.record(id, Event{Kind: EvSystem, Text: "Sent back" + noteSuffix(note)})
		if _, err := m.mutate(id, func(s *Session) { s.Review, s.ReviewNote = ReviewSentBack, note }); err != nil {
			return Session{}, err
		}
		if err := m.Prompt(ctx, id, "Review feedback — please address this and finish again:\n"+note); err != nil {
			return Session{}, err
		}
		return m.Get(id)
	case "discard":
		if err := m.authorize(ctx, "agent.discard", s.Dir, "Stop this agent and discard its owned worktree, including uncommitted changes."); err != nil {
			return Session{}, err
		}
		if s.Live {
			if _, err := m.Stop(ctx, id); err != nil {
				return Session{}, err
			}
		}
		if s.WorktreeOwned && m.cfg.Worktrees != nil {
			if err := m.cfg.Worktrees.Remove(ctx, s.Project, s.Dir); err != nil {
				m.cfg.Audit("agent.review", id, "error", "discard: "+err.Error())
				return Session{}, fmt.Errorf("remove worktree: %w", err)
			}
		}
		m.cfg.Audit("agent.review", id, "allowed", "discard")
		m.record(id, Event{Kind: EvSystem, Text: "Discarded" + noteSuffix(note)})
		return m.mutate(id, func(s *Session) { s.Review, s.ReviewNote = ReviewDiscard, note })
	}
	return Session{}, fmt.Errorf("unknown review decision %q (approve | send-back | discard)", decision)
}

func noteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return ": " + truncate(note, 500)
}

// RunCheckCommand authorizes each orchestration check before executing it.
func (m *Manager) RunCheckCommand(ctx context.Context, dir, command string, timeout time.Duration) (int, string, error) {
	if err := m.authorize(ctx, "agent.check", dir, command); err != nil {
		return -1, "", err
	}
	return RunShell(ctx, dir, command, timeout)
}

// SetCheck records a check result obtained elsewhere (loop-until runs).
func (m *Manager) SetCheck(id, state, command, output string) error {
	_, err := m.mutate(id, func(s *Session) {
		s.CheckState, s.CheckCommand, s.CheckOutput = state, command, truncate(output, 8<<10)
	})
	return err
}

// HasTurnSignal reports whether the daemon can observe when a turn of this session ends
// (stream-json result or the Stop hook). Orchestration that waits for turns needs it.
func (s Session) HasTurnSignal() bool { return localMachine(s.Machine) && s.Kind == "claude" }

// AdapterName returns the display name of an agent kind.
func (m *Manager) AdapterName(kind string) string {
	if a, ok := m.adapters[kind]; ok {
		return a.Name()
	}
	return kind
}

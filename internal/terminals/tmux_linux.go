//go:build linux

package terminals

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/paths"
)

// A separate server per absolute Orole home avoids touching the user's ordinary
// tmux sessions. -f /dev/null deliberately excludes personal tmux configuration.
func (b linuxBackend) tmuxArgs(args ...string) ([]string, error) {
	socket := b.tmuxSocket
	if socket == "" {
		home, err := paths.DataDir()
		if err != nil {
			return nil, err
		}
		socket = fmt.Sprintf("orole-%x", sha256.Sum256([]byte(home)))
	}
	return append([]string{"-L", socket, "-f", "/dev/null"}, args...), nil
}

func tmuxEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "TMUX=") && !strings.HasPrefix(entry, "TMUX_PANE=") {
			out = append(out, entry)
		}
	}
	return terminalEnv(out)
}

func (b linuxBackend) tmux(args ...string) ([]byte, error) {
	argv, err := b.tmuxArgs(args...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", argv...)
	cmd.Env = tmuxEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("terminals: tmux (requires tmux 3.2+): %w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func validPersistentID(id string) bool {
	if !strings.HasPrefix(id, "orole-") {
		return false
	}
	u, err := uuid.Parse(strings.TrimPrefix(id, "orole-"))
	return err == nil && id == "orole-"+u.String()
}

func (b linuxBackend) StartPersistent(s StartSpec) (Process, error) {
	if strings.TrimSpace(s.Command) == "" {
		return nil, errors.New("terminals: empty command")
	}
	if s.Cols < 1 || s.Rows < 1 || s.Cols > 1000 || s.Rows > 1000 {
		return nil, errors.New("terminals: invalid size")
	}
	id := "orole-" + uuid.NewString()
	args := []string{"new-session", "-d", "-s", id, "-x", fmt.Sprint(s.Cols), "-y", fmt.Sprint(s.Rows)}
	if s.Cwd != "" {
		args = append(args, "-c", s.Cwd)
	}
	env := s.Env
	if env == nil {
		env = os.Environ()
	}
	// Multiple command arguments are exec'd directly by tmux 3.2+. env -i
	// prevents an old server environment leaking into newly created sessions.
	args = append(args, "--", "/usr/bin/env", "-i")
	args = append(args, terminalEnv(env)...)
	args = append(args, "/bin/sh", "-c", s.Command)
	if _, err := b.tmux(args...); err != nil {
		return nil, err
	}
	// set-option accepts a pane target: the colon makes this an exact session
	// component, not a pane named "=orole-...". Do not reserve a status row.
	if _, err := b.tmux("set-option", "-t", "="+id+":", "status", "off"); err != nil {
		_, _ = b.tmux("kill-session", "-t", "="+id)
		return nil, err
	}
	p, err := b.RestorePersistent(id, s.Cols, s.Rows)
	if err != nil {
		_, _ = b.tmux("kill-session", "-t", "="+id)
		return nil, err
	}
	return p, nil
}

func (b linuxBackend) PersistentSessions() ([]PersistentSession, error) {
	out, err := b.tmux("list-sessions", "-F", "#{session_name}")
	if err != nil {
		// An absent socket/server means there are no durable sessions. A missing
		// executable, permission failure or incompatible version remains an error.
		message := string(out)
		if strings.Contains(message, "no server running on") ||
			(strings.Contains(message, "error connecting to") && strings.Contains(message, "No such file or directory")) {
			return []PersistentSession{}, nil
		}
		return nil, err
	}
	result := make([]PersistentSession, 0)
	for _, id := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if validPersistentID(id) {
			result = append(result, PersistentSession{ID: id})
		}
	}
	return result, nil
}

func (b linuxBackend) RestorePersistent(id string, cols, rows int) (Process, error) {
	if !validPersistentID(id) {
		return nil, errors.New("terminals: invalid persistent session ID")
	}
	if _, err := b.tmux("has-session", "-t", "="+id); err != nil {
		return nil, err
	}
	args, err := b.tmuxArgs("attach-session", "-t", "="+id)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("tmux", args...)
	cmd.Env = tmuxEnv()
	p, err := startLinuxPTY(cmd, cols, rows)
	if err != nil {
		return nil, err
	}
	return &tmuxProc{linuxProc: p, backend: b, id: id}, nil
}

// Close releases only the local client. Explicit Manager.Close additionally
// calls Terminate; pump cleanup and daemon Shutdown must not kill the session.
type tmuxProc struct {
	*linuxProc
	backend linuxBackend
	id      string
}

func (p *tmuxProc) PersistentID() string { return p.id }
func (p *tmuxProc) Terminate() error {
	_, err := p.backend.tmux("kill-session", "-t", "="+p.id)
	if err != nil {
		sessions, listErr := p.backend.PersistentSessions()
		if listErr != nil {
			return err
		}
		for _, session := range sessions {
			if session.ID == p.id {
				return err
			}
		}
	}
	return nil
}

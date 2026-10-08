//go:build linux

package terminals

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// NewBackend returns the native Linux PTY backend. tmux persistence is opt-in.
func NewBackend() Backend { return linuxBackend{} }

type linuxBackend struct {
	tmuxSocket string
}

// Command is a POSIX shell command line on Linux, not a Windows command line.
// Using sh preserves quoting, pipelines and redirections without splitting on spaces.
func (linuxBackend) Start(s StartSpec) (Process, error) {
	if strings.TrimSpace(s.Command) == "" {
		return nil, errors.New("terminals: empty command")
	}
	cmd := exec.Command("/bin/sh", "-c", s.Command)
	cmd.Dir = s.Cwd
	cmd.Env = s.Env
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = terminalEnv(cmd.Env)
	return startLinuxPTY(cmd, s.Cols, s.Rows)
}

func terminalEnv(env []string) []string {
	for _, entry := range env {
		if strings.HasPrefix(entry, "TERM=") && entry != "TERM=" && entry != "TERM=dumb" {
			return env
		}
	}
	return append(append([]string(nil), env...), "TERM=xterm-256color")
}

func (linuxBackend) Shells() []Shell {
	var out []Shell
	for _, sh := range []struct{ id, name string }{{"bash", "Bash"}, {"zsh", "Zsh"}, {"fish", "Fish"}, {"sh", "POSIX shell"}} {
		if path, err := exec.LookPath(sh.id); err == nil {
			out = append(out, Shell{ID: sh.id, Name: sh.name, Command: shellQuote(path) + " -i"})
		}
	}
	return out
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

type linuxProc struct {
	cmd       *exec.Cmd
	master    *os.File
	done      chan struct{}
	code      int
	waitErr   error
	closeOnce sync.Once
	closeErr  error
}

func startLinuxPTY(cmd *exec.Cmd, cols, rows int) (*linuxProc, error) {
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return nil, errors.New("terminals: invalid size")
	}
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, fmt.Errorf("terminals: start PTY: %w", err)
	}
	// Enable Go's poller so Close interrupts a blocked Read even if a child
	// keeps the slave open. The PTY library deliberately leaves this opt-in.
	if err := syscall.SetNonblock(int(master.Fd()), true); err != nil {
		_ = cmd.Process.Kill()
		_ = master.Close()
		_ = cmd.Wait()
		return nil, err
	}
	p := &linuxProc{cmd: cmd, master: master, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		p.code = cmd.ProcessState.ExitCode()
		var exit *exec.ExitError
		if errors.As(p.waitErr, &exit) {
			p.waitErr = nil // Nonzero exit status is a result, not a Wait failure.
		}
		close(p.done)
	}()
	return p, nil
}

func (p *linuxProc) Read(b []byte) (int, error) {
	n, err := p.master.Read(b)
	if errors.Is(err, syscall.EIO) {
		err = io.EOF // Linux returns EIO when the last slave closes.
	}
	return n, err
}

func (p *linuxProc) Write(b []byte) (int, error) { return p.master.Write(b) }
func (p *linuxProc) Pid() int                    { return p.cmd.Process.Pid }

func (p *linuxProc) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return errors.New("terminals: invalid size")
	}
	return pty.Setsize(p.master, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *linuxProc) Wait(ctx context.Context) (int, error) {
	select {
	case <-p.done:
		return p.code, p.waitErr
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

func (p *linuxProc) Close() error {
	p.closeOnce.Do(func() {
		// The slave is the child's controlling terminal. Closing the master
		// hangs up foreground jobs; Kill also handles shells ignoring SIGHUP.
		_ = p.cmd.Process.Kill()
		p.closeErr = p.master.Close()
	})
	return p.closeErr
}

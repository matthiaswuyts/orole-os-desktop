//go:build !windows

package agents

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// job approximates a Windows job object with a process group: kill signals the whole group.
type job struct {
	mu  sync.Mutex
	pid int
}

func newJob() (*job, error) { return &job{}, nil }

func (j *job) assign(pid int) error {
	j.mu.Lock()
	j.pid = pid
	j.mu.Unlock()
	return nil
}

func (j *job) kill() {
	j.mu.Lock()
	pid := j.pid
	j.mu.Unlock()
	if pid > 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
}

func (j *job) close() {}

// shellWrap turns a user-written command line into an sh invocation.
func shellWrap(cmd string) string { return "/bin/sh -c '" + strings.ReplaceAll(cmd, "'", `'\''`) + "'" }

// shellCommand builds a command that runs a user-written command line through sh.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

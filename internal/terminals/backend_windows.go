//go:build windows

package terminals

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

// NewBackend returns the Windows ConPTY backend.
func NewBackend() Backend { return winBackend{} }

type winBackend struct{}

func (winBackend) Start(s StartSpec) (Process, error) {
	opts := []conpty.ConPtyOption{conpty.ConPtyDimensions(s.Cols, s.Rows)}
	if s.Cwd != "" {
		opts = append(opts, conpty.ConPtyWorkDir(s.Cwd))
	}
	if s.Env != nil {
		opts = append(opts, conpty.ConPtyEnv(s.Env))
	}
	cp, err := conpty.Start(s.Command, opts...)
	if err != nil {
		return nil, err
	}
	return &winProc{ConPty: cp}, nil
}

// winProc guards ConPty.Close: closing the pseudoconsole twice corrupts the heap.
type winProc struct {
	*conpty.ConPty
	once sync.Once
	err  error
}

// Close kills the process tree root and releases the pseudoconsole exactly once.
func (p *winProc) Close() error {
	p.once.Do(func() {
		if proc, err := os.FindProcess(p.Pid()); err == nil {
			_ = proc.Kill()
		}
		p.err = p.ConPty.Close()
	})
	return p.err
}

func (p *winProc) Wait(ctx context.Context) (int, error) {
	c, err := p.ConPty.Wait(ctx)
	return int(c), err
}

func (winBackend) Shells() []Shell {
	var out []Shell
	add := func(id, name, exe string, args string) {
		if path, err := exec.LookPath(exe); err == nil {
			out = append(out, Shell{ID: id, Name: name, Command: quote(path) + args})
		}
	}
	// Windows PowerShell 5.1 ships with Windows; pwsh and Git Bash are optional.
	add("powershell", "Windows PowerShell", "powershell.exe", " -NoLogo")
	add("pwsh", "PowerShell 7", "pwsh.exe", " -NoLogo")
	add("cmd", "Command Prompt", "cmd.exe", "")
	for _, p := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "usr", "bin", "bash.exe"),
	} {
		if _, err := os.Stat(p); err == nil {
			out = append(out, Shell{ID: "git-bash", Name: "Git Bash", Command: quote(p) + " -i -l"})
			break
		}
	}
	return out
}

func quote(s string) string { return `"` + s + `"` }

func init() {
	// A parent (shell, service host, CI) may have started us with Ctrl-C
	// ignored; children inherit that flag and a pty's 0x03 would then do
	// nothing. Re-enable normal Ctrl-C handling for this process tree.
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	_, _, _ = k32.NewProc("SetConsoleCtrlHandler").Call(0, 0)
}

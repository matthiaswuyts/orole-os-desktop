//go:build windows

package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

const createNoWindow = 0x08000000

type taskScheduler struct{ run runner }

// NewAutostart returns the Task Scheduler implementation (per-user logon task, no admin).
func NewAutostart() Autostart { return taskScheduler{run: execRun} }

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.CombinedOutput()
}

const mechanism = "Task Scheduler (per-user logon task)"

func (t taskScheduler) Status(ctx context.Context) (AutostartStatus, error) {
	st := AutostartStatus{Supported: true, Mechanism: mechanism, Detail: TaskName}
	out, err := t.run(ctx, "schtasks", "/Query", "/TN", TaskName, "/FO", "LIST")
	if err == nil {
		st.Enabled = true
		return st, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return st, nil // schtasks exits 1 when the task does not exist
	}
	return st, fmt.Errorf("schtasks: %w: %s", err, strings.TrimSpace(string(out)))
}

func (t taskScheduler) Enable(ctx context.Context, daemonExe string) error {
	if !filepath.IsAbs(daemonExe) {
		return fmt.Errorf("daemon path must be absolute, got %q", daemonExe)
	}
	if _, err := os.Stat(daemonExe); err != nil {
		return fmt.Errorf("daemon executable: %w", err)
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "orole-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(utf16le(taskXML(u.Username, daemonExe))); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if out, err := t.run(ctx, "schtasks", "/Create", "/TN", TaskName, "/XML", f.Name(), "/F"); err != nil {
		return fmt.Errorf("schtasks /Create: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (t taskScheduler) Disable(ctx context.Context) error {
	st, err := t.Status(ctx)
	if err != nil {
		return err
	}
	if !st.Enabled {
		return nil
	}
	if out, err := t.run(ctx, "schtasks", "/Delete", "/TN", TaskName, "/F"); err != nil {
		return fmt.Errorf("schtasks /Delete: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// utf16le encodes s as UTF-16LE with a BOM, the encoding schtasks expects for /XML files.
func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2, 2+2*len(u))
	b[0], b[1] = 0xFF, 0xFE
	for _, c := range u {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return b
}

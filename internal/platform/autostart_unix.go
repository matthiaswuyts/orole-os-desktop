//go:build !windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type systemdUser struct {
	run     runner
	unitDir func() (string, error)
}

// NewAutostart returns the systemd user-unit implementation. Orole OS is
// Windows-first; this exists so the interface has a second, honest implementation.
func NewAutostart() Autostart {
	return systemdUser{run: execRun, unitDir: func() (string, error) {
		c, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(c, "systemd", "user"), nil
	}}
}

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (s systemdUser) path() (string, error) {
	d, err := s.unitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, UnitName), nil
}

func (s systemdUser) Status(ctx context.Context) (AutostartStatus, error) {
	st := AutostartStatus{Supported: true, Mechanism: "systemd user unit", Detail: UnitName}
	p, err := s.path()
	if err != nil {
		return st, err
	}
	if _, err := os.Stat(p); err == nil {
		st.Enabled = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return st, err
	}
	return st, nil
}

func (s systemdUser) Enable(ctx context.Context, daemonExe string) error {
	if !filepath.IsAbs(daemonExe) {
		return fmt.Errorf("daemon path must be absolute, got %q", daemonExe)
	}
	p, err := s.path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(systemdUnit(daemonExe)), 0o644); err != nil {
		return err
	}
	if out, err := s.run(ctx, "systemctl", "--user", "enable", UnitName); err != nil {
		return fmt.Errorf("systemctl enable: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s systemdUser) Disable(ctx context.Context) error {
	p, err := s.path()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	_, _ = s.run(ctx, "systemctl", "--user", "disable", UnitName) // best effort; the file removal is what matters
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

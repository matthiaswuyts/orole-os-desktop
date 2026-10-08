//go:build !windows

package platform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdUserEnableDisable(t *testing.T) {
	dir := t.TempDir()
	var calls []string
	a := systemdUser{
		unitDir: func() (string, error) { return dir, nil },
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil, nil
		},
	}
	ctx := context.Background()
	if st, _ := a.Status(ctx); st.Enabled {
		t.Fatal("enabled before install")
	}
	if err := a.Enable(ctx, "/opt/orole/orole-daemon"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, UnitName))
	if err != nil || !strings.Contains(string(b), "ExecStart=/opt/orole/orole-daemon") {
		t.Fatalf("unit not written: %v %s", err, b)
	}
	if st, _ := a.Status(ctx); !st.Enabled {
		t.Fatal("not enabled after install")
	}
	if err := a.Enable(ctx, "relative/path"); err == nil {
		t.Fatal("relative daemon path accepted")
	}
	if err := a.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, UnitName)); err == nil {
		t.Fatal("unit still present")
	}
	if err := a.Disable(ctx); err != nil {
		t.Fatalf("disable of missing unit must succeed: %v", err)
	}
	if len(calls) < 2 {
		t.Fatalf("expected systemctl enable+disable, got %v", calls)
	}
}

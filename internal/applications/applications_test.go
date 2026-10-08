package applications

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type authorizeFunc func(context.Context, policy.Request) error

func (fn authorizeFunc) Require(ctx context.Context, req policy.Request) error { return fn(ctx, req) }

func fixture(t *testing.T, auth Authorizer) (*Manager, SaveRequest, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "applications.db")
	db, err := database.Open(dbPath, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	path := filepath.Join(dir, "native-app.exe")
	if err := os.WriteFile(path, []byte("not executed by these tests"), 0700); err != nil {
		t.Fatal(err)
	}
	return New(db.DB, auth), SaveRequest{Name: "Native app", Executable: path, Arguments: []string{"a b", "& literal"}}, dbPath
}

func TestShortcutsPersistAndRejectStaleEdits(t *testing.T) {
	ctx := context.Background()
	m, req, path := fixture(t, nil)
	created, err := m.Save(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.ID == "" {
		t.Fatalf("created: %+v", created)
	}
	if err := m.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(path, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m = New(db.DB, nil)
	items, err := m.List(ctx)
	if err != nil || len(items) != 1 || items[0].ID != created.ID || len(items[0].Arguments) != 2 || items[0].Arguments[0] != "a b" {
		t.Fatalf("persisted items: %+v, %v", items, err)
	}
	req.ID, req.Revision, req.Name = created.ID, created.Revision, "Renamed app"
	updated, err := m.Save(ctx, req)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("updated: %+v, %v", updated, err)
	}
	if _, err := m.Save(ctx, req); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale edit: %v", err)
	}
	if err := m.Remove(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted get: %v", err)
	}
}

func TestLaunchRequiresPolicyBeforeExecution(t *testing.T) {
	ctx := context.Background()
	m, req, _ := fixture(t, nil)
	s, err := m.Save(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	m.start = func(got Shortcut) (int, error) {
		starts++
		if got.Executable != s.Executable || got.Arguments[1] != "& literal" {
			t.Fatalf("launch changed arguments: %+v", got)
		}
		return 321, nil
	}
	if _, err := m.Launch(ctx, s.ID); !errors.Is(err, policy.ErrDenied) || starts != 0 {
		t.Fatalf("missing policy: starts=%d, err=%v", starts, err)
	}
	m.policy = authorizeFunc(func(_ context.Context, request policy.Request) error {
		if starts != 0 {
			t.Fatal("started before authorization")
		}
		if request.Actor != "user" || request.Action != "applications.launch" || request.Effect != policy.EffectDestructive || request.FromHint {
			t.Fatalf("unsafe request: %+v", request)
		}
		if request.Resource != "applications/"+s.ID+"/revision/1" || !strings.Contains(request.Detail, s.Executable) {
			t.Fatalf("incomplete approval: %+v", request)
		}
		return policy.ErrDenied
	})
	if _, err := m.Launch(ctx, s.ID); !errors.Is(err, policy.ErrDenied) || starts != 0 {
		t.Fatalf("denial: starts=%d, err=%v", starts, err)
	}
	m.policy = authorizeFunc(func(context.Context, policy.Request) error { return nil })
	result, err := m.Launch(ctx, s.ID)
	if err != nil || starts != 1 || result.PID != 321 {
		t.Fatalf("allow: %+v, starts=%d, err=%v", result, starts, err)
	}
}

func TestApprovalDoesNotAuthorizeChangedOrDeletedShortcut(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "edited", true: "deleted"}[remove], func(t *testing.T) {
			ctx := context.Background()
			m, req, _ := fixture(t, nil)
			s, err := m.Save(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			m.start = func(Shortcut) (int, error) { t.Fatal("changed shortcut was executed"); return 0, nil }
			m.policy = authorizeFunc(func(context.Context, policy.Request) error {
				if remove {
					return m.Remove(ctx, s.ID)
				}
				req.ID, req.Revision, req.Arguments = s.ID, s.Revision, []string{"--different"}
				_, err := m.Save(ctx, req)
				return err
			})
			_, err = m.Launch(ctx, s.ID)
			want := ErrChanged
			if remove {
				want = ErrNotFound
			}
			if !errors.Is(err, want) {
				t.Fatalf("launch: %v; want %v", err, want)
			}
		})
	}
}

func TestCanceledApprovalCannotStartProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, req, _ := fixture(t, authorizeFunc(func(context.Context, policy.Request) error { cancel(); return nil }))
	s, err := m.Save(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	m.start = func(Shortcut) (int, error) { t.Fatal("canceled launch started"); return 0, nil }
	if _, err := m.Launch(ctx, s.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestLauncherRejectsUnsafeInput(t *testing.T) {
	m, base, _ := fixture(t, nil)
	for _, tc := range []struct {
		name   string
		change func(*SaveRequest)
	}{
		{"relative executable", func(r *SaveRequest) { r.Executable = "cmd.exe" }},
		{"URL", func(r *SaveRequest) { r.Executable = "https://example.com/app.exe" }},
		{"directory", func(r *SaveRequest) { r.Executable = filepath.Dir(r.Executable) }},
		{"control name", func(r *SaveRequest) { r.Name = "app\nspoofed" }},
		{"control argument", func(r *SaveRequest) { r.Arguments = []string{"\x00"} }},
		{"many arguments", func(r *SaveRequest) { r.Arguments = make([]string, 65) }},
		{"truncated approval", func(r *SaveRequest) { r.Arguments = []string{strings.Repeat("x", 1800)} }},
		{"relative directory", func(r *SaveRequest) { r.WorkingDirectory = "relative" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.change(&req)
			if _, err := m.Save(context.Background(), req); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid input: %v", err)
			}
		})
	}
}

func TestExecutableRemovedDuringApprovalDoesNotLaunch(t *testing.T) {
	m, req, _ := fixture(t, nil)
	s, err := m.Save(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	m.policy = authorizeFunc(func(context.Context, policy.Request) error { return os.Remove(s.Executable) })
	m.start = func(Shortcut) (int, error) { t.Fatal("missing executable started"); return 0, nil }
	if _, err := m.Launch(context.Background(), s.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing executable: %v", err)
	}
}

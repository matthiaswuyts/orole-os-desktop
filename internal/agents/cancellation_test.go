package agents

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func cancellationManager(t *testing.T, authorize func(context.Context, string, string, string) error) *Manager {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "agents.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m := NewManager(Config{Store: NewStore(db), Authorize: authorize})
	t.Cleanup(m.Shutdown)
	return m
}

func TestCancelledQueuedStartDoesNotCreateSession(t *testing.T) {
	authorized := make(chan struct{})
	m := cancellationManager(t, func(context.Context, string, string, string) error { close(authorized); return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.launchMu.Lock()
	result := make(chan error, 1)
	go func() {
		_, err := m.Start(ctx, StartSpec{Kind: "claude", Dir: t.TempDir(), NewWorktree: true})
		result <- err
	}()
	<-authorized
	cancel()
	m.launchMu.Unlock()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued start: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued start did not return")
	}
	sessions, err := m.List()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("cancelled start persisted sessions: %v %v", sessions, err)
	}
}

func TestCancelledRelaunchWaitDoesNotDispatch(t *testing.T) {
	m := cancellationManager(t, func(context.Context, string, string, string) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	m.launchMu.Lock()
	result := make(chan error, 1)
	go func() { _, err := m.relaunch(ctx, "absent", "task", false); result <- err }()
	cancel()
	m.launchMu.Unlock()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled relaunch: %v", err)
	}
}

func TestCancelledPermissionWaitDoesNotDispatchCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := cancellationManager(t, func(context.Context, string, string, string) error { cancel(); return nil })
	now := time.Now().UTC()
	s := Session{ID: "check", Kind: "claude", Dir: t.TempDir(), Status: StatusStopped, StartedAt: now, UpdatedAt: now}
	if err := m.st.SaveSession(s, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Check(ctx, s.ID, "echo must-not-run"); !errors.Is(err, context.Canceled) {
		t.Fatalf("check: %v", err)
	}
	got, err := m.Get(s.ID)
	if err != nil || got.CheckState != "" || got.CheckCommand != "" {
		t.Fatalf("cancelled check dispatched: %+v %v", got, err)
	}
}

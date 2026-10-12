package daemon_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
)

func start(t *testing.T, dir string) (stop func()) {
	t.Helper()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	for i := 0; i < 100; i++ {
		if _, err := control.Running(context.Background(), dir); err == nil {
			return func() { d.Shutdown(); <-done }
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("daemon did not start")
	return nil
}

func TestStatePersistsAcrossDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	stop := start(t, dir)
	ctx := context.Background()
	c := control.NewClient(dir, "test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "settings.set", map[string]any{"key": "theme", "value": "midnight"}, nil); err != nil {
		t.Fatal(err)
	}
	c.Close()
	stop()
	if _, err := control.Running(ctx, dir); err == nil {
		t.Fatal("daemon still answering after stop")
	}

	stop = start(t, dir) // new process-equivalent: new token, same DB
	defer stop()
	c = control.NewClient(dir, "test") // must pick up the fresh token
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var settings map[string]json.RawMessage
	if err := c.Call(ctx, "settings.list", nil, &settings); err != nil || string(settings["theme"]) != `"midnight"` {
		t.Fatalf("settings: %v %v", settings, err)
	}
}

func TestSecondDaemonRefusesToStart(t *testing.T) {
	dir := t.TempDir()
	stop := start(t, dir)
	defer stop()
	d2, err := daemon.New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	d2.Endpoint = control.NewClientEndpoint(dir) // contend for the first daemon's endpoint
	if err := d2.Run(context.Background()); err == nil {
		t.Fatal("second daemon on the same endpoint must fail")
	}
}

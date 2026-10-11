package daemon_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
)

// YOLO is gated in the daemon: it needs the explicit confirmation flag and the "agents" setting
// must not turn it off. Detection reports the same, and the switch method is wired through IPC.
func TestAgentsIPCYoloGateAndSetPermissionMode(t *testing.T) {
	dir := t.TempDir()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := control.NewClient(dir, "agents-yolo-test")
	defer c.Close()
	for {
		if err = c.Connect(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	spec := agents.StartSpec{Kind: "claude", Dir: t.TempDir(), PermissionMode: "yolo"}
	var s agents.Session
	if err = c.Call(ctx, "agents.start", spec, &s); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("yolo without confirmation: %v", err)
	}
	if err = c.Call(ctx, "settings.set", map[string]any{"key": "agents", "value": map[string]any{"yoloDisabled": true}}, nil); err != nil {
		t.Fatal(err)
	}
	spec.ConfirmYolo = true
	if err = c.Call(ctx, "agents.start", spec, &s); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("yolo while disabled in settings: %v", err)
	}
	var detections []agents.Detection
	if err = c.Call(ctx, "agents.detect", map[string]bool{"refresh": true}, &detections); err != nil {
		t.Fatal(err)
	}
	for _, det := range detections {
		for _, m := range det.PermissionModes {
			if m.Mode == "yolo" && m.Available {
				t.Errorf("%s: yolo advertised while disabled", det.Kind)
			}
		}
	}
	if err = c.Call(ctx, "agents.setPermissionMode", map[string]any{"id": "missing", "mode": "auto"}, &s); err == nil || !strings.Contains(err.Error(), "no such session") {
		t.Fatalf("unknown session: %v", err)
	}
	if err = c.Call(ctx, "agents.setPermissionMode", map[string]any{"id": "missing", "mode": "bypassPermissions"}, &s); err == nil || !strings.Contains(err.Error(), "must be manual, auto or yolo") {
		t.Fatalf("legacy mode: %v", err)
	}
	var count int
	if err = d.DB.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='agent.yolo' AND outcome='denied'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("denied yolo attempts audited: %d %v", count, err)
	}
}

package daemon_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
)

// TestAutomationCommandNotification exercises the real named-pipe daemon and
// actual go executable, not fake integration data. Set OROLE_AUTOMATION_SMOKE=1
// and OROLE_HOME to an isolated development directory to retain the journal.
func TestAutomationCommandNotification(t *testing.T) {
	if os.Getenv("OROLE_AUTOMATION_SMOKE") != "1" {
		t.Skip("opt-in real daemon workflow smoke")
	}
	dir := os.Getenv("OROLE_HOME")
	if dir == "" {
		t.Fatal("OROLE_HOME must name an isolated development directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := control.Running(ctx, dir); err != nil {
		d, err := daemon.New(dir, slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- d.Run(ctx) }()
		ready := false
		for !ready {
			select {
			case err := <-done:
				t.Fatalf("daemon startup: %v", err)
			case <-ctx.Done():
				d.Shutdown()
				<-done
				t.Fatal(ctx.Err())
			case <-time.After(50 * time.Millisecond):
				_, err = control.Running(ctx, dir)
				ready = err == nil
			}
		}
		defer func() {
			d.Shutdown()
			if err := <-done; err != nil {
				t.Error(err)
			}
		}()
	}
	client := control.NewClient(dir, "automation-smoke")
	if err := client.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cwd, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]any{"executable": "go", "args": []string{"version"}, "dir": cwd})
	definition := automations.Definition{Name: "Automation smoke: Go version", AllowedExecutables: []string{"go"}, Steps: []automations.Step{{ID: "version", Name: "Go version", Type: "command.run", Params: params}, {ID: "notify", Name: "Notify result", Type: "notify", Params: json.RawMessage(`{"title":"Automation smoke succeeded","body":"{{ steps.version.output.stdout }}","level":"success"}`)}}}
	var saved automations.SaveResult
	if err = client.Call(ctx, "automations.save", definition, &saved); err != nil || !saved.Saved {
		t.Fatalf("save %v %+v", err, saved.Issues)
	}
	var run automations.Run
	if err = client.Call(ctx, "automations.run", map[string]string{"id": saved.Definition.ID}, &run); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		var detail automations.Detail
		if err = client.Call(ctx, "automations.run.detail", map[string]string{"id": run.ID}, &detail); err != nil {
			t.Fatal(err)
		}
		switch detail.Run.Status {
		case "succeeded":
			b, _ := json.MarshalIndent(detail, "", "  ")
			t.Logf("REAL DAEMON JOURNAL:\n%s", b)
			if len(detail.Steps) != 2 {
				t.Fatal("expected command and notification journal rows")
			}
			return
		case "failed", "canceled", "uncertain":
			t.Fatalf("workflow %+v", detail)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(ctx.Err())
}

package daemon_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

func TestAgentsIPCRecoveryTerminalAndEvents(t *testing.T) {
	dir := t.TempDir()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	err = d.Agents.Store().SaveSession(agents.Session{ID: "orphan-ipc", Kind: "claude", Machine: "local", Dir: t.TempDir(), Mode: agents.ModeStructured, Status: agents.StatusWorking, AgentSessionID: "prior-cli-id", StartedAt: now, UpdatedAt: now}, "")
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
	c := control.NewClient(dir, "agents-test")
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
	eventCh := make(chan string, 64)
	approvalCh := make(chan policy.Pending, 1)
	c.OnEvent(func(name string, data json.RawMessage) {
		if name == "policy.pending" {
			var snapshot struct {
				Items []policy.Pending `json:"items"`
			}
			if json.Unmarshal(data, &snapshot) == nil {
				for _, p := range snapshot.Items {
					if p.Action == "agent.start" {
						select {
						case approvalCh <- p:
						default:
						}
					}
				}
			}
			return
		}
		if !strings.HasPrefix(name, "agents.") {
			return
		}
		select {
		case eventCh <- name:
		default:
		}
	})
	if err = c.Subscribe(ctx, "agents.*", "policy.*"); err != nil {
		t.Fatal(err)
	}
	var sessions []agents.Session
	if err = c.Call(ctx, "agents.list", nil, &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Status != agents.StatusLost {
		t.Fatalf("recovery list: %+v", sessions)
	}
	var s agents.Session
	agentDir := t.TempDir()
	started := make(chan error, 1)
	go func() {
		started <- c.Call(ctx, "agents.start", agents.StartSpec{Kind: "custom", Dir: agentDir, Command: "ping -n 30 127.0.0.1", Key: "ipc-start-once"}, &s)
	}()
	select {
	case p := <-approvalCh:
		if p.Resource != "local:"+agentDir || p.CanRemember {
			t.Fatalf("unexpected execution approval: %+v", p)
		}
		var before []agents.Session
		if err := c.Call(ctx, "agents.list", nil, &before); err != nil || len(before) != 1 {
			t.Fatalf("agent launched before approval: %+v %v", before, err)
		}
		if err := c.Call(ctx, "policy.resolve", map[string]any{"id": p.ID, "allow": true, "remember": false}, nil); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("execution approval not published")
	}
	if err = <-started; err != nil {
		t.Fatal(err)
	}
	if s.TerminalID == "" || !s.Live {
		t.Fatalf("start: %+v", s)
	}
	var attached terminals.Attachment
	if err = c.Call(ctx, "terminals.attach", map[string]string{"id": s.TerminalID}, &attached); err != nil {
		t.Fatal(err)
	}
	if attached.Info.Owner != "agent:"+s.ID {
		t.Fatalf("terminal owner %q", attached.Info.Owner)
	}
	var again agents.Session
	if err = c.Call(ctx, "agents.start", agents.StartSpec{Key: "ipc-start-once"}, &again); err != nil || again.ID != s.ID {
		t.Fatalf("idempotency: %+v %v", again, err)
	}
	if err = c.Call(ctx, "agents.approve", map[string]any{"id": s.ID, "requestId": "forged", "allow": true}, nil); err == nil {
		t.Fatal("accepted unknown permission request")
	}
	if err = c.Call(ctx, "agents.stop", map[string]string{"id": s.ID}, &s); err != nil {
		t.Fatal(err)
	}
	if s.Live || s.Status != agents.StatusStopped {
		t.Fatalf("stop: %+v", s)
	}
	var evs []agents.Event
	if err = c.Call(ctx, "agents.events", map[string]any{"id": s.ID, "after": 0, "limit": 100}, &evs); err != nil {
		t.Fatal(err)
	}
	if len(evs) == 0 {
		t.Fatal("no durable process event")
	}
	select {
	case topic := <-eventCh:
		if !strings.HasPrefix(topic, "agents.") {
			t.Fatal(topic)
		}
	case <-ctx.Done():
		t.Fatal("no agent update broadcast")
	}
	var count int
	if err = d.DB.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE target=? AND action IN ('agent.start','agent.stop')`, s.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
}

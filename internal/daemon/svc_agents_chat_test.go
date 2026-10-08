package daemon_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
)

// This exercises real local IPC and SQLite without starting a model process.
func TestAgentsChatIPCQueueNeverLaunches(t *testing.T) {
	dir := t.TempDir()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := agents.Session{ID: "chat-ipc", Kind: "claude", Machine: "local", Dir: t.TempDir(), Mode: agents.ModeStructured, Status: agents.StatusStopped, StartedAt: now, UpdatedAt: now}
	if err := d.Agents.Store().SaveSession(s, ""); err != nil {
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
	c := control.NewClient(dir, "chat-ipc-test")
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
	if err := c.Call(ctx, "agents.queuePrompt", map[string]string{"id": s.ID, "text": "Durable follow-up, do not launch"}, &s); err != nil {
		t.Fatal(err)
	}
	if s.Live || len(s.Queue) != 1 || s.Queue[0].State != "paused" {
		t.Fatalf("unexpected queued state: %+v", s)
	}
	if err := c.Call(ctx, "agents.resumeQueue", map[string]string{"id": s.ID}, &s); err == nil {
		t.Fatal("queue resume restarted the process")
	}
	if err := c.Call(ctx, "agents.answerInput", map[string]any{"id": s.ID, "requestId": "forged", "answers": []agents.QuestionAnswer{}, "cancel": false}, nil); err == nil {
		t.Fatal("forged question accepted")
	}
	if err := c.Call(ctx, "agents.answerInput", map[string]any{"id": s.ID, "requestId": "forged", "answers": true, "cancel": false}, nil); err == nil {
		t.Fatal("Boolean accepted by typed question IPC")
	}
	var fresh agents.Session
	if err := c.Call(ctx, "agents.get", map[string]string{"id": "chat-ipc"}, &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.Live || len(fresh.Queue) != 1 {
		t.Fatal("failed control changed queue or launched a process")
	}
	var cancelled agents.Session
	if err := c.Call(ctx, "agents.cancelQueuedPrompt", map[string]string{"id": fresh.ID, "queueId": fresh.Queue[0].ID}, &cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled.Live || len(cancelled.Queue) != 0 {
		t.Fatal("IPC queue cancellation failed")
	}
	stored, err := d.Agents.Store().GetSession(fresh.ID)
	if err != nil || len(stored.Queue) != 0 {
		t.Fatalf("queue cancellation was not durable: %+v %v", stored, err)
	}
}

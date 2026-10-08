package ipc_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

func setup(t *testing.T) (*ipc.Server, string) {
	t.Helper()
	ep := ipc.Endpoint(t.TempDir())
	if ep[0] != '\\' {
		ep = filepath.Join(t.TempDir(), "s.sock")
	}
	ln, err := ipc.Listen(ep)
	if err != nil {
		t.Fatal(err)
	}
	s := ipc.NewServer("secret", "test", 1, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Handle("echo", func(ctx context.Context, p json.RawMessage) (any, error) { return json.RawMessage(p), nil })
	s.Handle("slow", func(ctx context.Context, p json.RawMessage) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	go s.Serve(ln)
	t.Cleanup(func() { ln.Close(); s.CloseAll() })
	return s, ep
}

func TestAuthRequiredAndEcho(t *testing.T) {
	_, ep := setup(t)
	bad := ipc.NewClient(ipc.Dial(ep), "wrong", "t")
	var e *ipc.Error
	if err := bad.Connect(context.Background()); !errors.As(err, &e) || e.Code != ipc.CodeUnauthenticated {
		t.Fatalf("want unauthenticated, got %v", err)
	}
	c := ipc.NewClient(ipc.Dial(ep), "secret", "t")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var out map[string]int
	if err := c.Call(context.Background(), "echo", map[string]int{"a": 1}, &out); err != nil || out["a"] != 1 {
		t.Fatalf("echo: %v %v", out, err)
	}
	if err := c.Call(context.Background(), "nope", nil, nil); !errors.As(err, &e) || e.Code != ipc.CodeNotFound {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestCancelPropagates(t *testing.T) {
	_, ep := setup(t)
	c := ipc.NewClient(ipc.Dial(ep), "secret", "t")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := c.Call(ctx, "slow", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("did not cancel promptly")
	}
}

func TestEventsAndReconnect(t *testing.T) {
	s, ep := setup(t)
	c := ipc.NewClient(ipc.Dial(ep), "secret", "t")
	got := make(chan string, 4)
	c.OnEvent(func(ev string, _ json.RawMessage) { got <- ev })
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Subscribe(context.Background(), "agent.*"); err != nil {
		t.Fatal(err)
	}
	s.Publish("mail.new", 1) // not subscribed
	s.Publish("agent.updated", 1)
	select {
	case ev := <-got:
		if ev != "agent.updated" {
			t.Fatalf("got %s", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
	s.CloseAll() // server drops us; next Call must transparently reconnect and resubscribe
	time.Sleep(100 * time.Millisecond)
	var out map[string]int
	if err := c.Call(context.Background(), "echo", map[string]int{"a": 2}, &out); err != nil || out["a"] != 2 {
		t.Fatalf("after reconnect: %v %v", out, err)
	}
	s.Publish("agent.updated", 2)
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription not restored")
	}
}

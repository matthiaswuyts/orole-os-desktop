package ipc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

// countedHandler registers a method that counts executions and returns
// whatever result/err it is given.
func countedHandler(s *ipc.Server, method string, res any, err error) *atomic.Int32 {
	var n atomic.Int32
	s.Handle(method, func(ctx context.Context, p json.RawMessage) (any, error) {
		n.Add(1)
		return res, err
	})
	return &n
}

func connect(t *testing.T, ep string) *ipc.Client {
	t.Helper()
	c := ipc.NewClient(ipc.Dial(ep), "secret", "t")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *ipc.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want error code %q, got %v", code, err)
	}
}

// An oversized reply must come back as too_large on the same connection and
// the handler must run exactly once (the client used to see a dead link,
// reconnect and re-send, running the side effect twice).
func TestOversizedReplyRunsOnce(t *testing.T) {
	s, ep := setup(t)
	big := strings.Repeat("x", ipc.MaxMessageBytes+1)
	n := countedHandler(s, "big", big, nil)
	c := connect(t, ep)

	err := c.Call(context.Background(), "big", nil, nil)
	if got := n.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1 (call err: %v)", got, err)
	}
	wantCode(t, err, "too_large")
	// Same link still healthy: no reconnect needed.
	var out map[string]int
	if err := c.Call(context.Background(), "echo", map[string]int{"a": 1}, &out); err != nil || out["a"] != 1 {
		t.Fatalf("echo after too_large: %v %v", out, err)
	}
}

func TestReplyAtFrameBoundary(t *testing.T) {
	s, ep := setup(t)
	// The quoted string plus the envelope must fit; leave generous headroom.
	ok := strings.Repeat("x", ipc.MaxMessageBytes-1024)
	n := countedHandler(s, "ok", ok, nil)
	c := connect(t, ep)
	var out string
	if err := c.Call(context.Background(), "ok", nil, &out); err != nil || len(out) != len(ok) {
		t.Fatalf("near-limit reply: len=%d err=%v", len(out), err)
	}
	if n.Load() != 1 {
		t.Fatalf("ran %d times", n.Load())
	}
}

func TestOversizedErrorMessageIsTruncated(t *testing.T) {
	s, ep := setup(t)
	n := countedHandler(s, "boom", nil, errors.New(strings.Repeat("e", ipc.MaxMessageBytes+10)))
	c := connect(t, ep)
	err := c.Call(context.Background(), "boom", nil, nil)
	wantCode(t, err, ipc.CodeInternal)
	if n.Load() != 1 {
		t.Fatalf("ran %d times", n.Load())
	}
}

func TestOversizedEventDropped(t *testing.T) {
	s, ep := setup(t)
	c := connect(t, ep)
	got := make(chan json.RawMessage, 4)
	c.OnEvent(func(ev string, d json.RawMessage) { got <- d })
	if err := c.Subscribe(context.Background(), "*"); err != nil {
		t.Fatal(err)
	}
	s.Publish("big", strings.Repeat("x", ipc.MaxMessageBytes+1))
	s.Publish("small", 7)
	select {
	case d := <-got:
		if string(d) != "7" {
			t.Fatalf("got %.40s, want the small event", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
	if !c.Connected() {
		t.Fatal("oversized event dropped the connection")
	}
}

type countMarshal struct{ n *atomic.Int32 }

func (c countMarshal) MarshalJSON() ([]byte, error) { c.n.Add(1); return []byte("1"), nil }

func TestPublishSkipsMarshalWithoutSubscribers(t *testing.T) {
	s, ep := setup(t)
	var n atomic.Int32
	s.Publish("x", countMarshal{&n})
	if n.Load() != 0 {
		t.Fatalf("marshalled %d times with no connections", n.Load())
	}
	c := connect(t, ep)
	s.Publish("x", countMarshal{&n}) // connected but not subscribed
	if n.Load() != 0 {
		t.Fatalf("marshalled %d times with no subscribers", n.Load())
	}
	got := make(chan struct{}, 2)
	c.OnEvent(func(string, json.RawMessage) { got <- struct{}{} })
	if err := c.Subscribe(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	s.Publish("x", countMarshal{&n})
	<-got
	if n.Load() != 1 {
		t.Fatalf("marshalled %d times, want once", n.Load())
	}
}

// If the link drops after the request was written, the client cannot know
// whether the daemon executed it, so Call must not re-send.
func TestCallDoesNotResendWrittenRequest(t *testing.T) {
	s, ep := setup(t)
	var n atomic.Int32
	s.Handle("dropper", func(ctx context.Context, p json.RawMessage) (any, error) {
		if n.Add(1) == 1 {
			s.CloseAll() // link dies before the reply goes out
		}
		return "ok", nil
	})
	c := connect(t, ep)
	wantCode(t, c.Call(context.Background(), "dropper", nil, nil), "disconnected")
	time.Sleep(100 * time.Millisecond)
	if got := n.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1", got)
	}
	// The next call is a fresh request and reconnects.
	var out string
	if err := c.Call(context.Background(), "dropper", nil, &out); err != nil || out != "ok" {
		t.Fatalf("after reconnect: %q %v", out, err)
	}
}

// A request that was never written (stale link) is safe to retry once.
func TestCallRetriesWhenNeverWritten(t *testing.T) {
	s, ep := setup(t)
	n := countedHandler(s, "once", "ok", nil)
	c := connect(t, ep)
	s.CloseAll()
	time.Sleep(100 * time.Millisecond) // client read loop notices
	var out string
	if err := c.Call(context.Background(), "once", nil, &out); err != nil || out != "ok" {
		t.Fatalf("%q %v", out, err)
	}
	if n.Load() != 1 {
		t.Fatalf("ran %d times", n.Load())
	}
}

// A frame the client rejects must close the link rather than leave a socket
// nobody reads from.
func TestClientClosesConnOnOversizedFrame(t *testing.T) {
	cs, ss := net.Pipe()
	closed := make(chan error, 1)
	go func() {
		defer ss.Close()
		br := bufio.NewReader(ss)
		if _, err := br.ReadBytes('\n'); err != nil { // hello
			closed <- err
			return
		}
		ss.Write([]byte(`{"id":1,"result":{"version":1}}` + "\n"))
		go func() {
			// Write the oversized frame; it fails once the client hangs up.
			ss.Write([]byte(strings.Repeat("x", ipc.MaxMessageBytes+4096) + "\n"))
		}()
		_, err := io.Copy(io.Discard, br) // returns when the client closes
		closed <- err
	}()
	c := ipc.NewClient(func(context.Context) (net.Conn, error) { return cs, nil }, "secret", "t")
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("client left the connection open after rejecting an oversized frame")
	}
	if c.Connected() {
		t.Fatal("client still reports connected")
	}
}

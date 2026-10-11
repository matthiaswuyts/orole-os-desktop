package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

// floodProc is a pty that emits `reads` 32 KiB chunks as fast as it is read, then idles.
type floodProc struct {
	remaining atomic.Int64
	closed    chan struct{}
	once      sync.Once
}

func (p *floodProc) Read(b []byte) (int, error) {
	if p.remaining.Add(-1) >= 0 {
		n := min(len(b), 32<<10)
		for i := range n {
			b[i] = 'x'
		}
		return n, nil
	}
	<-p.closed
	return 0, io.EOF
}
func (p *floodProc) Write(b []byte) (int, error) { return len(b), nil }
func (p *floodProc) Resize(int, int) error       { return nil }
func (p *floodProc) Pid() int                    { return 1 }
func (p *floodProc) Close() error                { p.once.Do(func() { close(p.closed) }); return nil }
func (p *floodProc) Wait(ctx context.Context) (int, error) {
	select {
	case <-p.closed:
	case <-ctx.Done():
	}
	return 0, nil
}

type floodBackend struct{ reads int }

func (b floodBackend) Start(terminals.StartSpec) (terminals.Process, error) {
	p := &floodProc{closed: make(chan struct{})}
	p.remaining.Store(int64(b.reads))
	return p, nil
}
func (floodBackend) Shells() []terminals.Shell {
	return []terminals.Shell{{ID: "fake", Name: "fake", Command: "fake"}}
}

// A terminal flooding faster than a slow GUI can consume must not get the GUI
// connection closed: output is coalesced and rate-capped, the oldest undelivered
// bytes are dropped (seq jumps), and the tail still arrives so the client can
// re-Attach.
func TestTerminalFloodKeepsConnectionAlive(t *testing.T) {
	const reads = 1024 // 32 MiB
	const total = uint64(reads * (32 << 10))
	ep := ipc.Endpoint(t.TempDir())
	ln, err := ipc.Listen(ep)
	if err != nil {
		t.Fatal(err)
	}
	srv := ipc.NewServer("secret", "test", 1, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.Handle("echo", func(ctx context.Context, p json.RawMessage) (any, error) { return json.RawMessage(p), nil })
	go srv.Serve(ln)
	t.Cleanup(func() { ln.Close(); srv.CloseAll() })

	d := &Daemon{IPC: srv}
	m := terminals.NewManager(floodBackend{reads: reads}, termSink{d})
	t.Cleanup(m.Shutdown)

	var mu sync.Mutex
	var events int
	var delivered int64
	var last uint64
	cl := ipc.NewClient(ipc.Dial(ep), "secret", "gui")
	cl.OnEvent(func(event string, data json.RawMessage) {
		if event != "terminal.output" {
			return
		}
		var o outputEvent
		if json.Unmarshal(data, &o) != nil {
			return
		}
		time.Sleep(20 * time.Millisecond) // a GUI that handles ~50 events/s
		mu.Lock()
		events++
		delivered += int64(len(o.Data))
		last = o.Seq
		mu.Unlock()
	})
	ctx := context.Background()
	if err := cl.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	if err := cl.Subscribe(ctx, "terminal.*"); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if _, err := m.Create(terminals.CreateOptions{Shell: "fake"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		mu.Lock()
		got := last
		mu.Unlock()
		if got == total {
			break
		}
		if !cl.Connected() {
			mu.Lock()
			t.Fatalf("GUI connection was closed during the flood after %d events (%d bytes, seq %d)", events, delivered, last)
		}
		if time.Now().After(deadline) {
			mu.Lock()
			t.Fatalf("tail never delivered: events=%d seq=%d want %d", events, last, total)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cl.Call(ctx, "echo", map[string]int{"a": 1}, nil); err != nil {
		t.Fatalf("call after flood: %v", err)
	}
	mu.Lock()
	t.Logf("32 MiB flood vs 20ms/event consumer: events=%d delivered=%d KiB connected=%v elapsed=%v",
		events, delivered>>10, cl.Connected(), time.Since(start).Round(time.Millisecond))
	mu.Unlock()
}

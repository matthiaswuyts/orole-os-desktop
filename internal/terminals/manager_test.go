package terminals_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

func idleBackend() *fakeBackend {
	return &fakeBackend{next: func() *fakeProc { return newFakeProc(1, 0) }}
}

// Exited sessions do not occupy the 64-session cap and are retained only up
// to a small bound.
func TestExitedSessionsAreNotCountedAndAreReaped(t *testing.T) {
	be := idleBackend()
	sink := &countSink{}
	m := terminals.NewManager(be, sink)
	t.Cleanup(m.Shutdown)

	var ids []string
	for range 64 {
		info, err := m.Create(terminals.CreateOptions{Shell: "fake"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, info.ID)
	}
	if _, err := m.Create(terminals.CreateOptions{Shell: "fake"}); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("65th running session: err=%v, want limit error", err)
	}
	for _, p := range be.procs {
		p.exit <- 0
	}
	if !eventually(10*time.Second, func() bool { return len(sink.changedExited()) == 64 }) {
		t.Fatal("sessions did not all report exit")
	}
	// All 64 slots are free again even though the exited sessions are still listed.
	for range 10 {
		if _, err := m.Create(terminals.CreateOptions{Shell: "fake"}); err != nil {
			t.Fatalf("create after exits: %v", err)
		}
	}
	var exited int
	for _, in := range m.List() {
		if in.State == terminals.Exited {
			exited++
		}
	}
	if exited != 16 {
		t.Fatalf("retained %d exited sessions, want 16", exited)
	}
	sink.mu.Lock()
	removed := len(sink.removed)
	sink.mu.Unlock()
	if removed != 64-16 {
		t.Fatalf("Removed called %d times, want %d", removed, 64-16)
	}
	_ = ids
}

func TestExitedSessionExpires(t *testing.T) {
	be := idleBackend()
	sink := &countSink{}
	m := terminals.NewManager(be, sink)
	t.Cleanup(m.Shutdown)
	m.SetExitedPolicy(4, 100*time.Millisecond)
	info, err := m.Create(terminals.CreateOptions{Shell: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	be.procs[0].exit <- 3
	if !eventually(5*time.Second, func() bool { _, err := m.Attach(info.ID); return err != nil }) {
		t.Fatal("exited session never expired")
	}
	if !eventually(time.Second, func() bool {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		return len(sink.removed) == 1 && sink.removed[0] == info.ID
	}) {
		t.Fatalf("Removed not reported: %v", sink.removed)
	}
}

func TestWait(t *testing.T) {
	t.Run("exit code", func(t *testing.T) {
		be := idleBackend()
		m := terminals.NewManager(be, nil)
		t.Cleanup(m.Shutdown)
		info, _ := m.Create(terminals.CreateOptions{Shell: "fake"})
		got := make(chan terminals.Info, 1)
		go func() {
			in, err := m.Wait(context.Background(), info.ID)
			if err != nil {
				t.Error(err)
			}
			got <- in
		}()
		select {
		case <-got:
			t.Fatal("Wait returned before the process exited")
		case <-time.After(50 * time.Millisecond):
		}
		be.procs[0].exit <- 7
		select {
		case in := <-got:
			if in.State != terminals.Exited || in.ExitCode == nil || *in.ExitCode != 7 {
				t.Fatalf("got %+v", in)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Wait did not return after exit")
		}
		// Waiting again on an already exited (retained) session returns at once.
		if in, err := m.Wait(context.Background(), info.ID); err != nil || *in.ExitCode != 7 {
			t.Fatalf("second Wait: %+v %v", in, err)
		}
	})
	t.Run("closed", func(t *testing.T) {
		m := terminals.NewManager(idleBackend(), nil)
		t.Cleanup(m.Shutdown)
		info, _ := m.Create(terminals.CreateOptions{Shell: "fake"})
		errc := make(chan error, 1)
		go func() { _, err := m.Wait(context.Background(), info.ID); errc <- err }()
		time.Sleep(20 * time.Millisecond)
		if err := m.Close(info.ID); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-errc:
			if !errors.Is(err, terminals.ErrClosed) {
				t.Fatalf("err=%v, want ErrClosed", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Wait did not return after Close")
		}
	})
	t.Run("unknown and canceled", func(t *testing.T) {
		m := terminals.NewManager(idleBackend(), nil)
		t.Cleanup(m.Shutdown)
		if _, err := m.Wait(context.Background(), "nope"); err == nil {
			t.Fatal("want error for unknown session")
		}
		info, _ := m.Create(terminals.CreateOptions{Shell: "fake"})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		if _, err := m.Wait(ctx, info.ID); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err=%v", err)
		}
	})
}

// A burst that fits the pending budget is delivered whole, in order, with no
// gap, and in a handful of events.
func TestSmallBurstIsDeliveredIntact(t *testing.T) {
	const chunk, reads = 4 << 10, 40 // 160 KiB
	be := &fakeBackend{next: func() *fakeProc { return newFakeProc(chunk, reads) }}
	sink := &patternSink{}
	m := terminals.NewManager(be, sink)
	t.Cleanup(m.Shutdown)
	if _, err := m.Create(terminals.CreateOptions{Shell: "fake"}); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { return sink.end() == chunk*reads }) {
		t.Fatalf("delivered %d of %d bytes", sink.end(), chunk*reads)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.bad != "" {
		t.Fatal(sink.bad)
	}
	if sink.events > 6 {
		t.Fatalf("%d events for 160 KiB, want a handful", sink.events)
	}
}

// Attach during a flood returns bytes that match the stream and a Seq that
// no delivered event straddles, so the client can splice live events on.
func TestAttachDuringFloodIsConsistent(t *testing.T) {
	const reads = 1500 // ~47 MiB
	be := &fakeBackend{next: func() *fakeProc { return newFakeProc(32<<10, reads) }}
	sink := &patternSink{}
	m := terminals.NewManager(be, sink)
	t.Cleanup(m.Shutdown)
	info, err := m.Create(terminals.CreateOptions{Shell: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	var seqs []uint64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for sink.end() < reads*(32<<10) {
			a, err := m.Attach(info.ID)
			if err != nil {
				t.Error(err)
				return
			}
			seqs = append(seqs, a.Seq)
			from := int(a.Seq) - len(a.Data)
			for i, b := range a.Data {
				if b != patternByte(from+i) {
					t.Errorf("attach byte %d (stream %d) corrupt", i, from+i)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.bad != "" {
		t.Fatal(sink.bad)
	}
	for _, s := range seqs {
		for _, e := range sink.spans {
			if e[0] < s && s < e[1] {
				t.Fatalf("attach seq %d falls inside delivered event [%d,%d)", s, e[0], e[1])
			}
		}
	}
	if len(seqs) < 5 {
		t.Fatalf("only %d attaches ran during the flood", len(seqs))
	}
}

// patternSink verifies each event's bytes against the fakeProc pattern and records spans.
type patternSink struct {
	mu     sync.Mutex
	events int
	last   uint64
	spans  [][2]uint64
	bad    string
}

func (s *patternSink) Output(_ string, seq uint64, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := seq - uint64(len(data))
	if start < s.last && s.bad == "" {
		s.bad = "events overlap or arrive out of order"
	}
	for i, b := range data {
		if b != patternByte(int(start)+i) && s.bad == "" {
			s.bad = "event bytes do not match the stream"
		}
	}
	s.events++
	s.last = seq
	s.spans = append(s.spans, [2]uint64{start, seq})
}
func (s *patternSink) Changed(terminals.Info) {}
func (s *patternSink) end() int               { s.mu.Lock(); defer s.mu.Unlock(); return int(s.last) }

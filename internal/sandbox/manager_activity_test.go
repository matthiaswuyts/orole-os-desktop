package sandbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/wire"
)

func TestIdleStopAfterTimeout(t *testing.T) {
	h := newHarness(t, nil)
	ensure(t, h, "bot1")
	used := h.clock()
	h.advance(15*time.Minute - time.Second)
	h.m.reapIdle()
	if st := h.m.Status("bot1"); st.State != StateRunning {
		t.Fatalf("stopped before 15 min: %+v", st)
	}
	h.advance(time.Second)
	h.m.reapIdle()
	if st := h.m.Status("bot1"); st.State != StateOff || st.Error != "" {
		t.Fatalf("after 15 idle min = %+v, want off without error", st)
	}
	select {
	case <-h.b.vmFor("bot1").Done():
	default:
		t.Fatal("VM still running")
	}
	rec, _, _ := h.store.Get(context.Background(), "bot1")
	if !rec.LastUsedAt.Equal(used) {
		t.Fatalf("last_used_at = %v, want %v (the last bot activity, not the stop)", rec.LastUsedAt, used)
	}
}

func TestIdleStopSkipsViewersTakeoverAndBusyVMs(t *testing.T) {
	h := newHarness(t, nil)
	ensure(t, h, "a")
	ensure(t, h, "b")
	g := ensure(t, h, "c")
	h.m.SetViewer("a", true)
	if err := h.m.TakeOver("b"); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = g.Exec(context.Background(), wire.ExecRequest{Command: "sleep 5s"}) }()
	eventually(t, func() bool { return h.active("c") == 1 })
	h.advance(time.Hour)
	h.m.reapIdle()
	for _, id := range []string{"a", "b", "c"} {
		if st := h.m.Status(id); st.State != StateRunning {
			t.Fatalf("%s = %+v, want running", id, st)
		}
	}
	h.m.SetViewer("a", false) // the idle clock restarts when the last viewer leaves
	h.advance(15 * time.Minute)
	h.m.reapIdle()
	if st := h.m.Status("a"); st.State != StateOff {
		t.Fatalf("a = %+v, want off", st)
	}
	if st := h.m.Status("b"); st.State != StateRunning || !st.TakenOver {
		t.Fatalf("b = %+v, want running and taken over", st)
	}
}

func TestActivityResetsIdleTimer(t *testing.T) {
	h := newHarness(t, nil)
	g := ensure(t, h, "bot1")
	h.advance(10 * time.Minute)
	h.m.Touch("bot1")
	h.advance(10 * time.Minute)
	h.m.reapIdle()
	if h.m.Status("bot1").State != StateRunning {
		t.Fatal("Touch did not count as activity")
	}
	if _, err := g.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.advance(10 * time.Minute)
	h.m.reapIdle()
	if h.m.Status("bot1").State != StateRunning {
		t.Fatal("a guest request did not count as activity")
	}
	h.advance(5 * time.Minute)
	h.m.reapIdle()
	if h.m.Status("bot1").State != StateOff {
		t.Fatal("not stopped 15 min after the last activity")
	}
}

func TestTakeOverLock(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.m.TakeOver("bot1"); !errors.Is(err, errNotRunning) {
		t.Fatalf("TakeOver without a VM = %v, want errNotRunning", err)
	}
	ensure(t, h, "bot1")
	var mu sync.Mutex
	var events []Status
	h.m.Subscribe(func(s Status) { mu.Lock(); events = append(events, s); mu.Unlock() })

	if err := h.m.TakeOver("bot1"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.TakeOver("bot1"); err != nil {
		t.Fatalf("second TakeOver = %v, want nil", err)
	}
	if !h.m.TakenOver("bot1") || !h.m.Status("bot1").TakenOver {
		t.Fatal("lock not visible after TakeOver")
	}
	h.m.Release("bot1")
	h.m.Release("bot1")
	if h.m.TakenOver("bot1") || h.m.Status("bot1").TakenOver {
		t.Fatal("lock still held after Release")
	}
	mu.Lock()
	takes, releasedAfter := 0, false
	for _, e := range events {
		if e.TakenOver {
			takes++
		} else if takes > 0 && e.State == StateRunning {
			releasedAfter = true
		}
	}
	mu.Unlock()
	if takes != 1 || !releasedAfter {
		t.Fatalf("events = %+v, want one taken-over event then a released one", events)
	}

	if err := h.m.TakeOver("bot1"); err != nil {
		t.Fatal(err)
	}
	h.b.vmFor("bot1").crash()
	eventually(t, func() bool { return h.m.Status("bot1").State == StateOff })
	if h.m.TakenOver("bot1") {
		t.Fatal("lock survived a crash")
	}
}

func TestTakenOverVMIsNeverEvicted(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxRunning = 1; c.BusyWait = 200 * time.Millisecond })
	ensure(t, h, "a")
	if err := h.m.TakeOver("a"); err != nil {
		t.Fatal(err)
	}
	h.advance(time.Hour)
	if _, err := h.m.Ensure(context.Background(), "b"); !errors.Is(err, ErrBusy) {
		t.Fatalf("Ensure(b) = %v, want ErrBusy", err)
	}
	h.m.reapIdle()
	if st := h.m.Status("a"); st.State != StateRunning || !st.TakenOver {
		t.Fatalf("a = %+v", st)
	}
}

// The idleLoop goroutine itself reaps (real clock, short timeout), not just reapIdle called by hand.
func TestIdleLoopReapsIdleVM(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.IdleTimeout = 40 * time.Millisecond; c.Now = time.Now })
	ensure(t, h, "bot1")
	eventually(t, func() bool { return h.m.Status("bot1").State == StateOff })
	select {
	case <-h.b.vmFor("bot1").Done():
	default:
		t.Fatal("status is off but the VM is still running")
	}
}

// A VM that was busy is reaped once its call ends and the timeout passes.
func TestBusyVMIsReapedAfterCallEnds(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.IdleTimeout = 40 * time.Millisecond; c.Now = time.Now })
	h.m.SetViewer("bot1", true) // keeps the idle loop away until the Exec below is registered
	g := ensure(t, h, "bot1")
	vm := h.b.vmFor("bot1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { _, err := g.Exec(ctx, wire.ExecRequest{Command: "sleep 10s"}); errc <- err }()
	eventually(t, func() bool { return h.active("bot1") == 1 })
	h.m.SetViewer("bot1", false)       // from here only the in-flight call keeps the VM alive
	time.Sleep(200 * time.Millisecond) // well past the timeout while busy
	if st := h.m.Status("bot1"); st.State != StateRunning {
		t.Fatalf("busy VM = %+v, want running", st)
	}
	cancel()
	select {
	case <-errc:
	case <-time.After(5 * time.Second):
		t.Fatal("Exec did not return after cancel")
	}
	eventually(t, func() bool { return h.m.Status("bot1").State == StateOff })
	select {
	case <-vm.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("status is off but the VM is still running")
	}
}

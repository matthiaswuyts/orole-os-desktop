package sandbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type handoffResult struct {
	note string
	err  error
}

func askAsync(h *Handoffs, ctx context.Context, botID, reason string, takenOver bool) <-chan handoffResult {
	out := make(chan handoffResult, 1)
	go func() {
		note, err := h.Ask(ctx, botID, reason, takenOver)
		out <- handoffResult{note, err}
	}()
	return out
}

func waitPending(t *testing.T, h *Handoffs, botID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !h.State(botID).Pending {
		if time.Now().After(deadline) {
			t.Fatal("the wait never started")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestHandoffWaitsForTheUserToFinish(t *testing.T) {
	var mu sync.Mutex
	var seen []HandoffState
	h := NewHandoffs(func(s HandoffState) { mu.Lock(); seen = append(seen, s); mu.Unlock() })
	h.AskTimeout = 200 * time.Millisecond
	h.HoldTimeout = 5 * time.Second
	res := askAsync(h, context.Background(), "b1", "Log in to example.com", false)
	waitPending(t, h, "b1")
	if st := h.State("b1"); st.Reason != "Log in to example.com" || st.Since == 0 {
		t.Fatalf("state: %+v", st)
	}
	h.Started("b1")
	time.Sleep(300 * time.Millisecond) // past AskTimeout: taking over switched to the hold timeout
	h.Finish("b1", "logged in")
	r := <-res
	if r.err != nil || r.note != "logged in" {
		t.Fatalf("Ask = %q %v", r.note, r.err)
	}
	if h.State("b1").Pending {
		t.Fatal("the wait must end")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || !seen[0].Pending || seen[1].Pending {
		t.Fatalf("changes: %+v", seen)
	}
}

func TestHandoffTimesOutWhenNobodyTakesOver(t *testing.T) {
	h := NewHandoffs(nil)
	h.AskTimeout = 20 * time.Millisecond
	if _, err := h.Ask(context.Background(), "b1", "help", false); !errors.Is(err, ErrHandoffTimeout) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandoffHoldTimesOutWhenTheUserNeverHandsBack(t *testing.T) {
	h := NewHandoffs(nil)
	h.AskTimeout = 5 * time.Second
	h.HoldTimeout = 30 * time.Millisecond
	res := askAsync(h, context.Background(), "b1", "help", false)
	waitPending(t, h, "b1")
	h.Started("b1")
	if r := <-res; !errors.Is(r.err, ErrHandoffHoldTimeout) {
		t.Fatalf("err = %v", r.err)
	}
}

func TestHandoffWhileAlreadyTakenOverUsesTheHoldTimeout(t *testing.T) {
	h := NewHandoffs(nil)
	h.AskTimeout = 10 * time.Millisecond
	h.HoldTimeout = 5 * time.Second
	res := askAsync(h, context.Background(), "b1", "help", true)
	waitPending(t, h, "b1")
	time.Sleep(50 * time.Millisecond)
	h.Finish("b1", "")
	if r := <-res; r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
}

func TestHandoffEndsWithTheTurn(t *testing.T) {
	h := NewHandoffs(nil)
	ctx, cancel := context.WithCancel(context.Background())
	res := askAsync(h, ctx, "b1", "help", false)
	waitPending(t, h, "b1")
	cancel()
	if r := <-res; !errors.Is(r.err, context.Canceled) {
		t.Fatalf("err = %v", r.err)
	}
	if h.State("b1").Pending {
		t.Fatal("a canceled wait must not stay pending")
	}
}

func TestHandoffOneWaitPerBot(t *testing.T) {
	h := NewHandoffs(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = askAsync(h, ctx, "b1", "first", false)
	waitPending(t, h, "b1")
	if _, err := h.Ask(ctx, "b1", "second", false); !errors.Is(err, ErrHandoffBusy) {
		t.Fatalf("err = %v", err)
	}
	h.Finish("b2", "nobody waits") // no-op
	h.Started("b2")                // no-op
}

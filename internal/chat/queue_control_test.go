package chat

import (
	"context"
	"testing"
	"time"
)

func TestPausedQueueHoldsWaitingMessagesUntilResume(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 4), release: make(chan struct{}), text: "reply"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	b := e.bot("Pause")
	ctx := context.Background()

	e.send(b.ID, "one")
	<-bp.started
	second := e.send(b.ID, "two")
	third := e.send(b.ID, "three")
	if err := e.m.PauseQueue(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.m.CancelQueued(ctx, b.ID, third.ID); err != nil {
		t.Fatal(err)
	}
	close(bp.release) // the active reply finishes; the paused queue must not start "two"

	e.until("the first reply to finish", func() bool { return len(e.history(b.ID)) >= 3 })
	time.Sleep(100 * time.Millisecond)
	h := e.history(b.ID)
	for _, m := range h {
		if m.ID == second.ID && m.Status != StatusPending {
			t.Fatalf("a paused queue started the next message: %s", m.Status)
		}
		if m.ID == third.ID && (m.Status != StatusCanceled || m.Error == "") {
			t.Fatalf("a canceled waiting message must be marked canceled: %+v", m)
		}
	}
	paused := false
	for _, ev := range e.rec.all() {
		if rs, ok := ev.payload.(RunState); ok && ev.event == "chat.run" && rs.BotID == b.ID && rs.Paused {
			paused = true
		}
	}
	if !paused {
		t.Fatal("pausing must be announced in a chat.run event")
	}

	if err := e.m.ResumeQueue(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.idle(b.ID)
	if r := e.lastReply(b.ID); r.Status != StatusComplete {
		t.Fatalf("after resume: %+v", r)
	}
	for _, m := range e.history(b.ID) {
		if m.ID == second.ID && m.Status != StatusComplete {
			t.Fatalf("the held message must be answered after resume: %s", m.Status)
		}
	}
}

func TestCancelQueuedEndsAPausedQueueThatBecomesEmpty(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 4), release: make(chan struct{}), text: "reply"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	b := e.bot("Drain")
	ctx := context.Background()

	e.send(b.ID, "one")
	<-bp.started
	second := e.send(b.ID, "two")
	if err := e.m.PauseQueue(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	close(bp.release)
	e.until("the first reply to finish", func() bool { return len(e.history(b.ID)) >= 3 })
	if err := e.m.CancelQueued(ctx, b.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	e.idle(b.ID) // the parked worker must notice that nothing is left and exit

	if err := e.m.CancelQueued(ctx, b.ID, second.ID); err == nil {
		t.Fatal("a message that is no longer waiting cannot be canceled")
	}
}

func TestCancelStopsAPausedQueue(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 4), release: make(chan struct{}), text: "reply"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	b := e.bot("Stop")
	ctx := context.Background()

	e.send(b.ID, "one")
	<-bp.started
	queued := e.send(b.ID, "two")
	if err := e.m.PauseQueue(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	close(bp.release)
	e.until("the first reply to finish", func() bool { return len(e.history(b.ID)) >= 3 })
	if err := e.m.Cancel(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.idle(b.ID)
	for _, m := range e.history(b.ID) {
		if m.ID == queued.ID && m.Status != StatusCanceled {
			t.Fatalf("stopping must cancel what a paused queue still holds: %s", m.Status)
		}
	}
}

package chat

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
)

func executive(in *BotInput) { in.Config.Executive = true }

type askOutcome struct {
	text string
	err  error
}

// askAsync runs Manager.Ask on its own goroutine.
func (e *env) askAsync(ctx context.Context, from RunContext, calleeID, question string) <-chan askOutcome {
	out := make(chan askOutcome, 1)
	go func() {
		text, err := e.m.Ask(ctx, from, calleeID, question)
		out <- askOutcome{text, err}
	}()
	return out
}

func (e *env) outcome(ch <-chan askOutcome) askOutcome {
	e.t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(10 * time.Second):
		e.t.Fatal("Ask did not return")
		return askOutcome{}
	}
}

// fromBot is the RunContext of a user-started run of bot b that is about to delegate: the chain
// holds the asking bot, as the ask_bot tool sets it.
func fromBot(b Bot) RunContext { return RunContext{BotID: b.ID, Bot: b, Chain: []string{b.ID}} }

func TestAskIsARealTurnOnTheCallee(t *testing.T) {
	var gotRC RunContext
	probe := &fakeTool{id: "probe", call: func(_ context.Context, rc RunContext, _ string, _ json.RawMessage) (ToolResult, error) {
		gotRC = rc
		return ToolResult{Text: "probed"}, nil
	}}
	e := newEnv(t, func(call int, req llm.Request) []llm.Delta {
		if !strings.Contains(req.System, "Be Callee.") {
			return []llm.Delta{llmtest.Text("caller reply")}
		}
		if req.Messages[len(req.Messages)-1].Role == llm.RoleTool {
			return []llm.Delta{llmtest.Text("callee says 42")}
		}
		return append([]llm.Delta{llmtest.Text("Let me check. ")}, llmtest.Call("c1", "probe", `{}`)...)
	})
	e.m.AddTools(probe)
	callee := e.bot("Callee")
	caller := e.bot("Caller", executive)
	from := RunContext{BotID: caller.ID, Bot: caller, Chain: []string{"origin", caller.ID}}

	out, err := e.m.Ask(context.Background(), from, callee.ID, "what is the answer?")
	if err != nil || out != "callee says 42" {
		t.Fatalf("ask: %q %v", out, err)
	}
	e.idle(callee.ID)

	// The exchange is part of the callee's own conversation: the question as the executive's
	// message, the work and the reply as a normal assistant message.
	h := e.history(callee.ID)
	if len(h) != 2 {
		t.Fatalf("callee conversation: %+v", h)
	}
	q, a := h[0], h[1]
	if q.Role != "user" || q.Author != "bot:"+caller.ID || q.Status != StatusComplete || messageText(q.Parts) != "what is the answer?" {
		t.Fatalf("delegated question: %+v", q)
	}
	if a.Role != "assistant" || a.Author != "bot:"+callee.ID || a.Status != StatusComplete ||
		len(partsOfType(a.Parts, PartTool)) != 1 || !strings.HasSuffix(replyText(a), out) {
		t.Fatalf("callee reply: %+v (returned %q)", a, out)
	}
	if n := len(e.history(caller.ID)); n != 0 {
		t.Fatalf("the caller's conversation gained %d messages", n)
	}

	// The run is a depth-1 run that knows who waits on it, and the model was told it is a handed-off task.
	if gotRC.BotID != callee.ID || gotRC.Depth != 1 || !slices.Equal(gotRC.Chain, from.Chain) {
		t.Fatalf("callee run context: %+v", gotRC)
	}
	first := e.fake.Calls()[0]
	if !strings.Contains(first.System, `"Caller"`) || !strings.Contains(first.System, "handed off") ||
		len(first.Messages) != 1 || first.Messages[0].Parts[0].Text != "what is the answer?" {
		t.Fatalf("callee request: system=%q messages=%+v", first.System, first.Messages)
	}

	// The user can watch it: the question is published as the executive's message, the reply streams
	// as snapshots of a live message, and the bot rail sees the worker run and then rest.
	var sawQuestion, sawStreaming bool
	published := e.rec.messages(callee.ID)
	for _, m := range published {
		sawQuestion = sawQuestion || (m.Role == "user" && m.Author == "bot:"+caller.ID)
		sawStreaming = sawStreaming || (m.ID == a.ID && m.Status == StatusRunning)
	}
	if last := published[len(published)-1]; !sawQuestion || !sawStreaming || last.ID != a.ID || last.Status != StatusComplete {
		t.Fatalf("published messages: %+v", published)
	}
	runEvents := func() (started, stopped bool) {
		for _, ev := range e.rec.all() {
			if rs, ok := ev.payload.(RunState); ok && ev.event == "chat.run" && rs.BotID == callee.ID {
				if rs.Running && rs.MessageID == a.ID {
					started = true
				}
				if !rs.Running && started {
					stopped = true
				}
			}
		}
		return started, stopped
	}
	e.until("the callee to be reported idle", func() bool { _, stopped := runEvents(); return stopped })
	if started, _ := runEvents(); !started {
		t.Fatal("no chat.run running event for the callee")
	}
}

func TestAskRules(t *testing.T) {
	e := newEnv(t, func(int, llm.Request) []llm.Delta { return []llm.Delta{llmtest.Text("ok")} })
	callee := e.bot("Callee")
	stranger := e.bot("Stranger")
	caller := e.bot("Caller", executive)
	from := fromBot(caller)

	for _, tc := range []struct {
		name     string
		from     RunContext
		callee   string
		question string
	}{
		{"only executives delegate", fromBot(stranger), callee.ID, "hi"},
		{"depth limit", RunContext{BotID: caller.ID, Bot: caller, Depth: maxDelegationDepth, Chain: from.Chain}, callee.ID, "hi"},
		{"previews cannot delegate", RunContext{}, callee.ID, "hi"},
		{"not itself", from, caller.ID, "hi"},
		{"no cycles", RunContext{BotID: caller.ID, Bot: caller, Chain: []string{callee.ID, caller.ID}}, callee.ID, "hi"},
		{"the callee must exist", from, "ghost", "hi"},
		{"empty question", from, callee.ID, "  "},
		{"oversized question", from, callee.ID, strings.Repeat("x", MaxMessageTextLen+1)},
	} {
		if _, err := e.m.Ask(context.Background(), tc.from, tc.callee, tc.question); err == nil {
			t.Errorf("%s: Ask succeeded", tc.name)
		}
	}
	if n := len(e.history(callee.ID)) + len(e.history(stranger.ID)) + len(e.history(caller.ID)); n != 0 {
		t.Fatalf("refused delegations left %d messages behind", n)
	}
	if out, err := e.m.Ask(context.Background(), from, callee.ID, "hi"); err != nil || out != "ok" {
		t.Fatalf("a valid ask: %q %v", out, err)
	}
}

func TestAskQueuesBehindABusyCallee(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "working"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	callee := e.bot("Callee")
	caller := e.bot("Caller", executive)
	own := e.send(callee.ID, "a question from the user")
	<-bp.started

	res := e.askAsync(context.Background(), fromBot(caller), callee.ID, "handoff question")
	e.until("the handoff to be queued", func() bool { return len(e.history(callee.ID)) == 3 })
	h := e.history(callee.ID)
	if h[2].Author != "bot:"+caller.ID || h[2].Status != StatusPending {
		t.Fatalf("queued handoff: %+v", h[2])
	}
	if h[0].ID != own.ID || h[1].Status != StatusRunning {
		t.Fatalf("the user's own turn must keep running: %+v", h[:2])
	}
	select {
	case o := <-res:
		t.Fatalf("answered while the callee was still busy: %+v", o)
	case <-time.After(50 * time.Millisecond):
	}

	close(bp.release)
	o := e.outcome(res)
	e.idle(callee.ID)
	if o.err != nil || o.text != "working done" {
		t.Fatalf("ask: %+v", o)
	}
	h = e.history(callee.ID)
	var order []string
	for _, m := range h {
		if m.Status != StatusComplete {
			t.Fatalf("message %d (%s) ended %s", m.Seq, m.Role, m.Status)
		}
		order = append(order, m.Role)
	}
	if want := []string{"user", "assistant", "user", "assistant"}; !slices.Equal(order, want) {
		t.Fatalf("order: %v want %v", order, want)
	}
	if h[0].ID != own.ID || h[0].Author != "user" || h[2].Author != "bot:"+caller.ID || replyText(h[1]) != "working done" || replyText(h[3]) != o.text {
		t.Fatalf("conversation: %+v", h)
	}
}

func TestAskCanceledWhileRunningStopsOnlyThatTurn(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "working"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	callee := e.bot("Callee")
	caller := e.bot("Caller", executive)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res := e.askAsync(ctx, fromBot(caller), callee.ID, "long task")
	<-bp.started
	later := e.send(callee.ID, "a message the user sends meanwhile")
	if later.Status != StatusPending {
		t.Fatalf("later message: %s", later.Status)
	}
	cancel() // the executive was stopped
	if o := e.outcome(res); !errors.Is(o.err, context.Canceled) {
		t.Fatalf("ask: %+v", o)
	}
	close(bp.release)
	e.idle(callee.ID)

	// The delegated reply stopped; the user's message that waited behind it was neither dropped nor canceled.
	h := e.history(callee.ID)
	if len(h) != 4 {
		t.Fatalf("history: %+v", h)
	}
	if h[0].Status != StatusComplete || h[1].Status != StatusCanceled || replyText(h[1]) != "working" {
		t.Fatalf("delegated turn: %+v %+v", h[0], h[1])
	}
	if h[2].ID != later.ID || h[2].Status != StatusComplete || h[3].Status != StatusComplete || replyText(h[3]) != "working done" {
		t.Fatalf("the user's message must still be answered: %+v %+v", h[2], h[3])
	}
}

func TestAskCanceledWhileQueuedIsDropped(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "working"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	callee := e.bot("Callee")
	caller := e.bot("Caller", executive)
	e.send(callee.ID, "a question from the user")
	<-bp.started
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res := e.askAsync(ctx, fromBot(caller), callee.ID, "handoff")
	e.until("the handoff to be queued", func() bool { return len(e.history(callee.ID)) == 3 })
	cancel()
	if o := e.outcome(res); !errors.Is(o.err, context.Canceled) {
		t.Fatalf("ask: %+v", o)
	}

	h := e.history(callee.ID)
	if h[2].Status != StatusCanceled || h[2].Error == "" || h[1].Status != StatusRunning {
		t.Fatalf("dropped handoff and the user's turn: %+v", h)
	}
	published := e.rec.messages(callee.ID)
	if last := published[len(published)-1]; last.ID != h[2].ID || last.Status != StatusCanceled {
		t.Fatalf("the dropped message must be published as canceled: %+v", last)
	}
	close(bp.release)
	e.idle(callee.ID)
	h = e.history(callee.ID)
	if len(h) != 3 || h[1].Status != StatusComplete || replyText(h[1]) != "working done" {
		t.Fatalf("the user's turn must finish untouched and the dropped handoff must never run: %+v", h)
	}
}

func TestAskTimeoutIsReportedAsATimeout(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "still researching"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	callee := e.bot("Callee")
	caller := e.bot("Caller", executive)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	res := e.askAsync(ctx, fromBot(caller), callee.ID, "slow task")
	o := e.outcome(res)
	if !errors.Is(o.err, context.DeadlineExceeded) || errors.Is(o.err, context.Canceled) {
		t.Fatalf("a run slower than the ask timeout must report the deadline, got %v", o.err)
	}
	e.idle(callee.ID)
	for _, m := range e.history(callee.ID) {
		if m.Role == "assistant" && m.Status != StatusCanceled {
			t.Fatalf("the timed-out delegated run must be stopped: %+v", m)
		}
	}
}

func TestAbandoningADelegationNeverTouchesOtherTurns(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "working"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	callee := e.bot("Callee")
	e.send(callee.ID, "the user's own turn")
	<-bp.started

	e.m.abandonDelegated(context.Background(), callee.ID, "some-other-message")
	close(bp.release)
	e.idle(callee.ID)
	if r := e.lastReply(callee.ID); r.Status != StatusComplete || replyText(r) != "working done" {
		t.Fatalf("an unrelated turn was stopped: %+v", r)
	}
}

func TestStoppingTheCalleeEndsTheAskInsteadOfHanging(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "working"}
		e := newEnv(t, nil)
		e.llm.prov = bp
		callee := e.bot("Callee")
		caller := e.bot("Caller", executive)
		res := e.askAsync(context.Background(), fromBot(caller), callee.ID, "task")
		<-bp.started
		if err := e.m.Cancel(context.Background(), callee.ID); err != nil {
			t.Fatal(err)
		}
		o := e.outcome(res)
		if o.err == nil || !strings.Contains(o.err.Error(), "stopped") || errors.Is(o.err, context.Canceled) {
			t.Fatalf("ask: %+v", o)
		}
	})
	t.Run("queued", func(t *testing.T) {
		bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "working"}
		e := newEnv(t, nil)
		e.llm.prov = bp
		callee := e.bot("Callee")
		caller := e.bot("Caller", executive)
		e.send(callee.ID, "the user's own turn")
		<-bp.started
		res := e.askAsync(context.Background(), fromBot(caller), callee.ID, "task")
		e.until("the handoff to be queued", func() bool { return len(e.history(callee.ID)) == 3 })
		if err := e.m.Cancel(context.Background(), callee.ID); err != nil {
			t.Fatal(err)
		}
		o := e.outcome(res)
		if o.err == nil || !strings.Contains(o.err.Error(), "stopped before it started") {
			t.Fatalf("ask: %+v", o)
		}
		if h := e.history(callee.ID); h[2].Status != StatusCanceled {
			t.Fatalf("handoff: %+v", h[2])
		}
	})
}

func TestAskRefusesToQueueBehindABotThatWaitsForTheCaller(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "working"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	alpha := e.bot("Alpha", executive)
	beta := e.bot("Beta", executive)
	e.send(beta.ID, "keep Beta busy")
	<-bp.started
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Alpha waits for Beta, whose own turn is busy; the handoff sits in Beta's queue.
	res := e.askAsync(ctx, fromBot(alpha), beta.ID, "from Alpha")
	e.until("Alpha's handoff to be queued", func() bool { return len(e.history(beta.ID)) == 3 })

	// Beta's run now asks Alpha, which cannot start anything until Beta answers: neither could finish.
	_, err := e.m.Ask(context.Background(), fromBot(beta), alpha.ID, "from Beta")
	if err == nil || !strings.Contains(err.Error(), "deadlock") {
		t.Fatalf("ask back: %v", err)
	}
	if n := len(e.history(alpha.ID)); n != 0 {
		t.Fatalf("a refused delegation left %d messages behind", n)
	}

	cancel()
	e.outcome(res)
	e.m.mu.Lock()
	waiting := len(e.m.waits)
	e.m.mu.Unlock()
	if waiting != 0 {
		t.Fatalf("%d finished asks are still recorded as waiting", waiting)
	}
	// With nothing waiting any more, the same ask goes through.
	close(bp.release)
	if _, err := e.m.Ask(context.Background(), fromBot(beta), alpha.ID, "from Beta again"); err != nil {
		t.Fatalf("ask after the first finished: %v", err)
	}
}

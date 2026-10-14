package jarvis

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
)

type fakeHost struct {
	mu             sync.Mutex
	bots           map[string]chat.Bot
	gateErr        error
	opener         string
	turns          []chat.VoiceRequest
	reply          string
	cards          map[string]chat.MsgPart
	notices        []string
	disabled       []string
	toolDuringTurn string
	holdFirst      bool          // the first VoiceTurn blocks until its context is canceled
	started        chan struct{} // signaled when the held turn is running
	botErr         error
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		bots: map[string]chat.Bot{
			"b1": {ID: "b1", Name: "Atlas", Config: chat.BotConfig{Executive: true, Jarvis: chat.BotJarvis{Enabled: true}}},
			"b2": {ID: "b2", Name: "Nova", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}},
			"b3": {ID: "b3", Name: "Quiet"},
		},
		opener: "Hey, Atlas here. The deploy failed.",
		reply:  "Two tasks are running.",
		cards:  map[string]chat.MsgPart{},
	}
}

func (h *fakeHost) Bot(_ context.Context, id string) (chat.Bot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.botErr != nil {
		return chat.Bot{}, h.botErr
	}
	b, ok := h.bots[id]
	if !ok {
		return b, errors.New("no bot")
	}
	return b, nil
}
func (h *fakeHost) Bots(context.Context) ([]chat.Bot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return []chat.Bot{h.bots["b1"], h.bots["b2"], h.bots["b3"]}, nil
}
func (h *fakeHost) Gate(_ context.Context, b chat.Bot) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !b.Config.Jarvis.Enabled {
		return &GateError{"off"}
	}
	return h.gateErr
}
func (h *fakeHost) VoiceTurn(ctx context.Context, r chat.VoiceRequest) (string, error) {
	h.mu.Lock()
	h.turns = append(h.turns, r)
	hold := h.holdFirst && len(h.turns) == 1
	tool, reply := h.toolDuringTurn, h.reply
	h.mu.Unlock()
	if hold {
		h.started <- struct{}{}
		<-ctx.Done()
		r.OnText("stale words")
		return "stale reply", ctx.Err()
	}
	if tool != "" && r.OnTool != nil {
		r.OnTool(tool)
	}
	r.OnText(reply)
	return reply, nil
}
func (h *fakeHost) Compose(context.Context, string, string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.opener, nil
}
func (h *fakeHost) PostCallCard(_ context.Context, _ string, p chat.MsgPart) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cards["m-"+p.ID] = p
	return "m-" + p.ID, nil
}
func (h *fakeHost) UpdateCallCard(_ context.Context, id string, p chat.MsgPart) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cards[id] = p
	return nil
}
func (h *fakeHost) Notify(title, body, target, level string) {
	h.mu.Lock()
	h.notices = append(h.notices, title+": "+body)
	h.mu.Unlock()
}
func (h *fakeHost) Audit(string, string, string, string) {}
func (h *fakeHost) DisableJarvis(_ context.Context, id, _ string) {
	h.mu.Lock()
	h.disabled = append(h.disabled, id)
	h.mu.Unlock()
}
func (h *fakeHost) noticeList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.notices...)
}
func (h *fakeHost) turnList() []chat.VoiceRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]chat.VoiceRequest(nil), h.turns...)
}

type fakeDialer struct {
	mu     sync.Mutex
	calls  []OutboundCall
	status string
	err    error
}

func (d *fakeDialer) OutboundCall(_ context.Context, c OutboundCall) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, c)
	return "conv-" + c.CallID, "CA1", d.err
}
func (d *fakeDialer) ConversationStatus(context.Context, string) (string, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status, 0, nil
}
func (d *fakeDialer) setStatus(s string) { d.mu.Lock(); d.status = s; d.mu.Unlock() }
func (d *fakeDialer) dialed() []OutboundCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]OutboundCall(nil), d.calls...)
}

type fakeSender struct {
	mu     sync.Mutex
	frames []Frame
	down   bool
}

func (s *fakeSender) Send(_ context.Context, f Frame) error {
	if s.down {
		return ErrRelayDown
	}
	s.mu.Lock()
	s.frames = append(s.frames, f)
	s.mu.Unlock()
	return nil
}
func (s *fakeSender) Connected() bool { return !s.down }
func (s *fakeSender) all() []Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Frame(nil), s.frames...)
}
func (s *fakeSender) text(id string) string {
	var b strings.Builder
	for _, f := range s.all() {
		if f.ID == id && f.Type == FrameLLMDelta {
			b.WriteString(f.Text)
		}
	}
	return b.String()
}
func (s *fakeSender) hungUp(callID string) bool {
	for _, f := range s.all() {
		if f.Type == FrameHangup && f.CallID == callID {
			return true
		}
	}
	return false
}

// fakeAfter replaces time.After: timers only fire when the test says so.
type fakeAfter struct {
	mu     sync.Mutex
	timers []*fakeTimer
}
type fakeTimer struct {
	d     time.Duration
	ch    chan time.Time
	fired bool
}

func (f *fakeAfter) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{d: d, ch: make(chan time.Time, 1)}
	f.timers = append(f.timers, t)
	return t.ch
}

// fire waits for an unfired timer of duration d, then fires it.
func (f *fakeAfter) fire(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, tm := range f.timers {
			if tm.d == d && !tm.fired {
				tm.fired = true
				tm.ch <- time.Time{}
				f.mu.Unlock()
				return
			}
		}
		f.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no pending timer of %v", d)
}

type callEnv struct {
	c     *Calls
	host  *fakeHost
	dial  *fakeDialer
	relay *fakeSender
	store *Store
	now   *time.Time
}

func newCallEnv(t *testing.T) *callEnv {
	t.Helper()
	st, now := testStore(t)
	*now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	e := &callEnv{host: newFakeHost(), dial: &fakeDialer{}, relay: &fakeSender{}, store: st, now: now}
	settings := PhoneSettings{OwnerNumber: "+32470000000", OwnerVerified: true, AgentID: "ag", PhoneNumberID: "pn"}
	e.c = NewCalls(st, e.host, e.dial, e.relay, func() PhoneSettings { return settings }, func() time.Time { return *e.now }, slog.Default())
	t.Cleanup(e.c.Close) // registered after testStore's cleanup, so it runs first
	return e
}

func (e *callEnv) withClock() *fakeAfter {
	fa := &fakeAfter{}
	e.c.after = fa.After
	return fa
}

func (e *callEnv) waitInactive(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, active := e.c.Active(); !active {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("call still active")
}

func llmFrame(id, callID, text string) Frame {
	return Frame{Type: FrameLLMRequest, ID: id, CallID: callID, Messages: []LLMMessage{{Role: "user", Content: text}}}
}

func TestOutboundCallSpeaksCachedOpenerFirst(t *testing.T) {
	e := newCallEnv(t)
	call, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "the deploy failed"})
	if err != nil {
		t.Fatal(err)
	}
	d := e.dial.dialed()
	if len(d) != 1 || d[0].To != "+32470000000" || d[0].CallID != call.ID {
		t.Fatalf("dialed %+v", d)
	}
	e.c.HandleFrame(context.Background(), Frame{Type: FrameLLMRequest, ID: "r0", CallID: call.ID}) // first turn: no user message yet
	if got := e.relay.text("r0"); got != e.host.opener {
		t.Fatalf("first turn spoke %q", got)
	}
	if len(e.host.turnList()) != 0 {
		t.Fatal("opener ran a model turn")
	}
}

func TestOpenerFallbackWhenInvalid(t *testing.T) {
	e := newCallEnv(t)
	e.host.opener = "Hi from Orole!"
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "the deploy failed"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameLLMRequest, ID: "r0", CallID: call.ID})
	got := e.relay.text("r0")
	if !strings.Contains(got, "Atlas") || strings.Contains(strings.ToLower(got), "orole") {
		t.Fatalf("fallback %q", got)
	}
}

func TestSpokenTurnRunsVoiceTurnWithFiller(t *testing.T) {
	e := newCallEnv(t)
	e.host.toolDuringTurn = "list_tasks"
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameLLMRequest, ID: "r0", CallID: call.ID})
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "what's running?"))
	got := e.relay.text("r1")
	if !strings.HasPrefix(got, "Checking your tasks.") || !strings.HasSuffix(got, "Two tasks are running.") {
		t.Fatalf("spoken %q", got)
	}
	turns := e.host.turnList()
	if turns[0].Text != "what's running?" || turns[0].CallID != call.ID {
		t.Fatalf("turn %+v", turns[0])
	}
	if len(turns[0].History) != 1 || turns[0].History[0].Role != "assistant" {
		t.Fatalf("history should start with the opener: %+v", turns[0].History)
	}
}

func TestSpokenTextNeverNamesTheApp(t *testing.T) {
	e := newCallEnv(t)
	e.host.reply = "Orole OS says all good, and Orole agrees."
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "status?"))
	if got := e.relay.text("r1"); got != "the app says all good, and the app agrees." {
		t.Fatalf("spoken %q", got)
	}
}

func TestGateFailureMidCallDisablesJarvis(t *testing.T) {
	e := newCallEnv(t)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.host.mu.Lock()
	e.host.gateErr = &GateError{"Jarvis needs this bot's tool access set to All."}
	e.host.mu.Unlock()
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "start a task"))
	if len(e.host.turnList()) != 0 {
		t.Fatal("turn ran after the gate failed")
	}
	e.host.mu.Lock()
	disabled := e.host.disabled
	e.host.mu.Unlock()
	if !strings.Contains(e.relay.text("r1"), "tool access") || len(disabled) != 1 {
		t.Fatalf("spoken=%q disabled=%v", e.relay.text("r1"), disabled)
	}
}

func TestSecondRuleDuringCallIsInjected(t *testing.T) {
	e := newCallEnv(t)
	if _, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b2", Reason: "the deploy finished"}); err != ErrInjected {
		t.Fatalf("second place: %v", err)
	}
	if len(e.dial.dialed()) != 1 {
		t.Fatal("dialed twice")
	}
	call, _ := e.c.Active()
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "anything else?"))
	if !strings.Contains(e.host.turnList()[0].Brief, "the deploy finished") {
		t.Fatalf("injection not in brief: %q", e.host.turnList()[0].Brief)
	}
}

func TestUnknownCallGetsGoodbye(t *testing.T) {
	e := newCallEnv(t)
	e.c.HandleFrame(context.Background(), llmFrame("r1", "gone", "hello?"))
	if e.relay.text("r1") == "" || len(e.host.turnList()) != 0 {
		t.Fatalf("spoken %q", e.relay.text("r1"))
	}
	if !e.relay.hungUp("gone") {
		t.Fatal("no hangup for unknown call")
	}
}

func TestInboundOnlyFromOwner(t *testing.T) {
	e := newCallEnv(t)
	e.c.HandleFrame(context.Background(), Frame{Type: FrameInboundLookup, ID: "i1", Caller: "+15550000000"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameInboundLookup, ID: "i2", Caller: "+32470000000"})
	var a1, a2 Frame
	for _, f := range e.relay.all() {
		if f.ID == "i1" {
			a1 = f
		}
		if f.ID == "i2" {
			a2 = f
		}
	}
	if a1.Accept || !a2.Accept || a2.Vars["call_id"] == "" || a2.Vars["bot_id"] != "b1" {
		t.Fatalf("a1=%+v a2=%+v", a1, a2)
	}
	if len(e.host.noticeList()) != 1 {
		t.Fatalf("inbound call not notified: %v", e.host.noticeList())
	}
}

func TestInboundRejectedWhileBusy(t *testing.T) {
	e := newCallEnv(t)
	e.c.HandleFrame(context.Background(), Frame{Type: FrameInboundLookup, ID: "i1", Caller: "+32470000000"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameInboundLookup, ID: "i2", Caller: "+32470000000"})
	for _, f := range e.relay.all() {
		if f.ID == "i2" && f.Accept {
			t.Fatal("accepted a second inbound call")
		}
	}
}

func TestSwitchBotOnCall(t *testing.T) {
	e := newCallEnv(t)
	e.c.HandleFrame(context.Background(), Frame{Type: FrameInboundLookup, ID: "i1", Caller: "+32470000000"})
	call, _ := e.c.Active()
	if _, err := e.c.SwitchBot(context.Background(), call.ID, "quiet"); err == nil {
		t.Fatal("switched to a bot without Jarvis")
	}
	if _, err := e.c.SwitchBot(context.Background(), call.ID, "nova"); err != nil {
		t.Fatal(err)
	}
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "hi"))
	if e.host.turnList()[0].BotID != "b2" {
		t.Fatalf("turn went to %s", e.host.turnList()[0].BotID)
	}
}

func TestVoicemailBecomesNotice(t *testing.T) {
	e := newCallEnv(t)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "the deploy failed"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameCallStatus, CallID: call.ID, Status: "voicemail"})
	got, _ := e.store.GetCall(context.Background(), call.ID)
	n := e.host.noticeList()
	if got.Status != StatusVoicemail || len(n) != 1 || !strings.Contains(n[0], "Atlas tried to call") {
		t.Fatalf("call=%+v notices=%v", got, n)
	}
	if _, active := e.c.Active(); active {
		t.Fatal("call still active")
	}
}

func TestRelayDownRefusesCall(t *testing.T) {
	e := newCallEnv(t)
	e.relay.down = true
	if _, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"}); err == nil {
		t.Fatal("placed a call with the relay down")
	}
}

func TestDialFailureReleasesTheLine(t *testing.T) {
	e := newCallEnv(t)
	e.dial.err = errors.New("boom")
	if _, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"}); err == nil {
		t.Fatal("expected error")
	}
	if _, active := e.c.Active(); active {
		t.Fatal("failed dial left the call active")
	}
}

func TestVerifyCallSpeaksCodeAndHangsUp(t *testing.T) {
	e := newCallEnv(t)
	call, err := e.c.Place(context.Background(), PlaceRequest{Verify: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	e.c.HandleFrame(context.Background(), Frame{Type: FrameLLMRequest, ID: "r0", CallID: call.ID})
	if got := e.relay.text("r0"); !strings.Contains(got, "1 2 3 4 5 6") {
		t.Fatalf("spoke %q", got)
	}
	if !e.relay.hungUp(call.ID) || len(e.host.turnList()) != 0 {
		t.Fatal("verify call should hang up without a model turn")
	}
}

// ---- ending: poll, ceiling, idempotence ----

func TestPollEndsCallWithoutWebhook(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "hi")) // answered
	e.dial.setStatus("done")
	fa.fire(t, pollEvery)
	e.waitInactive(t)
	got, _ := e.store.GetCall(context.Background(), call.ID)
	if got.Status != StatusCompleted || got.EndedAt == 0 {
		t.Fatalf("call=%+v", got)
	}
}

func TestPollKeepsGoingWhileCallLive(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	if _, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	e.dial.setStatus("in-progress")
	fa.fire(t, pollEvery)
	fa.fire(t, pollEvery) // the loop re-armed
	if _, active := e.c.Active(); !active {
		t.Fatal("call ended while the provider still reported it live")
	}
}

func TestPollDoneWithoutAnswerIsNoAnswer(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.dial.setStatus("done")
	fa.fire(t, pollEvery)
	e.waitInactive(t)
	got, _ := e.store.GetCall(context.Background(), call.ID)
	if got.Status != StatusNoAnswer {
		t.Fatalf("status %s", got.Status)
	}
}

func TestPollFailedEndsCallFailed(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.dial.setStatus("failed")
	fa.fire(t, pollEvery)
	e.waitInactive(t)
	got, _ := e.store.GetCall(context.Background(), call.ID)
	if got.Status != StatusFailed {
		t.Fatalf("status %s", got.Status)
	}
}

func TestCeilingEndsCall(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "hi"))
	fa.fire(t, maxCallDuration)
	e.waitInactive(t)
	got, _ := e.store.GetCall(context.Background(), call.ID)
	if got.Status != StatusCompleted || !e.relay.hungUp(call.ID) {
		t.Fatalf("call=%+v hungUp=%v", got, e.relay.hungUp(call.ID))
	}
}

func TestStatusFrameAfterPollEndingIsNoOp(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "hi"))
	e.dial.setStatus("done")
	fa.fire(t, pollEvery)
	e.waitInactive(t)
	before := len(e.host.noticeList())
	e.c.HandleFrame(context.Background(), Frame{Type: FrameCallStatus, CallID: call.ID, Status: "voicemail"})
	got, _ := e.store.GetCall(context.Background(), call.ID)
	if got.Status != StatusCompleted || len(e.host.noticeList()) != before {
		t.Fatalf("late status frame changed the call: %+v", got)
	}
}

func TestFinishIsIdempotent(t *testing.T) {
	e := newCallEnv(t)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.c.mu.Lock()
	l := e.c.cur
	e.c.mu.Unlock()
	var wins int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.c.finish(context.Background(), l, StatusCompleted, 0) {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d finishers won", wins)
	}
	_ = call
}

func TestUrgentRetryOnce(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	call, err := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "prod is down", Urgent: true})
	if err != nil {
		t.Fatal(err)
	}
	e.c.HandleFrame(context.Background(), Frame{Type: FrameCallStatus, CallID: call.ID, Status: "no-answer"})
	fa.fire(t, urgentRetryWait)
	deadline := time.Now().Add(2 * time.Second)
	for len(e.dial.dialed()) < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if len(e.dial.dialed()) != 2 {
		t.Fatalf("dials = %d", len(e.dial.dialed()))
	}
	second, ok := e.c.Active()
	if !ok {
		t.Fatal("retry call not active")
	}
	e.c.HandleFrame(context.Background(), Frame{Type: FrameCallStatus, CallID: second.ID, Status: "no-answer"})
	time.Sleep(50 * time.Millisecond)
	fa.mu.Lock()
	for _, tm := range fa.timers {
		if tm.d == urgentRetryWait && !tm.fired {
			fa.mu.Unlock()
			t.Fatal("a second retry was scheduled")
		}
	}
	fa.mu.Unlock()
	if len(e.dial.dialed()) != 2 {
		t.Fatalf("dials = %d", len(e.dial.dialed()))
	}
}

func TestNonUrgentUnansweredDoesNotRetry(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameCallStatus, CallID: call.ID, Status: "no-answer"})
	fa.mu.Lock()
	defer fa.mu.Unlock()
	for _, tm := range fa.timers {
		if tm.d == urgentRetryWait {
			t.Fatal("retry scheduled for a non-urgent call")
		}
	}
}

// ---- timing log ----

type recHandler struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *recHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.recs = append(h.recs, r)
	h.mu.Unlock()
	return nil
}
func (h *recHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recHandler) WithGroup(string) slog.Handler      { return h }

func TestTurnLogsTimingsWithoutText(t *testing.T) {
	e := newCallEnv(t)
	h := &recHandler{}
	e.c.log = slog.New(h)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "secret reason"})
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "my private utterance"))
	h.mu.Lock()
	defer h.mu.Unlock()
	var found bool
	for _, r := range h.recs {
		var line strings.Builder
		line.WriteString(r.Message)
		r.Attrs(func(a slog.Attr) bool {
			line.WriteString(" " + a.Key + "=" + a.Value.String())
			if a.Key == "first_audio_ms" {
				found = true
			}
			return true
		})
		s := line.String()
		if strings.Contains(s, "private utterance") || strings.Contains(s, "Two tasks") || strings.Contains(s, "secret reason") {
			t.Fatalf("log leaks text: %s", s)
		}
	}
	if !found {
		t.Fatal("no first_audio_ms in the turn log")
	}
}

// ---- sanitizer ----

func sanitizeChunks(bot string, chunks ...string) string {
	s := newSanitizer(bot)
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(s.Write(c))
	}
	out.WriteString(s.Flush())
	return out.String()
}

func TestSanitizer(t *testing.T) {
	cases := []struct {
		name   string
		bot    string
		chunks []string
		want   string
	}{
		{"plain", "Atlas", []string{"Hello there."}, "Hello there."},
		{"whole", "Atlas", []string{"Open Orole now"}, "Open the app now"},
		{"split in the middle", "Atlas", []string{"Open Or", "ole now"}, "Open the app now"},
		{"split byte by byte", "Atlas", strings.Split("Open Orole OS now", ""), "Open the app now"},
		{"upper OS", "Atlas", []string{"OROLE OS is up"}, "the app is up"},
		{"OS split", "Atlas", []string{"Orole ", "OS", " ready"}, "the app ready"},
		{"not OS", "Atlas", []string{"Orole Oscar"}, "the app Oscar"},
		{"at end of turn", "Atlas", []string{"see Orole"}, "see the app"},
		{"partial at end is flushed", "Atlas", []string{"see Oro"}, "see Oro"},
		{"bot named Orolebot", "Orolebot", []string{"Orolebot here, in Orole OS"}, "Orolebot here, in the app"},
		{"bot named Orolebot split", "Orolebot", []string{"Oro", "lebot says Or", "ole"}, "Orolebot says the app"},
		{"bot name split from context", "Big Orole", []string{"Big ", "Orole speaking, Orole"}, "Big Orole speaking, the app"},
		{"bot name split mid-name", "Orolebot", strings.Split("Orolebot, Orole OS and Orolebot", ""), "Orolebot, the app and Orolebot"},
		{"bot name chunk Orolebo+t", "Orolebot", []string{"Orolebo", "t rocks"}, "Orolebot rocks"},
		{"Orole Oscar bytewise", "Atlas", strings.Split("Orole Oscar", ""), "the app Oscar"},
		{"unicode", "Atlas", []string{"Orolé ", "Orole"}, "Orolé the app"},
	}
	for _, c := range cases {
		if got := sanitizeChunks(c.bot, c.chunks...); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestSanitizerHoldsBackAtMostEightBytes(t *testing.T) {
	s := newSanitizer("Atlas")
	if got := s.Write("Hello there, Orole O"); got != "Hello there, " {
		t.Fatalf("emitted %q", got)
	}
	if len(s.pend) > 8 {
		t.Fatalf("held %d bytes", len(s.pend))
	}
}

// ---- barge-in and call-speech hardening ----

func TestBargeInCancelsStaleTurn(t *testing.T) {
	e := newCallEnv(t)
	e.host.holdFirst = true
	e.host.started = make(chan struct{}, 1)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "first question"))
	}()
	<-e.host.started
	e.c.HandleFrame(context.Background(), llmFrame("r2", call.ID, "second question"))
	wg.Wait()
	if got := e.relay.text("r1"); got != "" {
		t.Fatalf("stale turn spoke %q", got)
	}
	if got := e.relay.text("r2"); got != "Two tasks are running." {
		t.Fatalf("owner heard %q", got)
	}
	for _, f := range e.relay.all() {
		if f.ID == "r1" && f.Type == FrameLLMDone {
			t.Fatal("stale turn sent done")
		}
		if strings.Contains(f.Text, "That failed") {
			t.Fatal("owner heard a failure")
		}
	}
	e.c.HandleFrame(context.Background(), llmFrame("r3", call.ID, "third"))
	turns := e.host.turnList()
	for _, l := range turns[2].History {
		if strings.Contains(l.Text, "first question") || strings.Contains(l.Text, "stale") {
			t.Fatalf("canceled turn leaked into history: %+v", turns[2].History)
		}
	}
	if len(turns[2].History) != 2 || turns[2].History[0].Text != "second question" {
		t.Fatalf("history %+v", turns[2].History)
	}
}

func TestNonGateErrorSpeaksFixedLine(t *testing.T) {
	e := newCallEnv(t)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.host.mu.Lock()
	e.host.botErr = errors.New("database is locked at secret path")
	e.host.mu.Unlock()
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "hi"))
	if got := e.relay.text("r1"); got != "I can't do that right now." {
		t.Fatalf("spoke %q", got)
	}
}

func TestGateFailureHangsUp(t *testing.T) {
	e := newCallEnv(t)
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "x"})
	e.host.mu.Lock()
	e.host.gateErr = &GateError{"tool access is off"}
	e.host.mu.Unlock()
	e.c.HandleFrame(context.Background(), llmFrame("r1", call.ID, "hi"))
	if !e.relay.hungUp(call.ID) {
		t.Fatal("no hangup after a gate refusal")
	}
}

func TestVerifyCodeSpokenEvenIfFirstRequestHasUtterance(t *testing.T) {
	e := newCallEnv(t)
	call, _ := e.c.Place(context.Background(), PlaceRequest{Verify: "123456"})
	e.c.HandleFrame(context.Background(), llmFrame("r0", call.ID, "hello?"))
	if got := e.relay.text("r0"); !strings.Contains(got, "1 2 3 4 5 6") {
		t.Fatalf("spoke %q", got)
	}
	if !e.relay.hungUp(call.ID) || len(e.host.turnList()) != 0 {
		t.Fatal("verify call ran a turn or did not hang up")
	}
}

func TestRetryHookIsUsed(t *testing.T) {
	e := newCallEnv(t)
	fa := e.withClock()
	got := make(chan PlaceRequest, 1)
	e.c.Retry = func(_ context.Context, r PlaceRequest) { got <- r }
	call, _ := e.c.Place(context.Background(), PlaceRequest{BotID: "b1", Reason: "prod is down", Urgent: true, QuietOverride: true})
	e.c.HandleFrame(context.Background(), Frame{Type: FrameCallStatus, CallID: call.ID, Status: "no-answer"})
	fa.fire(t, urgentRetryWait)
	select {
	case r := <-got:
		if r.BotID != "b1" || r.Urgent || !r.QuietOverride {
			t.Fatalf("retry %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook not called")
	}
	if len(e.dial.dialed()) != 1 {
		t.Fatal("retry bypassed the hook")
	}
}

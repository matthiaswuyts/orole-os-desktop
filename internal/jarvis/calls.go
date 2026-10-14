package jarvis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/chat"
)

// ErrInjected means a second call request was folded into the call already in progress.
var ErrInjected = errors.New("jarvis: added to the call in progress")

const (
	pollEvery       = 5 * time.Second
	maxCallDuration = 30 * time.Minute
	urgentRetryWait = 5 * time.Minute
	maxHistory      = 40
)

type Host interface {
	Bot(ctx context.Context, id string) (chat.Bot, error)
	Bots(ctx context.Context) ([]chat.Bot, error)
	Gate(ctx context.Context, bot chat.Bot) error
	VoiceTurn(ctx context.Context, req chat.VoiceRequest) (string, error)
	Compose(ctx context.Context, botID, instruction string) (string, error)
	PostCallCard(ctx context.Context, botID string, part chat.MsgPart) (string, error)
	UpdateCallCard(ctx context.Context, msgID string, part chat.MsgPart) error
	Notify(title, body, target, level string)
	Audit(action, target, outcome, detail string)
	DisableJarvis(ctx context.Context, botID, reason string)
}

type Dialer interface {
	OutboundCall(ctx context.Context, c OutboundCall) (string, string, error)
	ConversationStatus(ctx context.Context, id string) (string, int, error)
}

type Sender interface {
	Send(ctx context.Context, f Frame) error
	Connected() bool
}

type PlaceRequest struct {
	BotID, Reason, Brief, RuleID string
	Urgent                       bool
	Verify                       string // digits for a phone verification call; no bot turns, BotID may be empty
}

// live is the in-memory state of the one call in progress. Mutable fields are guarded by Calls.mu;
// opener, done, verify and urgent are fixed at creation.
type live struct {
	call    Call
	botName string
	brief   string
	inject  []string
	history []chat.VoiceLine
	spoken  bool // the first turn (opener) has been taken
	ended   bool

	opener chan string   // receives the opener exactly once (buffered)
	done   chan struct{} // closed when the call ends
	verify string
	urgent bool

	turnCancel context.CancelFunc // in-flight model turn, if any
	turnDone   chan struct{}
}

type Calls struct {
	store    *Store
	host     Host
	dial     Dialer
	relay    Sender
	settings func() Settings
	now      func() time.Time
	log      *slog.Logger
	after    func(time.Duration) <-chan time.Time // injectable for tests
	// Retry places the second attempt after an unanswered urgent call. The daemon points it at the policy-checked path
	// (caps, quiet hours); the default calls Place directly.
	Retry func(ctx context.Context, req PlaceRequest)

	baseCtx context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup

	mu     sync.Mutex
	cur    *live
	seed   int
	closed bool
}

func NewCalls(store *Store, host Host, dial Dialer, relay Sender, settings func() Settings, now func() time.Time, log *slog.Logger) *Calls {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Calls{store: store, host: host, dial: dial, relay: relay, settings: settings, now: now, log: log,
		after: time.After, baseCtx: ctx, stop: cancel}
	c.Retry = func(ctx context.Context, req PlaceRequest) { _, _ = c.Place(ctx, req) }
	return c
}

// Close stops the background watchers (status polling, retries) and waits for them. A live call is left to the provider.
func (c *Calls) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.stop()
	c.wg.Wait()
}

// spawn runs fn as a tracked goroutine unless Close has started.
func (c *Calls) spawn(fn func()) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.wg.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.wg.Done()
		fn()
	}()
}

func (c *Calls) Active() (Call, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur == nil {
		return Call{}, false
	}
	return c.cur.call, true
}

// Inject adds a line of news to the live call; the bot sees it in its next turn's brief.
func (c *Calls) Inject(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur != nil && c.cur.verify == "" {
		c.cur.inject = append(c.cur.inject, text)
	}
}

func (c *Calls) Place(ctx context.Context, req PlaceRequest) (Call, error) {
	s := c.settings()
	if s.AgentID == "" || s.PhoneNumberID == "" || s.OwnerNumber == "" {
		return Call{}, &Refusal{"Phone calls are not set up yet."}
	}
	if !c.relay.Connected() {
		return Call{}, &Refusal{"The relay is not connected."}
	}
	var bot chat.Bot
	if req.Verify == "" || req.BotID != "" {
		var err error
		if bot, err = c.host.Bot(ctx, req.BotID); err != nil {
			return Call{}, err
		}
	}
	if req.Verify == "" {
		if err := c.host.Gate(ctx, bot); err != nil {
			return Call{}, err
		}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Call{}, errors.New("jarvis: shutting down")
	}
	if c.cur != nil {
		if c.cur.verify != "" || req.Verify != "" {
			c.mu.Unlock()
			return Call{}, &Refusal{"A call is already in progress."}
		}
		c.cur.inject = append(c.cur.inject, bot.Name+": "+req.Reason)
		c.mu.Unlock()
		return Call{}, ErrInjected
	}
	call := Call{ID: uuid.NewString(), BotID: bot.ID, Direction: Outbound, RuleID: req.RuleID, Reason: req.Reason,
		Status: StatusDialing, StartedAt: c.now().UnixMilli()}
	l := &live{call: call, botName: bot.Name, brief: req.Brief, opener: make(chan string, 1), done: make(chan struct{}),
		verify: req.Verify, urgent: req.Urgent}
	c.cur = l
	c.seed++
	seed := c.seed
	c.mu.Unlock()

	if err := c.store.InsertCall(ctx, &call); err != nil {
		c.clear(l)
		return Call{}, err
	}
	if req.Verify == "" {
		if id, err := c.host.PostCallCard(ctx, bot.ID, c.card(call)); err == nil {
			c.mu.Lock()
			l.call.MessageID = id
			c.mu.Unlock()
		}
	}
	conv, sid, err := c.dial.OutboundCall(ctx, OutboundCall{AgentID: s.AgentID, PhoneNumberID: s.PhoneNumberID, To: s.OwnerNumber, CallID: call.ID,
		BotID: bot.ID, VoiceID: or2(bot.Config.Jarvis.VoiceID, s.DefaultVoiceID), TTSModel: or2(bot.Config.Jarvis.TTSModel, s.DefaultTTSModel)})
	if err != nil {
		c.finish(ctx, l, StatusFailed, 0)
		if req.Verify == "" {
			c.host.Notify(bot.Name+" could not call", err.Error(), "bot:"+bot.ID, "error")
		}
		return Call{}, err
	}
	c.mu.Lock()
	l.call.ConversationID, l.call.ProviderCallID = conv, sid
	snap := l.call
	c.mu.Unlock()
	_ = c.store.UpdateCall(ctx, snap)
	c.host.Audit("jarvis.call.started", call.ID, "allowed", bot.Name+": "+req.Reason)
	c.spawn(func() { c.monitor(l) })

	// Write the opener while the phone rings.
	octx := context.WithoutCancel(ctx)
	c.spawn(func() {
		if req.Verify != "" {
			l.opener <- "Your verification code is " + spaced(req.Verify) + ". Again, " + spaced(req.Verify) + "."
			return
		}
		cctx, cancel := context.WithTimeout(octx, 15*time.Second)
		defer cancel()
		text, err := c.host.Compose(cctx, bot.ID, openerInstruction(bot.Name, req.Reason, req.Brief))
		if err == nil {
			if t, ok := CheckOpener(bot.Name, text); ok {
				l.opener <- t
				return
			}
		}
		l.opener <- FallbackOpener(bot.Name, req.Reason, seed)
	})
	return snap, nil
}

func openerInstruction(name, reason, brief string) string {
	return fmt.Sprintf("You are phoning the owner right now. Write the very first sentence you will say when they pick up. "+
		"Start with your name (%s) in a natural way, then say why you are calling: %s. One or two short sentences, no greeting formula, "+
		"never name the app. Context:\n%s", name, reason, brief)
}

func spaced(digits string) string { return strings.Join(strings.Split(digits, ""), " ") }

func or2(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (c *Calls) card(call Call) chat.MsgPart {
	dur := int64(0)
	if call.AnsweredAt > 0 && call.EndedAt > 0 {
		dur = call.EndedAt - call.AnsweredAt
	}
	summary := ""
	if call.EstCostMicros > 0 {
		summary = fmt.Sprintf("est. $%.2f", float64(call.EstCostMicros)/1e6)
	}
	return chat.MsgPart{Type: chat.PartCall, ID: call.ID, Name: call.Direction, Status: call.Status, Title: call.Reason, DurationMs: dur, Summary: summary}
}

func (c *Calls) HandleFrame(ctx context.Context, f Frame) {
	switch f.Type {
	case FrameLLMRequest:
		c.turn(ctx, f)
	case FrameInboundLookup:
		c.inbound(ctx, f)
	case FrameCallStatus:
		c.status(ctx, f)
	}
}

// ---- spoken output ----

// appNameRE matches the app's name as the model may have picked it up from tool text.
var appNameRE = regexp.MustCompile(`(?i)orole(?: os\b)?`)

const holdTarget = "orole os"

// sanitizer rewrites "Orole" / "Orole OS" to "the app" in a stream of text, so the voice never names the app.
// Matches that straddle chunk boundaries are handled by holding back a trailing partial match (at most 8 bytes)
// until the next chunk or Flush. Occurrences inside the bot's own name (e.g. "Orolebot") are left alone.
type sanitizer struct {
	bot  *regexp.Regexp // nil unless the bot's name contains the app name
	name string
	keep int // bytes of already-emitted text kept as context for bot-name matching
	ctx  string
	pend string
}

func newSanitizer(botName string) *sanitizer {
	s := &sanitizer{}
	if botName != "" && appNameRE.MatchString(botName) {
		s.bot = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(botName))
		s.keep = len(botName)
		s.name = botName
	}
	return s
}

// hold is how many trailing bytes of pend must wait for the next chunk: a partial app name, or a partial bot name
// (so a chunk boundary inside the bot's own name never lets the app-name match fire on its first half).
func (s *sanitizer) hold() int {
	n := holdLen(s.pend)
	for k := min(len(s.name)-1, len(s.pend)); k > n; k-- {
		if strings.EqualFold(s.pend[len(s.pend)-k:], s.name[:k]) {
			return k
		}
	}
	return n
}

func (s *sanitizer) Write(t string) string { s.pend += t; return s.drain(false) }
func (s *sanitizer) Flush() string         { return s.drain(true) }

// holdLen is the length of the longest suffix of t that could still grow into a match.
func holdLen(t string) int {
	for n := min(len(holdTarget), len(t)); n > 0; n-- {
		if strings.EqualFold(t[len(t)-n:], holdTarget[:n]) {
			return n
		}
	}
	return 0
}

func (s *sanitizer) drain(final bool) string {
	full := s.ctx + s.pend
	start := len(s.ctx)
	end := len(full)
	if !final {
		end -= s.hold()
	}
	var spans [][]int
	if s.bot != nil {
		spans = s.bot.FindAllStringIndex(full, -1)
	}
	inBot := func(a, b int) bool {
		for _, sp := range spans {
			if sp[0] <= a && b <= sp[1] {
				return true
			}
		}
		return false
	}
	var out strings.Builder
	pos := start
	for _, m := range appNameRE.FindAllStringIndex(full[:end], -1) {
		if m[0] < start || inBot(m[0], m[1]) {
			continue
		}
		out.WriteString(full[pos:m[0]])
		out.WriteString("the app")
		pos = m[1]
	}
	out.WriteString(full[pos:end])
	emitted := out.String()
	s.pend = full[end:]
	if s.keep > 0 {
		c := s.ctx + emitted
		if len(c) > s.keep {
			c = c[len(c)-s.keep:]
			for len(c) > 0 && !utf8.RuneStart(c[0]) {
				c = c[1:]
			}
		}
		s.ctx = c
	}
	return emitted
}

// speaker streams one turn's spoken text to the relay, sanitized and in order, and remembers when audio first left.
type speaker struct {
	c          *Calls
	ctx        context.Context
	quiet      context.Context // when done, this turn was superseded or aborted and must stay silent
	id, callID string

	mu         sync.Mutex
	san        *sanitizer
	first      time.Time
	lastFiller string
}

func (c *Calls) newSpeaker(ctx context.Context, id, callID, botName string) *speaker {
	return &speaker{c: c, ctx: ctx, id: id, callID: callID, san: newSanitizer(botName)}
}

func (s *speaker) emit(out string) {
	if out == "" || (s.quiet != nil && s.quiet.Err() != nil) {
		return
	}
	if s.first.IsZero() {
		s.first = s.c.now()
	}
	_ = s.c.relay.Send(s.ctx, Frame{Type: FrameLLMDelta, ID: s.id, CallID: s.callID, Text: out})
}

func (s *speaker) say(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emit(s.san.Write(text))
}

func (s *speaker) filler(tool string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := filler(tool)
	if f == s.lastFiller {
		return
	}
	s.lastFiller = f
	s.emit(s.san.Write(f + " "))
}

// finish flushes held text and closes the turn.
func (s *speaker) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emit(s.san.Flush())
	if s.quiet != nil && s.quiet.Err() != nil {
		return
	}
	_ = s.c.relay.Send(s.ctx, Frame{Type: FrameLLMDone, ID: s.id, CallID: s.callID})
}

// logTurn records timings only; never any spoken or transcribed text.
func (c *Calls) logTurn(sp *speaker, start time.Time, opener bool) {
	end := c.now()
	sp.mu.Lock()
	first := int64(-1)
	if !sp.first.IsZero() {
		first = sp.first.Sub(start).Milliseconds()
	}
	sp.mu.Unlock()
	c.log.Info("jarvis: turn", "call", sp.callID, "first_audio_ms", first, "total_ms", end.Sub(start).Milliseconds(), "opener", opener)
}

func (c *Calls) hangup(ctx context.Context, callID string) {
	_ = c.relay.Send(ctx, Frame{Type: FrameHangup, CallID: callID})
}

func (c *Calls) turn(ctx context.Context, f Frame) {
	start := c.now()
	c.mu.Lock()
	l := c.cur
	if l == nil || l.call.ID != f.CallID || l.ended {
		c.mu.Unlock()
		sp := c.newSpeaker(ctx, f.ID, f.CallID, "")
		sp.say("Sorry, this call has ended. I'll message you instead.")
		sp.finish()
		c.hangup(ctx, f.CallID)
		return
	}
	if l.call.AnsweredAt == 0 {
		l.call.AnsweredAt = c.now().UnixMilli()
		l.call.Status = StatusInProgress
	}
	first := !l.spoken
	l.spoken = true
	botID, botName, reason := l.call.BotID, l.botName, l.call.Reason
	c.mu.Unlock()
	sp := c.newSpeaker(ctx, f.ID, f.CallID, botName)

	utter := lastUser(f.Messages)
	if first && (utter == "" || l.verify != "") {
		var text string
		select {
		case text = <-l.opener:
		default:
			select {
			case text = <-l.opener:
			case <-time.After(3 * time.Second):
				text = FallbackOpener(botName, reason, 0)
			case <-ctx.Done():
				return
			}
		}
		c.mu.Lock()
		l.history = append(l.history, chat.VoiceLine{Role: "assistant", Text: text})
		c.mu.Unlock()
		sp.say(text)
		sp.finish()
		if l.verify != "" {
			c.hangup(ctx, f.CallID)
		}
		c.logTurn(sp, start, true)
		return
	}
	if l.verify != "" || utter == "" {
		sp.finish()
		return
	}
	// A new request for this call means the owner barged in: cancel the in-flight turn and wait for it to release
	// chat's run slot before starting this one. The superseded turn stays silent and leaves no history.
	tctx, tcancel := context.WithCancel(ctx)
	sp.quiet = tctx
	myDone := make(chan struct{})
	c.mu.Lock()
	prevCancel, prevDone := l.turnCancel, l.turnDone
	l.turnCancel, l.turnDone = tcancel, myDone
	c.mu.Unlock()
	defer func() {
		tcancel()
		close(myDone)
		c.mu.Lock()
		if l.turnDone == myDone {
			l.turnCancel, l.turnDone = nil, nil
		}
		c.mu.Unlock()
	}()
	if prevCancel != nil {
		prevCancel()
		select {
		case <-prevDone:
		case <-ctx.Done():
			return
		}
	}
	if tctx.Err() != nil {
		return
	}
	bot, err := c.host.Bot(tctx, botID)
	if err == nil {
		err = c.host.Gate(tctx, bot)
	}
	if err != nil {
		var ge *GateError
		if errors.As(err, &ge) {
			c.host.DisableJarvis(ctx, botID, ge.Reason)
			sp.say("I can't act on that: " + ge.Reason)
			sp.finish()
			c.hangup(ctx, f.CallID)
			return
		}
		sp.say("I can't do that right now.")
		sp.finish()
		return
	}
	c.mu.Lock()
	brief := l.brief
	if len(l.inject) > 0 {
		brief += "\n\nNews since the call started:\n- " + strings.Join(l.inject, "\n- ")
	}
	history := append([]chat.VoiceLine(nil), l.history...)
	c.mu.Unlock()
	reply, err := c.host.VoiceTurn(tctx, chat.VoiceRequest{BotID: bot.ID, CallID: f.CallID, Text: utter, Brief: brief, History: history,
		OnText: sp.say, OnTool: sp.filler})
	if tctx.Err() != nil {
		return // superseded or aborted: no more speech, no history
	}
	if err != nil {
		sp.say(" That failed: " + firstSentence(err.Error()))
	}
	c.mu.Lock()
	l.history = append(l.history, chat.VoiceLine{Role: "user", Text: utter})
	if reply != "" {
		l.history = append(l.history, chat.VoiceLine{Role: "assistant", Text: reply})
	}
	if len(l.history) > maxHistory {
		l.history = l.history[len(l.history)-maxHistory:]
	}
	c.mu.Unlock()
	sp.finish()
	c.logTurn(sp, start, false)
}

func lastUser(ms []LLMMessage) string {
	for i := len(ms) - 1; i >= 0; i-- {
		switch ms[i].Role {
		case "user":
			return strings.TrimSpace(ms[i].Content)
		case "assistant":
			return ""
		}
	}
	return ""
}

func firstSentence(s string) string {
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		return s[:i+1]
	}
	return s
}

var fillers = map[string]string{
	"list_tasks": "Checking your tasks.", "task_status": "Looking at that task.", "start_task": "Starting that coding task now.",
	"message_task": "Passing that on.", "stop_task": "Stopping it.", "list_automations": "Checking your automations.",
	"automation_runs": "Checking the runs.", "research": "Starting the research.", "web_search": "Looking that up.",
	"web_fetch": "Reading that.", "switch_bot": "One moment.",
}

func filler(tool string) string {
	if f, ok := fillers[tool]; ok {
		return f
	}
	return "One moment."
}

// ---- inbound ----

func (c *Calls) inbound(ctx context.Context, f Frame) {
	s := c.settings()
	reject := func(why string) {
		_ = c.relay.Send(ctx, Frame{Type: FrameInboundAnswer, ID: f.ID, Accept: false})
		c.host.Audit("jarvis.inbound.rejected", f.Caller, "denied", why)
	}
	if !s.OwnerVerified || f.Caller == "" || f.Caller != s.OwnerNumber {
		reject("caller is not the verified owner number")
		return
	}
	bot, ok := c.pickInboundBot(ctx)
	if !ok {
		reject("no bot has Jarvis mode on")
		return
	}
	call := Call{ID: uuid.NewString(), BotID: bot.ID, Direction: Inbound, Reason: "You called", Status: StatusInProgress,
		StartedAt: c.now().UnixMilli(), AnsweredAt: c.now().UnixMilli(), ProviderCallID: f.CallSID, ConversationID: f.ConversationID}
	l := &live{call: call, botName: bot.Name, opener: make(chan string, 1), done: make(chan struct{})}
	c.mu.Lock()
	if c.cur != nil || c.closed {
		c.mu.Unlock()
		reject("a call is already in progress")
		return
	}
	c.cur = l
	c.mu.Unlock()
	if err := c.store.InsertCall(ctx, &call); err != nil {
		c.clear(l)
		reject("could not record the call")
		return
	}
	if id, err := c.host.PostCallCard(ctx, bot.ID, c.card(call)); err == nil {
		c.mu.Lock()
		l.call.MessageID = id
		snap := l.call
		c.mu.Unlock()
		_ = c.store.UpdateCall(ctx, snap)
	}
	octx := context.WithoutCancel(ctx)
	c.spawn(func() {
		cctx, cancel := context.WithTimeout(octx, 3*time.Second)
		defer cancel()
		text, err := c.host.Compose(cctx, bot.ID, fmt.Sprintf("The owner just phoned you. Write your greeting: say your name (%s) naturally and ask what they need. One short sentence, never name the app.", bot.Name))
		if t, ok := CheckOpener(bot.Name, text); err == nil && ok {
			l.opener <- t
			return
		}
		l.opener <- "Hey, " + bot.Name + " here. What do you need?"
	})
	tts := map[string]string{}
	if v := or2(bot.Config.Jarvis.VoiceID, s.DefaultVoiceID); v != "" {
		tts["voice_id"] = v
	}
	if m := or2(bot.Config.Jarvis.TTSModel, s.DefaultTTSModel); m != "" {
		tts["model_id"] = m
	}
	ov, _ := json.Marshal(map[string]any{"tts": tts})
	_ = c.relay.Send(ctx, Frame{Type: FrameInboundAnswer, ID: f.ID, Accept: true, Vars: map[string]string{"call_id": call.ID, "bot_id": bot.ID}, Overrides: ov})
	c.host.Notify("Incoming call to "+bot.Name, "From your phone number.", "bot:"+bot.ID, "info")
	c.host.Audit("jarvis.call.started", call.ID, "allowed", "inbound to "+bot.Name)
	c.spawn(func() { c.monitor(l) })
}

// pickInboundBot returns the first executive bot that passes the gate, else the first bot that does.
func (c *Calls) pickInboundBot(ctx context.Context) (chat.Bot, bool) {
	bots, err := c.host.Bots(ctx)
	if err != nil {
		return chat.Bot{}, false
	}
	var fallback *chat.Bot
	for i := range bots {
		b := bots[i]
		if c.host.Gate(ctx, b) != nil {
			continue
		}
		if b.Config.Executive {
			return b, true
		}
		if fallback == nil {
			fallback = &bots[i]
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return chat.Bot{}, false
}

func (c *Calls) SwitchBot(ctx context.Context, callID, name string) (string, error) {
	bots, err := c.host.Bots(ctx)
	if err != nil {
		return "", err
	}
	for _, b := range bots {
		if !strings.EqualFold(b.Name, strings.TrimSpace(name)) {
			continue
		}
		if err := c.host.Gate(ctx, b); err != nil {
			return "", fmt.Errorf("%s isn't set up for calls", b.Name)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.cur == nil || c.cur.call.ID != callID || c.cur.ended {
			return "", errors.New("this call has ended")
		}
		c.cur.call.BotID, c.cur.botName = b.ID, b.Name
		c.cur.history = nil
		return fmt.Sprintf("Handed over. %s answers from the next sentence and should introduce itself by name.", b.Name), nil
	}
	return "", fmt.Errorf("there is no bot called %s", name)
}

// ---- ending ----

func (c *Calls) status(ctx context.Context, f Frame) {
	c.mu.Lock()
	l := c.cur
	if l == nil || l.call.ID != f.CallID || l.ended {
		c.mu.Unlock()
		return
	}
	if f.ConversationID != "" && l.call.ConversationID == "" {
		l.call.ConversationID = f.ConversationID
	}
	answered := l.call.AnsweredAt != 0
	c.mu.Unlock()
	st := StatusCompleted
	switch strings.ToLower(f.Status) {
	case "voicemail", "voicemail_detected":
		st = StatusVoicemail
	case "no-answer", "no_answer", "busy", "canceled":
		st = StatusNoAnswer
	case "failed":
		st = StatusFailed
	}
	if st == StatusCompleted && !answered {
		st = StatusNoAnswer
	}
	c.finish(ctx, l, st, f.DurationSecs)
}

// monitor ends the call even if the post-call webhook never arrives: it polls the provider while the call is live
// and enforces a hard ceiling. finish is idempotent, so whichever path ends the call first wins.
func (c *Calls) monitor(l *live) {
	ceiling := c.after(maxCallDuration)
	for {
		select {
		case <-c.baseCtx.Done():
			return
		case <-l.done:
			return
		case <-ceiling:
			if c.finish(c.baseCtx, l, StatusCompleted, 0) {
				c.hangup(c.baseCtx, l.call.ID)
			}
			return
		case <-c.after(pollEvery):
			c.mu.Lock()
			conv := l.call.ConversationID
			answered := l.call.AnsweredAt != 0
			c.mu.Unlock()
			if conv == "" {
				continue
			}
			pctx, cancel := context.WithTimeout(c.baseCtx, 10*time.Second)
			st, secs, err := c.dial.ConversationStatus(pctx, conv)
			cancel()
			if err != nil {
				continue
			}
			switch strings.ToLower(st) {
			case "done":
				end := StatusCompleted
				if !answered {
					end = StatusNoAnswer
				}
				c.finish(c.baseCtx, l, end, secs)
			case "failed":
				c.finish(c.baseCtx, l, StatusFailed, secs)
			}
		}
	}
}

// finish ends the call exactly once and reports whether this invocation was the one that ended it.
func (c *Calls) finish(ctx context.Context, l *live, status string, durationSecs int) bool {
	c.mu.Lock()
	if l.ended {
		c.mu.Unlock()
		return false
	}
	l.ended = true
	close(l.done)
	l.call.Status = status
	l.call.EndedAt = c.now().UnixMilli()
	if durationSecs <= 0 && l.call.AnsweredAt > 0 {
		durationSecs = int((l.call.EndedAt - l.call.AnsweredAt) / 1000)
	}
	if durationSecs > 0 {
		l.call.EstCostMicros = int64(durationSecs) * int64(c.settings().centsPerMinute()) * 10_000 / 60
	}
	call := l.call
	name := l.botName
	brief := l.brief
	if c.cur == l {
		c.cur = nil
	}
	c.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	_ = c.store.UpdateCall(ctx, call)
	if call.MessageID != "" {
		_ = c.host.UpdateCallCard(ctx, call.MessageID, c.card(call))
	}
	c.host.Audit("jarvis.call.ended", call.ID, "allowed", status)
	if (status == StatusNoAnswer || status == StatusVoicemail) && l.verify == "" && call.Direction == Outbound {
		c.host.Notify(name+" tried to call", call.Reason, "bot:"+call.BotID, "warning")
		if l.urgent {
			c.spawn(func() { c.retry(call, brief) })
		}
	}
	return true
}

// retry places one more attempt after an unanswered urgent call; the retry itself is not urgent, so it never repeats.
func (c *Calls) retry(call Call, brief string) {
	select {
	case <-c.after(urgentRetryWait):
	case <-c.baseCtx.Done():
		return
	}
	c.Retry(c.baseCtx, PlaceRequest{BotID: call.BotID, Reason: call.Reason, Brief: brief, RuleID: call.RuleID})
}

func (c *Calls) clear(l *live) {
	c.mu.Lock()
	if c.cur == l {
		c.cur = nil
	}
	c.mu.Unlock()
}

package jarvis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
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
	// outcomeDelay, outcomeRetryDelay and outcomeCheckTimeout time the check after a call ends: how long ElevenLabs gets
	// to record the conversation, how much longer it gets when the conversation is still open, and how long each
	// request of the check may take.
	outcomeDelay        = 10 * time.Second
	outcomeRetryDelay   = 30 * time.Second
	outcomeCheckTimeout = 30 * time.Second
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
	// ConversationOutcome reads how the voice provider recorded a finished conversation (see checkOutcome).
	ConversationOutcome(ctx context.Context, conversationID string) (Outcome, error)
	// FindConversation finds a call's conversation by its carrier call SID when the conversation id was never recorded;
	// "" with no error means none matched.
	FindConversation(ctx context.Context, agentID, callSID string, near time.Time) (string, error)
}

// Phone controls a live call at the carrier (Twilio) by its call SID: hanging up and reading its status.
type Phone interface {
	Hangup(ctx context.Context, callSID string) error
	CallStatus(ctx context.Context, callSID string) (string, error)
}

type Sender interface {
	Send(ctx context.Context, f Frame) error
	Connected() bool
}

type PlaceRequest struct {
	BotID, Reason, Brief, RuleID string
	Urgent                       bool
	QuietOverride                bool   // the rule may call during quiet hours; carried to the urgent retry
	Verify                       string // digits for a phone verification call; no bot turns, BotID may be empty
}

// live is the in-memory state of the one call in progress. Mutable fields are guarded by Calls.mu;
// opener, done, verify and urgent are fixed at creation.
type live struct {
	call    CallRecord
	botName string
	brief   string
	lang    string     // call language, fixed when the call starts (PhoneSettings.callLanguage)
	inject  []injected // news folded into the call; inject[:delivered] reached the bot in a finished turn
	history []chat.VoiceLine
	// delivered counts inject lines a completed model turn has seen; the rest become a notification at the end.
	delivered int
	spoken    bool // the first turn (opener) has been taken
	ended     bool
	// reasonGiven is set once a model turn has completed: the reason has been given, so the voicemail check stops.
	reasonGiven bool
	// silenced is set when voicemail is detected: no turn may speak or start from then on.
	silenced bool

	opener chan string   // receives the opener exactly once (buffered)
	done   chan struct{} // closed when the call ends
	verify string
	urgent bool
	quiet  bool // QuietOverride of the request, for the urgent retry

	turnCancel context.CancelFunc // in-flight model turn, if any
	turnDone   chan struct{}
	// The in-flight turn's speaker, utterance and request size: a request that repeats them is ElevenLabs retrying
	// the same turn (it re-sends when no text arrived in time), not the owner barging in; see Calls.adopt.
	turnSp    *speaker
	turnUtter string
	turnMsgs  int
	// hangAfter is set by the end_call tool: the call ends once the current reply has been spoken.
	hangAfter bool

	// persistMu serializes writes of this call's row and card; see Calls.persist.
	persistMu sync.Mutex
}

// injected is a call request folded into the call in progress.
type injected struct{ bot, reason string }

type Calls struct {
	store    *Store
	host     Host
	dial     Dialer
	phone    Phone
	relay    Sender
	settings func() PhoneSettings
	now      func() time.Time
	log      *slog.Logger
	after    func(time.Duration) <-chan time.Time // injectable for tests
	// ackAfter and stillAfter time the keep-alive lines of a slow turn (see speaker.keepAlive; tests shorten them).
	ackAfter, stillAfter time.Duration
	// speechWait is how long to let the voice finish a line before hanging up at the carrier (tests use 0).
	speechWait func(text string) time.Duration
	// outcomeAfter is how long after a call ends to wait before asking the voice provider how it recorded it (it needs
	// a moment), outcomeRetryAfter the wait before a second look when the conversation is not finished then, and
	// outcomeTimeout bounds each request of that check (see checkOutcome).
	outcomeAfter, outcomeRetryAfter, outcomeTimeout time.Duration
	// Retry places the second attempt after an unanswered urgent call. The daemon points it at the policy-checked path
	// (caps, quiet hours); the default calls Place directly.
	Retry func(ctx context.Context, req PlaceRequest)
	// Changed, if set, is called (and must not block) when a call is recorded or ends.
	Changed func()
	// afterAnswer, if set, runs after a turn marks the call answered and before that is persisted (tests only).
	afterAnswer func()

	baseCtx context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup

	mu     sync.Mutex
	cur    *live
	seed   int
	closed bool
}

func NewCalls(store *Store, host Host, dial Dialer, phone Phone, relay Sender, settings func() PhoneSettings, now func() time.Time, log *slog.Logger) *Calls {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Calls{store: store, host: host, dial: dial, phone: phone, relay: relay, settings: settings, now: now, log: log,
		after: time.After, speechWait: speechTime, ackAfter: ackAfter, stillAfter: stillAfter,
		outcomeAfter: outcomeDelay, outcomeRetryAfter: outcomeRetryDelay, outcomeTimeout: outcomeCheckTimeout,
		baseCtx: ctx, stop: cancel}
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

func (c *Calls) Active() (CallRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur == nil {
		return CallRecord{}, false
	}
	return c.cur.call, true
}

func (c *Calls) Place(ctx context.Context, req PlaceRequest) (CallRecord, error) {
	s := c.settings()
	if s.AgentID == "" || s.PhoneNumberID == "" || s.OwnerNumber == "" {
		return CallRecord{}, &Refusal{"Phone calls are not set up yet."}
	}
	if !c.relay.Connected() {
		return CallRecord{}, &Refusal{"The relay is not connected."}
	}
	var bot chat.Bot
	if req.Verify == "" || req.BotID != "" {
		var err error
		if bot, err = c.host.Bot(ctx, req.BotID); err != nil {
			return CallRecord{}, err
		}
	}
	if req.Verify == "" {
		if err := c.host.Gate(ctx, bot); err != nil {
			return CallRecord{}, err
		}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return CallRecord{}, errors.New("jarvis: shutting down")
	}
	if c.cur != nil {
		if c.cur.verify != "" || req.Verify != "" {
			c.mu.Unlock()
			return CallRecord{}, &Refusal{"A call is already in progress."}
		}
		c.cur.inject = append(c.cur.inject, injected{bot: bot.Name, reason: clipRunes(req.Reason, maxTitle)})
		c.mu.Unlock()
		return CallRecord{}, ErrInjected
	}
	call := CallRecord{ID: uuid.NewString(), BotID: bot.ID, Direction: Outbound, RuleID: req.RuleID, Reason: clipRunes(req.Reason, maxTitle),
		Status: StatusDialing, StartedAt: c.now().UnixMilli()}
	brief := req.Brief
	if req.Verify == "" {
		brief += "\n\n" + openedNote
	}
	lang := s.spokenLanguage()
	l := &live{call: call, botName: bot.Name, brief: brief, lang: lang, opener: make(chan string, 1), done: make(chan struct{}),
		verify: req.Verify, urgent: req.Urgent, quiet: req.QuietOverride}
	c.cur = l
	c.seed++
	seed := c.seed
	c.mu.Unlock()

	if err := c.store.InsertCall(ctx, &call); err != nil {
		c.clear(l)
		return CallRecord{}, err
	}
	c.changed()
	if req.Verify == "" {
		if id, err := c.host.PostCallCard(ctx, bot.ID, c.card(call)); err == nil {
			c.mu.Lock()
			l.call.MessageID = id
			c.mu.Unlock()
		}
	}
	conv, sid, err := c.dial.OutboundCall(ctx, OutboundCall{AgentID: s.AgentID, PhoneNumberID: s.PhoneNumberID, To: s.OwnerNumber, CallID: call.ID,
		BotID: bot.ID, VoiceID: or2(bot.Config.Jarvis.VoiceID, s.DefaultVoiceID), TTSModel: ttsModel(lang, or2(bot.Config.Jarvis.TTSModel, s.DefaultTTSModel))})
	if err != nil {
		c.finish(ctx, l, StatusFailed, 0)
		if req.Verify == "" {
			c.host.Notify(bot.Name+" could not call", err.Error(), "bot:"+bot.ID, "error")
		}
		return CallRecord{}, err
	}
	c.mu.Lock()
	l.call.ConversationID, l.call.ProviderCallID = conv, sid
	snap := l.call
	c.mu.Unlock()
	c.persist(ctx, l, false)
	c.host.Audit("jarvis.call.started", call.ID, "allowed", bot.Name+": "+req.Reason)
	c.spawn(func() { c.monitor(l) })

	// Write the opener while the phone rings.
	octx := context.WithoutCancel(ctx)
	c.spawn(func() {
		if req.Verify != "" {
			l.opener <- phrasesFor(lang).verifyLine(req.Verify)
			return
		}
		cctx, cancel := context.WithTimeout(octx, 15*time.Second)
		defer cancel()
		text, err := c.host.Compose(cctx, bot.ID, openerInstruction(lang, bot.Name))
		if err == nil {
			if t, ok := CheckOpener(bot.Name, text); ok {
				l.opener <- t
				return
			}
		}
		l.opener <- FallbackOpener(lang, bot.Name, seed)
	})
	return snap, nil
}

// openerInstruction asks for a name-only greeting in the call language. It carries no reason and no context: the call
// may reach voicemail, and the opener is spoken before anyone has answered.
func openerInstruction(lang, name string) string {
	return fmt.Sprintf("You are phoning the owner right now. Write the very first sentence you will say when they pick up: "+
		"a short, natural greeting with your name (%s) that asks whether they have a minute. Do not say why you are calling yet; "+
		"the call may reach voicemail. One short sentence, never name the app.", name) + phrasesFor(lang).writeIn
}

// openedNote tells the bot, in its call brief, that the opener did not give the reason.
const openedNote = "Your opening line only greeted the owner by name. When they answer, say why you are calling before anything else. " +
	"If you reach voicemail or an answering machine, say nothing about the reason."

// maxTitle caps event titles and call reasons that reach a brief, a card or a notification.
const maxTitle = 200

func spaced(digits string) string { return strings.Join(strings.Split(digits, ""), " ") }

func or2(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (c *Calls) card(call CallRecord) chat.MsgPart {
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
	lines      phrases // the call language's fixed lines

	mu         sync.Mutex
	san        *sanitizer
	first      time.Time
	lastReal   time.Time // wall-clock time text last left; the keep-alive speaks after a silence
	lastFiller string
	tools      []string // tools this turn started, for the note a superseded turn leaves
	said       strings.Builder
	closed     bool // done was sent: nothing more may go out
	acked      bool // the "one moment" line was spoken
	scripted   bool // the last text out was a scripted line (acknowledgement or filler), not the model's
	nudges     int  // "still working" lines spoken
}

func (c *Calls) newSpeaker(ctx context.Context, id, callID, botName, lang string) *speaker {
	return &speaker{c: c, ctx: ctx, id: id, callID: callID, lines: phrasesFor(lang), san: newSanitizer(botName)}
}

func (s *speaker) emit(out string) {
	if out == "" || s.closed || (s.quiet != nil && s.quiet.Err() != nil) {
		return
	}
	now := s.c.now()
	if s.first.IsZero() {
		s.first = now
	}
	s.lastReal = time.Now()
	s.said.WriteString(out)
	_ = s.c.relay.Send(s.ctx, Frame{Type: FrameLLMDelta, ID: s.id, CallID: s.callID, Text: out})
}

func (s *speaker) say(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.san.Write(text)
	if strings.TrimSpace(out) != "" {
		s.scripted = false
	}
	s.emit(out)
}

// line speaks a scripted line (acknowledgement, filler) as its own sentence. The caller holds s.mu.
func (s *speaker) line(text string) {
	s.emit(s.san.Write(text + " "))
	s.scripted = true
}

// spoken returns everything this turn has said so far.
func (s *speaker) spoken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.said.String()
}

// retarget sends the rest of this turn to a newer request for the same turn. ElevenLabs re-sends a request when no
// text arrived quickly enough and stops reading the old one, which is closed here.
func (s *speaker) retarget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.id == id {
		return
	}
	_ = s.c.relay.Send(s.ctx, Frame{Type: FrameLLMDone, ID: s.id, CallID: s.callID})
	s.id = id
}

// ackAfter is how long a turn may stay silent before the bot says it is on it; stillAfter is the silence before a
// "still working" line while a tool runs; maxNudges caps those per turn. Speaking early also keeps ElevenLabs from
// re-sending the request.
const (
	ackAfter   = 1500 * time.Millisecond
	stillAfter = 12 * time.Second
	maxNudges  = 3
)

// keepAlive fills silences of a running turn: "one moment" when nothing was said yet, and now and then a "still
// working" line during a long tool run. It stops when ctx ends or the turn is closed.
func (s *speaker) keepAlive(ctx context.Context) {
	wait := s.c.ackAfter
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		s.mu.Lock()
		if s.closed || s.nudges >= maxNudges {
			s.mu.Unlock()
			return
		}
		switch {
		case s.first.IsZero():
			s.acked = true
			s.lastFiller = s.lines.ackLine(s.c.now().Nanosecond())
			s.line(s.lastFiller)
		case time.Since(s.lastReal) >= s.c.stillAfter:
			s.nudges++
			s.line(s.lines.stillLine(s.nudges))
		}
		wait = max(s.c.stillAfter-time.Since(s.lastReal), s.c.stillAfter/10)
		s.mu.Unlock()
	}
}

func (s *speaker) filler(tool string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(s.tools, tool) {
		s.tools = append(s.tools, tool)
	}
	f := s.lines.fillerLine(tool)
	if f == s.lastFiller || (s.acked && s.scripted) {
		// Already said, or the acknowledgement was the last thing said: one scripted line in a row is enough.
		return
	}
	s.lastFiller = f
	s.line(f)
}

func (s *speaker) toolsRan() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tools...)
}

// finish flushes held text and closes the turn.
func (s *speaker) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emit(s.san.Flush())
	if s.closed || (s.quiet != nil && s.quiet.Err() != nil) {
		return
	}
	s.closed = true
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

// hangup ends a call at the carrier. Without a call SID there is nothing to end; the provider's own timeouts apply.
func (c *Calls) hangup(ctx context.Context, callSID string) {
	if callSID == "" || c.phone == nil {
		return
	}
	hctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := c.phone.Hangup(hctx, callSID); err != nil {
		c.log.Warn("jarvis: hang-up failed", "err", err)
	}
}

// hangupAfter hangs up once the voice has had time to say text (a goodbye or the verification code).
func (c *Calls) hangupAfter(callSID, text string) {
	wait := c.speechWait(text)
	if wait <= 0 {
		c.hangup(c.baseCtx, callSID)
		return
	}
	c.spawn(func() {
		select {
		case <-c.after(wait):
			c.hangup(c.baseCtx, callSID)
		case <-c.baseCtx.Done():
		}
	})
}

// speechTime estimates how long the voice needs for text: about 150 words a minute plus a margin.
func speechTime(text string) time.Duration {
	return 2*time.Second + time.Duration(len(strings.Fields(text)))*400*time.Millisecond
}

// silent ends a turn without a word.
func (c *Calls) silent(ctx context.Context, f Frame) {
	_ = c.relay.Send(ctx, Frame{Type: FrameLLMDone, ID: f.ID, CallID: f.CallID})
}

// endUnknown answers a turn for a call that is not live: it says nothing and, if the call is on record with a
// carrier id, hangs it up.
func (c *Calls) endUnknown(ctx context.Context, f Frame) {
	c.silent(ctx, f)
	if f.CallID == "" {
		return
	}
	if rec, err := c.store.GetCall(ctx, f.CallID); err == nil {
		c.hangup(ctx, rec.ProviderCallID)
	}
}

func (c *Calls) turn(ctx context.Context, f Frame) {
	start := c.now()
	utter := lastUser(f.Messages)
	if isSilence(utter) {
		// ElevenLabs sends "..." when the owner has been quiet for a while: that is a pause, not something to answer.
		utter = ""
	}
	c.mu.Lock()
	l := c.cur
	if l == nil || f.CallID == "" || l.call.ID != f.CallID || l.ended {
		c.mu.Unlock()
		c.endUnknown(ctx, f)
		return
	}
	verify := l.verify != ""
	sid := l.call.ProviderCallID
	outbound := l.call.Direction == Outbound
	if !verify && l.call.Direction == Outbound && !l.reasonGiven && utter != "" && IsVoicemail(utter) {
		// An answering machine, possibly after a greeting that passed as a person answering: say nothing more (cancel
		// a turn that is still speaking), end the call, and report it as unanswered.
		l.silenced = true
		cancel, done := l.turnCancel, l.turnDone
		c.mu.Unlock()
		if cancel != nil {
			cancel()
			select {
			case <-done:
			case <-ctx.Done():
			}
		}
		c.silent(ctx, f)
		c.hangup(ctx, sid)
		c.finish(ctx, l, StatusVoicemail, 0)
		c.log.Info("jarvis: voicemail detected", "call", f.CallID)
		return
	}
	answered := false
	if l.call.AnsweredAt == 0 && (utter != "" || verify) {
		l.call.AnsweredAt = c.now().UnixMilli()
		l.call.Status = StatusInProgress
		answered = true
	}
	first := !l.spoken
	l.spoken = true
	botName, lang := l.botName, l.lang
	c.mu.Unlock()
	if answered && c.afterAnswer != nil {
		c.afterAnswer()
	}
	if answered {
		c.persist(ctx, l, true)
		c.changed()
	}
	sp := c.newSpeaker(ctx, f.ID, f.CallID, botName, lang)

	if first {
		// The first turn always speaks the opener, whatever the owner already said.
		text, ok := c.awaitOpener(ctx, l, botName, lang)
		if !ok {
			return
		}
		c.mu.Lock()
		silenced := l.silenced
		c.mu.Unlock()
		if silenced {
			c.silent(ctx, f)
			return
		}
		sp.say(text)
		if verify {
			sp.finish()
			c.hangupAfter(sid, text)
			c.logTurn(sp, start, true)
			return
		}
		greeting := utter == "" || bareGreeting(utter)
		if outbound {
			// Before the reason, a first reply that asks for nothing may be the start of a machine greeting
			// ("Hey, it's Ray."): speak only the opener and wait for the next utterance.
			greeting = utter == "" || !asksSomething(utter)
		}
		c.mu.Lock()
		if utter != "" && greeting {
			l.history = append(l.history, chat.VoiceLine{Role: "user", Text: utter})
		}
		l.history = append(l.history, chat.VoiceLine{Role: "assistant", Text: text})
		c.mu.Unlock()
		if greeting {
			sp.finish()
			c.logTurn(sp, start, true)
			return
		}
		sp.say(" ") // the owner opened with a real request: answer it in the same response
	}
	if verify || utter == "" {
		sp.finish()
		return
	}
	if c.adopt(l, f, utter) {
		return
	}
	c.modelTurn(ctx, l, f, sp, utter, sid)
	c.logTurn(sp, start, first)
}

// isSilence reports whether an utterance has no words at all ("...", "", punctuation): ElevenLabs' silence prompt.
func isSilence(utter string) bool {
	return !strings.ContainsFunc(utter, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
}

// adopt hands a repeated request to the turn already answering it. ElevenLabs re-sends the same request (same last
// utterance, same number of messages) when the first text is slow; restarting the turn would cancel the work and
// make the next retry slower still. A request with anything new is a real barge-in and is not adopted.
func (c *Calls) adopt(l *live, f Frame, utter string) bool {
	c.mu.Lock()
	sp := l.turnSp
	same := sp != nil && l.turnUtter == utter && l.turnMsgs == len(f.Messages)
	c.mu.Unlock()
	if !same {
		return false
	}
	sp.retarget(f.ID)
	c.log.Info("jarvis: retried turn adopted", "call", f.CallID)
	return true
}

// EndAfterReply marks the call to end once the current reply has been spoken (the end_call tool).
func (c *Calls) EndAfterReply(callID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur == nil || c.cur.call.ID != callID || c.cur.ended {
		return errors.New("this call has ended")
	}
	c.cur.hangAfter = true
	return nil
}

// awaitOpener returns the opener written while the phone rang, or a fallback if it is late.
func (c *Calls) awaitOpener(ctx context.Context, l *live, botName, lang string) (string, bool) {
	select {
	case text := <-l.opener:
		return text, true
	default:
	}
	select {
	case text := <-l.opener:
		return text, true
	case <-time.After(3 * time.Second):
		return FallbackOpener(lang, botName, 0), true
	case <-ctx.Done():
		return "", false
	}
}

// modelTurn answers one utterance with the bot's model and tools.
func (c *Calls) modelTurn(ctx context.Context, l *live, f Frame, sp *speaker, utter, sid string) {
	// A new request for this call means the owner barged in: cancel the in-flight turn and wait for it to release
	// chat's run slot before starting this one. The superseded turn stays silent; it leaves only a note of the tools
	// it already ran.
	tctx, tcancel := context.WithCancel(ctx)
	sp.quiet = tctx
	myDone := make(chan struct{})
	c.mu.Lock()
	prevCancel, prevDone := l.turnCancel, l.turnDone
	l.turnCancel, l.turnDone = tcancel, myDone
	l.turnSp, l.turnUtter, l.turnMsgs = sp, utter, len(f.Messages)
	botID := l.call.BotID
	if l.silenced {
		tcancel() // voicemail: this turn must not start
	}
	c.mu.Unlock()
	defer func() {
		tcancel()
		close(myDone)
		c.mu.Lock()
		if l.turnDone == myDone {
			l.turnCancel, l.turnDone = nil, nil
			l.turnSp, l.turnUtter, l.turnMsgs = nil, "", 0
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
	go sp.keepAlive(tctx)
	bot, err := c.host.Bot(tctx, botID)
	if err == nil {
		err = c.host.Gate(tctx, bot)
	}
	if err != nil {
		var ge *GateError
		if errors.As(err, &ge) {
			c.host.DisableJarvis(ctx, botID, ge.Reason)
			line := sp.lines.refused + ge.Reason
			sp.say(line)
			sp.finish()
			c.hangupAfter(sid, line)
			return
		}
		sp.say(sp.lines.cannot)
		sp.finish()
		return
	}
	c.mu.Lock()
	brief := l.brief
	seen := len(l.inject)
	if seen > 0 {
		lines := make([]string, 0, seen)
		for _, in := range l.inject {
			lines = append(lines, in.bot+" wants to tell the owner: "+chat.Fence("call reason", in.reason))
		}
		brief += "\n\nNews since the call started:\n- " + strings.Join(lines, "\n- ")
	}
	history := append([]chat.VoiceLine(nil), l.history...)
	lang := l.lang
	c.mu.Unlock()
	reply, err := c.host.VoiceTurn(tctx, chat.VoiceRequest{BotID: bot.ID, CallID: f.CallID, Text: utter, Brief: brief, History: history,
		Language: lang, OnText: sp.say, OnTool: sp.filler})
	if tctx.Err() != nil {
		// Superseded or aborted: no more speech. Keep a note of tools that already ran so the next turn knows.
		if ran := sp.toolsRan(); len(ran) > 0 {
			c.mu.Lock()
			l.history = append(l.history, chat.VoiceLine{Role: "assistant",
				Text: "(I was interrupted. These tools already ran: " + strings.Join(ran, ", ") + ".)"})
			c.mu.Unlock()
		}
		return
	}
	if err != nil {
		sp.say(sp.lines.failed + firstSentence(err.Error()))
	}
	c.mu.Lock()
	if err == nil {
		l.reasonGiven = true
	}
	l.delivered = max(l.delivered, seen)
	l.history = append(l.history, chat.VoiceLine{Role: "user", Text: utter})
	if reply != "" {
		l.history = append(l.history, chat.VoiceLine{Role: "assistant", Text: reply})
	}
	if len(l.history) > maxHistory {
		l.history = l.history[len(l.history)-maxHistory:]
	}
	hang := l.hangAfter && !l.ended
	c.mu.Unlock()
	if hang && strings.TrimSpace(reply) == "" {
		sp.say(sp.lines.bye)
	}
	sp.finish()
	if hang {
		// end_call: hang up once the goodbye has been spoken.
		c.hangupAfter(sid, sp.spoken())
	}
}

// greetingWords only answer the phone; an utterance made of nothing else is a bare greeting.
var greetingWords = map[string]bool{
	"hello": true, "hallo": true, "hi": true, "hey": true, "hiya": true, "hoi": true, "yes": true, "yeah": true, "yep": true,
	"ja": true, "oui": true, "allo": true, "allô": true, "bonjour": true, "salut": true, "speaking": true, "morning": true,
	"goedemorgen": true, "goedemiddag": true, "goedenavond": true, "goeiemorgen": true, "goeiemiddag": true, "goeienavond": true,
	"hallootjes": true, "moin": true, "dag": true, "hé": true, "he": true,
}

// requestWords make an utterance a question or a request, never a bare greeting.
var requestWords = map[string]bool{
	"what": true, "who": true, "why": true, "how": true, "when": true, "where": true, "which": true, "can": true, "could": true,
	"check": true, "start": true, "tell": true,
	// Dutch
	"wat": true, "wie": true, "hoe": true, "waarom": true, "wanneer": true, "waar": true, "welke": true, "welk": true,
	"kun": true, "kunt": true, "kan": true, "zou": true, "vertel": true, "zeg": true, "stop": true, "starten": true,
}

// requestPhrases are requests that are not a single request word ("ga je gang": go ahead).
var requestPhrases = []string{"ga je gang", "ga uw gang", "go ahead"}

// asksSomething reports whether an utterance contains a question word, a request verb or a request phrase.
func asksSomething(utter string) bool {
	words := strings.FieldsFunc(strings.ToLower(utter), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for _, w := range words {
		if requestWords[w] {
			return true
		}
	}
	joined := " " + strings.Join(words, " ") + " "
	for _, p := range requestPhrases {
		if strings.Contains(joined, " "+p+" ") {
			return true
		}
	}
	return false
}

// bareGreeting reports whether an utterance only answers the phone: every word is a greeting or filler word, apart
// from a name in "met <name>", "this is <name>" or "<name> speaking", and no word asks or requests anything.
func bareGreeting(utter string) bool {
	words := strings.FieldsFunc(strings.ToLower(utter), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 || len(words) > 6 {
		return false
	}
	if asksSomething(utter) {
		return false
	}
	// name reports whether words[i:] is a name of one or two words that ends the utterance.
	name := func(i int) bool { n := len(words) - i; return n == 1 || n == 2 }
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case greetingWords[w]:
		case w == "good" && i+1 < len(words) && (words[i+1] == "morning" || words[i+1] == "afternoon" || words[i+1] == "evening"):
			i++
		case w == "met" && name(i+1):
			return true
		case w == "this" && i+1 < len(words) && words[i+1] == "is" && name(i+2):
			return true
		case len(words)-i == 2 && words[i+1] == "speaking":
			return true // "<name> speaking"
		default:
			return false
		}
	}
	return true
}

// voicemailPhrases are answering-machine prompts (English, Dutch, French, German), lower case.
var voicemailPhrases = []string{
	"leave a message", "after the tone", "after the beep", "voicemail", "voice mail", "mailbox", "is not available",
	"can't take your call", "cannot take your call", "not available right now", "can't come to the phone",
	"you've reached", "you have reached",
	"spreek een bericht in", "na de piep", "niet bereikbaar", "u bent verbonden met",
	"laissez un message", "après le bip", "apres le bip", "messagerie",
	"vous êtes bien sur", "vous etes bien sur",
	"nach dem signalton", "hinterlassen sie", "ist nicht erreichbar",
}

// IsVoicemail reports whether an utterance heard before anyone answered sounds like an answering machine.
func IsVoicemail(utter string) bool {
	u := strings.ToLower(strings.ReplaceAll(utter, "’", "'"))
	for _, p := range voicemailPhrases {
		if strings.Contains(u, p) {
			return true
		}
	}
	// "Sie haben <name> erreicht"
	if i := strings.Index(u, "haben "); i >= 0 && strings.Contains(u[i:], " erreicht") {
		return true
	}
	return false
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
	call := CallRecord{ID: uuid.NewString(), BotID: bot.ID, Direction: Inbound, Reason: "You called", Status: StatusInProgress,
		StartedAt: c.now().UnixMilli(), AnsweredAt: c.now().UnixMilli(), ProviderCallID: f.CallSID, ConversationID: f.ConversationID}
	lang := s.spokenLanguage()
	l := &live{call: call, botName: bot.Name, lang: lang, opener: make(chan string, 1), done: make(chan struct{})}
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
	c.changed()
	if id, err := c.host.PostCallCard(ctx, bot.ID, c.card(call)); err == nil {
		c.mu.Lock()
		l.call.MessageID = id
		c.mu.Unlock()
		c.persist(ctx, l, false)
	}
	octx := context.WithoutCancel(ctx)
	c.spawn(func() {
		cctx, cancel := context.WithTimeout(octx, 3*time.Second)
		defer cancel()
		text, err := c.host.Compose(cctx, bot.ID, inboundInstruction(lang, bot.Name))
		if t, ok := CheckOpener(bot.Name, text); err == nil && ok {
			l.opener <- t
			return
		}
		l.opener <- phrasesFor(lang).inboundLine(bot.Name)
	})
	tts := map[string]string{}
	if v := or2(bot.Config.Jarvis.VoiceID, s.DefaultVoiceID); v != "" {
		tts["voice_id"] = v
	}
	if m := ttsModel(lang, or2(bot.Config.Jarvis.TTSModel, s.DefaultTTSModel)); m != "" {
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

// monitor ends the call even if the post-call webhook never arrives: every pollEvery it asks Twilio for the call's
// status by its SID (both directions), then ElevenLabs for the conversation's, and it enforces a hard ceiling.
// finish is idempotent, so whichever path ends the call first wins.
func (c *Calls) monitor(l *live) {
	ceiling := c.after(maxCallDuration)
	for {
		select {
		case <-c.baseCtx.Done():
			return
		case <-l.done:
			return
		case <-ceiling:
			c.mu.Lock()
			sid := l.call.ProviderCallID
			c.mu.Unlock()
			if c.finish(c.baseCtx, l, StatusCompleted, 0) {
				c.hangup(c.baseCtx, sid)
			}
			return
		case <-c.after(pollEvery):
			c.poll(l)
		}
	}
}

// poll checks the carrier, then the voice provider, and ends the call if either reports it over.
func (c *Calls) poll(l *live) {
	c.mu.Lock()
	sid, conv := l.call.ProviderCallID, l.call.ConversationID
	answered := l.call.AnsweredAt != 0
	c.mu.Unlock()
	if sid != "" && c.phone != nil {
		pctx, cancel := context.WithTimeout(c.baseCtx, 10*time.Second)
		st, err := c.phone.CallStatus(pctx, sid)
		cancel()
		if err == nil {
			if end, ok := carrierEnd(st, answered); ok {
				c.finish(c.baseCtx, l, end, 0)
				return
			}
		}
	}
	if conv == "" {
		return
	}
	pctx, cancel := context.WithTimeout(c.baseCtx, 10*time.Second)
	st, secs, err := c.dial.ConversationStatus(pctx, conv)
	cancel()
	if err != nil {
		return
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

// carrierEnd maps a terminal Twilio call status to ours; ok is false while the call is still live.
func carrierEnd(status string, answered bool) (string, bool) {
	switch strings.ToLower(status) {
	case "completed", "busy", "no-answer", "canceled":
		if answered {
			return StatusCompleted, true
		}
		return StatusNoAnswer, true
	case "failed":
		return StatusFailed, true
	}
	return "", false
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
	brief := strings.TrimSuffix(l.brief, "\n\n"+openedNote)
	var missed []string
	if l.delivered < len(l.inject) {
		for _, in := range l.inject[l.delivered:] {
			missed = append(missed, in.bot+": "+in.reason)
		}
	}
	if c.cur == l {
		c.cur = nil
	}
	c.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	c.persist(ctx, l, true)
	c.host.Audit("jarvis.call.ended", call.ID, "allowed", status)
	c.changed()
	if len(missed) > 0 {
		// News folded into this call never reached a finished turn: tell the owner in writing instead.
		c.host.Notify("News you missed on the call", strings.Join(missed, "\n"), "bot:"+call.BotID, "info")
	}
	if (status == StatusNoAnswer || status == StatusVoicemail) && l.verify == "" && call.Direction == Outbound {
		c.host.Notify(name+" tried to call", call.Reason, "bot:"+call.BotID, "warning")
		if l.urgent {
			quiet := l.quiet
			c.spawn(func() { c.retry(call, brief, quiet) })
		}
	}
	c.spawn(func() { c.checkOutcome(l, call, name) })
	return true
}

// checkOutcome runs once per ended call, outcomeAfter later (ElevenLabs needs a moment to record the conversation), and
// asks ElevenLabs how it ended: the status, termination reason and error type and reason. It exists because the voice
// service can drop a call on its own (for example when the custom LLM is too slow) and nothing on our side shows it.
// A conversation that is not finished yet (any status but done or failed) is read once more outcomeRetryAfter later.
// The outcome is logged (provider metadata only, never spoken text) and a call that ElevenLabs marks failed is reported
// to the owner. The conversation id is looked up by the carrier call SID when the record has none, and then stored.
// Failures of the check itself are only logged.
func (c *Calls) checkOutcome(l *live, call CallRecord, botName string) {
	if call.ConversationID == "" && call.ProviderCallID == "" {
		return // the call never reached the provider
	}
	if !c.pause(c.outcomeAfter) {
		return
	}
	// The ids are read again now, not taken from finish: the record may have gained them in the meantime.
	c.mu.Lock()
	conv, sid := l.call.ConversationID, l.call.ProviderCallID
	near := time.UnixMilli(l.call.StartedAt)
	if l.call.AnsweredAt > 0 {
		near = time.UnixMilli(l.call.AnsweredAt)
	}
	c.mu.Unlock()
	if conv == "" {
		found, err := c.findConversation(l, sid, near)
		switch {
		case err != nil:
			c.log.Info("jarvis: call outcome not checked", "call", call.ID, "reason", "cannot look up the conversation", "err", err)
			return
		case found == "":
			c.log.Info("jarvis: call outcome not checked", "call", call.ID, "reason", "conversation not found")
			return
		}
		conv = found
	}
	out, ok := c.readOutcome(call.ID, conv)
	if !ok {
		return
	}
	if !outcomeFinal(out.Status) {
		c.log.Info("jarvis: call outcome pending", "call", call.ID, "status", out.Status)
		if !c.pause(c.outcomeRetryAfter) {
			return
		}
		if out, ok = c.readOutcome(call.ID, conv); !ok {
			return
		}
	}
	c.log.Info("jarvis: call outcome", "call", call.ID, "status", out.Status, "termination_reason", out.TerminationReason,
		"error_type", out.ErrorType, "error_reason", out.ErrorReason)
	if !strings.EqualFold(out.Status, "failed") || l.verify != "" {
		return
	}
	if call.Status == StatusNoAnswer || call.Status == StatusVoicemail {
		return // the owner was already told the call went unanswered
	}
	reason := or2(out.ErrorReason, out.TerminationReason)
	if reason == "" {
		reason = "no reason was given"
	}
	c.host.Notify("Call with "+botName+" dropped", "The voice service ended the call: "+clipRunes(reason, maxTitle), "bot:"+call.BotID, "warning")
}

// pause waits d on the injectable clock and reports false when the manager is closing first.
func (c *Calls) pause(d time.Duration) bool {
	select {
	case <-c.after(d):
		return true
	case <-c.baseCtx.Done():
		return false
	}
}

// outcomeFinal reports whether ElevenLabs is done with a conversation (done or failed); the other statuses (initiated,
// in-progress, processing) mean its record is still changing.
func outcomeFinal(status string) bool {
	return strings.EqualFold(status, "done") || strings.EqualFold(status, "failed")
}

// readOutcome reads the conversation's outcome within outcomeTimeout; a failure is logged and reported as !ok.
func (c *Calls) readOutcome(callID, conv string) (Outcome, bool) {
	ctx, cancel := context.WithTimeout(c.baseCtx, c.outcomeTimeout)
	defer cancel()
	out, err := c.dial.ConversationOutcome(ctx, conv)
	if err != nil {
		c.log.Info("jarvis: call outcome not checked", "call", callID, "reason", "cannot read the conversation", "err", err)
		return Outcome{}, false
	}
	return out, true
}

// findConversation looks a call's conversation up by its carrier call SID and stores the id on the call's record. It
// returns "" without an error when there is none.
func (c *Calls) findConversation(l *live, sid string, near time.Time) (string, error) {
	ctx, cancel := context.WithTimeout(c.baseCtx, c.outcomeTimeout)
	defer cancel()
	found, err := c.dial.FindConversation(ctx, c.settings().AgentID, sid, near)
	if err != nil || found == "" {
		return "", err
	}
	c.mu.Lock()
	if l.call.ConversationID == "" {
		l.call.ConversationID = found
	}
	c.mu.Unlock()
	c.persist(ctx, l, false)
	return found, nil
}

// retry places one more attempt after an unanswered urgent call; the retry itself is not urgent, so it never repeats.
func (c *Calls) retry(call CallRecord, brief string, quiet bool) {
	select {
	case <-c.after(urgentRetryWait):
	case <-c.baseCtx.Done():
		return
	}
	c.Retry(c.baseCtx, PlaceRequest{BotID: call.BotID, Reason: call.Reason, Brief: brief, RuleID: call.RuleID, QuietOverride: quiet})
}

// persist writes the call's row (and its card, if asked) as the call stands now. Writers for one call are serialized
// and each one reads the state only once it holds the call's write lock, so a write that started before the call
// ended can never land after finish's and reopen it: whichever write comes last carries the final state.
func (c *Calls) persist(ctx context.Context, l *live, card bool) {
	l.persistMu.Lock()
	defer l.persistMu.Unlock()
	c.mu.Lock()
	call := l.call
	c.mu.Unlock()
	_ = c.store.UpdateCall(ctx, call)
	if card && call.MessageID != "" {
		_ = c.host.UpdateCallCard(ctx, call.MessageID, c.card(call))
	}
}

func (c *Calls) changed() {
	if c.Changed != nil {
		c.Changed()
	}
}

func (c *Calls) clear(l *live) {
	c.mu.Lock()
	if c.cur == l {
		c.cur = nil
	}
	c.mu.Unlock()
}

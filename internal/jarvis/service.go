package jarvis

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
)

var e164 = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

const (
	tickEvery         = 20 * time.Second
	missedGrace       = 2 * time.Minute // an at_time call later than this becomes a notification
	verifyTTL         = 10 * time.Minute
	maxVerifyAttempts = 5
	maxEventDetail    = 2000
)

// ServiceConfig wires a Service. DB, Creds and Host are required.
type ServiceConfig struct {
	DB    *sql.DB
	Creds credentials.Store
	Host  Host
	Log   *slog.Logger
	Now   func() time.Time
	HTTP  *http.Client
	// ElevenLabsURL and TwilioURL override the provider API base URLs ("" = production); tests point them at fakes.
	ElevenLabsURL, TwilioURL string
	LoadSettings             func() (PhoneSettings, error)
	SaveSettings             func(PhoneSettings) error
	// LoadState and SaveState keep daemon-owned runtime state (the last at_time tick) across restarts.
	LoadState func() (State, error)
	SaveState func(State) error
	// Changed, if set, receives the status when the relay connects or drops and when a call starts or ends.
	// Calls are coalesced and made from one goroutine after Start.
	Changed func(StatusView)

	dialer Dialer // test seam; nil = ElevenLabs with the stored key
	sender Sender // test seam; nil = the relay WebSocket
	phone  Phone  // test seam; nil = Twilio with the stored credentials
}

// State is runtime state the daemon persists for the service; it is not user-editable.
type State struct {
	LastTickMs int64 `json:"lastTickMs"` // when at_time rules were last checked
}

// StatusView is what the settings screen shows. It never carries a secret, only whether one is stored.
type StatusView struct {
	Settings            PhoneSettings `json:"settings"`
	HasTwilio           bool          `json:"hasTwilio"`
	HasElevenLabs       bool          `json:"hasElevenLabs"`
	HasRelaySecrets     bool          `json:"hasRelaySecrets"`  // relay token and LLM secret
	HasWebhookSecret    bool          `json:"hasWebhookSecret"` // post-call HMAC secret from ElevenLabs
	RelayConnected      bool          `json:"relayConnected"`
	VerificationPending bool          `json:"verificationPending"`
	CallsToday          int           `json:"callsToday"`
	MonthCostMicros     int64         `json:"monthCostMicros"`
	LastError           string        `json:"lastError,omitempty"`
}

// SecretsInput sets provider credentials; empty fields are left unchanged.
type SecretsInput struct {
	TwilioSID   string `json:"twilioSid,omitempty"`
	TwilioToken string `json:"twilioToken,omitempty"`
	ElevenLabs  string `json:"elevenLabsKey,omitempty"`
}

// RelaySecrets are shown to the owner once so they can `wrangler secret put` them.
type RelaySecrets struct {
	RelayToken string `json:"relayToken"`
	LLMSecret  string `json:"llmSecret"`
}

// ConfigureResult is Configure's outcome. WebhookSecret is set only on the run that created the ElevenLabs
// post-call webhook: it is shown once so the owner can `wrangler secret put WEBHOOK_SECRET`.
type ConfigureResult struct {
	Status        StatusView `json:"status"`
	WebhookSecret string     `json:"webhookSecret,omitempty"`
}

// Service ties rules, the call policy, the call manager and the relay connection together.
type Service struct {
	cfg    ServiceConfig
	store  *Store
	calls  *Calls
	sender Sender
	holder *relayHolder // nil when a test sender is injected
	log    *slog.Logger
	now    func() time.Time

	cfgMu sync.Mutex // serializes Configure

	mu          sync.Mutex
	settings    PhoneSettings
	lastErr     string
	pendingCode string
	pendingNum  string
	pendingAt   time.Time
	attempts    int
	dialTo      string // set only while a verification call is being placed
	lastTick    time.Time
	startedAt   time.Time
	relayUpOnce bool          // the relay has connected at least once since Start
	changedCh   chan struct{} // coalesced Changed notifications
	baseCtx     context.Context
	cancel      context.CancelFunc
	started     bool
	closed      bool
	wg          sync.WaitGroup // event, tick and frame goroutines

	relayMu     sync.Mutex
	relayKey    string
	relayCancel context.CancelFunc
	relayDone   chan struct{}
}

func NewService(cfg ServiceConfig) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	s := &Service{cfg: cfg, store: NewStore(cfg.DB, cfg.Now), log: cfg.Log, now: cfg.Now, baseCtx: context.Background(),
		changedCh: make(chan struct{}, 1)}
	if cfg.LoadSettings != nil {
		v, err := cfg.LoadSettings()
		if err != nil {
			s.log.Warn("jarvis: cannot read settings", "err", err)
			s.lastErr = "The Jarvis settings could not be read."
		}
		s.settings = v
	}
	dial := cfg.dialer
	if dial == nil {
		dial = elDialer{s}
	}
	s.sender = cfg.sender
	if s.sender == nil {
		s.holder = &relayHolder{}
		s.sender = s.holder
	}
	phone := cfg.phone
	if phone == nil {
		phone = twilioPhone{s}
	}
	s.calls = NewCalls(s.store, cfg.Host, dial, phone, s.sender, s.callSettings, cfg.Now, s.log)
	s.calls.Retry = func(ctx context.Context, req PlaceRequest) { s.fire(ctx, req, Ask{QuietOverride: req.QuietOverride}) }
	s.calls.Changed = s.notifyChanged
	return s
}

// notifyChanged asks for a Changed callback without blocking; bursts collapse into one.
func (s *Service) notifyChanged() {
	select {
	case s.changedCh <- struct{}{}:
	default:
	}
}

// relayHolder points at the current RelayClient. A client is never mutated: a config change replaces it.
type relayHolder struct {
	mu  sync.Mutex
	cur *RelayClient
}

func (h *relayHolder) get() *RelayClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur
}

func (h *relayHolder) set(c *RelayClient) {
	h.mu.Lock()
	h.cur = c
	h.mu.Unlock()
}

func (h *relayHolder) Send(ctx context.Context, f Frame) error {
	c := h.get()
	if c == nil {
		return ErrRelayDown
	}
	return c.Send(ctx, f)
}

func (h *relayHolder) Connected() bool {
	c := h.get()
	return c != nil && c.Connected()
}

// elDialer reads the ElevenLabs key from Credential Manager on every use, so a key change applies at once.
type elDialer struct{ s *Service }

func (d elDialer) OutboundCall(ctx context.Context, c OutboundCall) (string, string, error) {
	el, err := d.s.elevenLabs()
	if err != nil {
		return "", "", err
	}
	return el.OutboundCall(ctx, c)
}

func (d elDialer) ConversationStatus(ctx context.Context, id string) (string, int, error) {
	el, err := d.s.elevenLabs()
	if err != nil {
		return "", 0, err
	}
	return el.ConversationStatus(ctx, id)
}

// twilioPhone reads the Twilio credentials from Credential Manager on every use.
type twilioPhone struct{ s *Service }

func (p twilioPhone) client() (*Twilio, error) {
	sid, err1 := p.s.cfg.Creds.Get(KeyTwilioSID)
	tok, err2 := p.s.cfg.Creds.Get(KeyTwilioToken)
	if err1 != nil || err2 != nil || len(sid) == 0 || len(tok) == 0 {
		return nil, &Refusal{"The Twilio credentials are not set."}
	}
	return &Twilio{SID: string(sid), Token: string(tok), BaseURL: p.s.cfg.TwilioURL, HTTP: p.s.cfg.HTTP}, nil
}

func (p twilioPhone) Hangup(ctx context.Context, callSID string) error {
	tw, err := p.client()
	if err != nil {
		return err
	}
	return tw.Hangup(ctx, callSID)
}

func (p twilioPhone) CallStatus(ctx context.Context, callSID string) (string, error) {
	tw, err := p.client()
	if err != nil {
		return "", err
	}
	return tw.CallStatus(ctx, callSID)
}

func (s *Service) elevenLabs() (*ElevenLabs, error) {
	key, err := s.cfg.Creds.Get(KeyElevenLabs)
	if err != nil || len(key) == 0 {
		return nil, &Refusal{"The ElevenLabs API key is not set."}
	}
	el := &ElevenLabs{Key: string(key), BaseURL: s.cfg.ElevenLabsURL, HTTP: s.cfg.HTTP}
	if llm, err := s.cfg.Creds.Get(KeyLLMSecret); err == nil && len(llm) > 0 {
		el.Scrub = []string{string(llm)}
	}
	return el, nil
}

// Settings returns the stored settings.
func (s *Service) Settings() PhoneSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// callSettings is what the call manager sees: while a verification call is placed it dials the number being
// verified, and inbound calls are refused because that number is not yet the owner's.
func (s *Service) callSettings() PhoneSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.settings
	if s.dialTo != "" {
		set.OwnerNumber, set.OwnerVerified = s.dialTo, false
	}
	return set
}

// Start connects the relay and runs the at_time ticker until Close.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started || s.closed {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.baseCtx, s.cancel = context.WithCancel(ctx)
	// Resume from the last persisted tick, so a time that fell due while the daemon was down is reported.
	now := s.now()
	s.lastTick = now
	s.startedAt = now
	if s.cfg.LoadState != nil {
		if st, err := s.cfg.LoadState(); err != nil {
			s.log.Warn("jarvis: cannot read state", "err", err)
		} else if st.LastTickMs > 0 && st.LastTickMs < now.UnixMilli() {
			s.lastTick = time.UnixMilli(st.LastTickMs).In(now.Location())
		}
	}
	base := s.baseCtx
	s.wg.Add(2)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-base.Done():
				return
			case <-s.changedCh:
				if s.cfg.Changed != nil {
					s.cfg.Changed(s.Status(base))
				}
			}
		}
	}()
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		s.tick(base)
		for {
			select {
			case <-base.Done():
				return
			case <-t.C:
				s.tick(base)
			}
		}
	}()
	s.restartRelay()
}

// Close stops the relay, the ticker, event handling and the call manager's watchers, and waits for them.
// A live call is left to the provider. Safe to call more than once.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.stopRelay()
	s.calls.Close()
	s.wg.Wait()
}

// track registers a goroutine unless Close has started; the caller must call s.wg.Done when it returns true.
func (s *Service) track() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.wg.Add(1)
	return true
}

func (s *Service) ctx() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.baseCtx
}

// wait blocks until in-flight event and tick work is done (tests).
func (s *Service) wait() { s.wg.Wait() }

// handleFrame runs one relay frame (RelayClient calls it on its own goroutine). A panic is logged without
// any frame content and never takes the daemon down.
func (s *Service) handleFrame(f Frame) {
	if !s.track() {
		return
	}
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("jarvis: frame handler panicked", "frame", f.Type, "panic", fmt.Sprintf("%T", r), "stack", string(debug.Stack()))
		}
	}()
	s.calls.HandleFrame(s.ctx(), f)
}

// normalizeRelayURL accepts https://host[/path], or http:// for localhost only (relay dev server).
func normalizeRelayURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	bad := &Refusal{"The relay URL must look like https://your-relay.workers.dev."}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", bad
	}
	switch u.Scheme {
	case "https":
	case "http":
		if h := u.Hostname(); h != "localhost" && h != "127.0.0.1" && h != "::1" {
			return "", bad
		}
	default:
		return "", bad
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/"), nil
}

func relaySocketURL(relayURL string) string {
	if rest, ok := strings.CutPrefix(relayURL, "https://"); ok {
		return "wss://" + rest + "/daemon"
	}
	return "ws://" + strings.TrimPrefix(relayURL, "http://") + "/daemon"
}

// restartRelay makes the relay connection match the settings and stored token. The old client's Run is canceled
// and awaited before a fresh client starts; nothing happens when the URL and token are unchanged.
func (s *Service) restartRelay() {
	if s.holder == nil {
		return
	}
	s.relayMu.Lock()
	defer s.relayMu.Unlock()
	s.mu.Lock()
	started, closed, base := s.started, s.closed, s.baseCtx
	relayURL := s.settings.RelayURL
	s.mu.Unlock()
	if !started || closed {
		return
	}
	tok, err := s.cfg.Creds.Get(KeyRelayToken)
	key := ""
	if relayURL != "" && err == nil && len(tok) > 0 {
		key = relayURL + "\x00" + string(tok)
	}
	if key == s.relayKey && (key == "" || s.relayCancel != nil) {
		return
	}
	s.stopRelayLocked()
	s.relayKey = key
	if key == "" {
		return
	}
	rc := NewRelayClient(relaySocketURL(relayURL), string(tok), s.handleFrame, s.log)
	rc.OnState = func(up bool) {
		if up {
			s.relayUp()
		}
		s.notifyChanged()
	}
	s.holder.set(rc)
	ctx, cancel := context.WithCancel(base)
	done := make(chan struct{})
	s.relayCancel, s.relayDone = cancel, done
	go func() {
		defer close(done)
		rc.Run(ctx)
	}()
}

func (s *Service) stopRelay() {
	if s.holder == nil {
		return
	}
	s.relayMu.Lock()
	defer s.relayMu.Unlock()
	s.stopRelayLocked()
	s.relayKey = ""
}

func (s *Service) stopRelayLocked() {
	if s.relayCancel != nil {
		s.relayCancel()
		<-s.relayDone
		s.relayCancel, s.relayDone = nil, nil
	}
	s.holder.set(nil)
}

func (s *Service) usage(ctx context.Context, botID string) Usage {
	now := s.now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).UnixMilli()
	today, _, _ := s.store.CallsSince(ctx, day)
	_, cost, _ := s.store.CallsSince(ctx, month)
	var last int64
	if botID != "" {
		last, _ = s.store.LastCallAt(ctx, botID)
	}
	_, in := s.calls.Active()
	return Usage{Today: today, MonthCostMicros: cost, LastBotCallMs: last, InCall: in}
}

// place is the policy-checked way to place a call: rules, the ticker, call_owner, test calls and urgent retries
// all go through it. During a call the request is folded into it; otherwise caps, quiet hours and the cool-down apply.
func (s *Service) place(ctx context.Context, req PlaceRequest, ask Ask) (CallRecord, error) {
	if _, in := s.calls.Active(); in {
		_, err := s.calls.Place(ctx, req) // ErrInjected, or a refusal
		return CallRecord{}, err
	}
	if err := Allow(s.Settings(), s.now(), s.usage(ctx, req.BotID), ask); err != nil {
		return CallRecord{}, err
	}
	return s.calls.Place(ctx, req)
}

// fire is place for calls nobody is waiting on (rules, the ticker, retries): a refusal becomes a notification
// so the owner still hears about it.
func (s *Service) fire(ctx context.Context, req PlaceRequest, ask Ask) {
	_, err := s.place(ctx, req, ask)
	var r *Refusal
	var g *GateError
	if errors.As(err, &r) || errors.As(err, &g) {
		s.cfg.Host.Notify(s.botName(ctx, req.BotID)+" did not call", req.Reason+" ("+err.Error()+")", "bot:"+req.BotID, "info")
	}
}

func (s *Service) botName(ctx context.Context, id string) string {
	if b, err := s.cfg.Host.Bot(ctx, id); err == nil && b.Name != "" {
		return b.Name
	}
	return "A bot"
}

// OnEvent matches rules against a task, automation or notification event. It never blocks the caller.
func (s *Service) OnEvent(ev Event) {
	if !s.track() {
		return
	}
	ctx := s.ctx()
	go func() {
		defer s.wg.Done()
		rules, err := s.store.Rules(ctx, "")
		if err != nil {
			s.log.Warn("jarvis: cannot read rules", "err", err)
			return
		}
		nowMs := s.now().UnixMilli()
		for _, r := range rules {
			if Expired(r, nowMs) {
				_ = s.store.DeleteRule(ctx, r.ID)
				continue
			}
			if !Matches(r, ev, nowMs) {
				continue
			}
			if !r.Repeat && errors.Is(s.store.DeleteRule(ctx, r.ID), ErrNotFound) {
				continue // another event already used this once-rule
			}
			title := clipRunes(ev.Title, maxTitle)
			brief := "Why you are calling: " + chat.Fence("event title", title)
			if r.Note != "" {
				brief += "\nThe owner's note for this rule: " + chat.Fence("rule note", r.Note)
			}
			if d := clipRunes(ev.Detail, maxEventDetail); d != "" {
				brief += "\nDetails:\n" + chat.Fence("event detail", d)
			}
			s.fire(ctx, PlaceRequest{BotID: r.BotID, Reason: title, Brief: brief, RuleID: r.ID, Urgent: r.Urgent, QuietOverride: r.QuietOverride},
				Ask{QuietOverride: r.QuietOverride})
		}
	}()
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// relayUp runs when the relay connects. The first time after Start it re-runs the at_time tick, so a time that fell
// due while the relay was still connecting is called now (or, past missedGrace, reported) instead of refused.
func (s *Service) relayUp() {
	s.mu.Lock()
	first := !s.relayUpOnce
	s.relayUpOnce = true
	ctx := s.baseCtx
	s.mu.Unlock()
	if !first || !s.track() {
		return
	}
	go func() {
		defer s.wg.Done()
		s.tick(ctx)
	}()
}

// holdTick reports whether the at_time tick should wait for the relay: it is configured but has not connected
// since Start, and Start was less than missedGrace ago.
func (s *Service) holdTick(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings.RelayURL != "" && !s.relayUpOnce && !s.startedAt.IsZero() && now.Sub(s.startedAt) < missedGrace &&
		!s.sender.Connected()
}

// tick fires at_time rules whose time passed since the last tick. Times missed by more than missedGrace
// (the PC was asleep) become a notification instead of a late call. Right after Start, while the relay is still
// connecting, the window is kept open until it connects (see relayUp).
func (s *Service) tick(ctx context.Context) {
	now := s.now()
	if s.holdTick(now) {
		return
	}
	s.mu.Lock()
	prev := s.lastTick
	s.lastTick = now
	s.mu.Unlock()
	if s.cfg.SaveState != nil {
		if err := s.cfg.SaveState(State{LastTickMs: now.UnixMilli()}); err != nil {
			s.log.Warn("jarvis: cannot save state", "err", err)
		}
	}
	rules, err := s.store.Rules(ctx, "")
	if err != nil {
		return
	}
	for _, r := range rules {
		if r.Trigger != AtTime {
			continue
		}
		if Expired(r, now.UnixMilli()) {
			_ = s.store.DeleteRule(ctx, r.ID)
			continue
		}
		// A rule only fires for times after it was created ("12:00" saved at 12:00:10 means tomorrow).
		from := prev
		if created := time.UnixMilli(r.CreatedAt).In(now.Location()); created.After(from) {
			from = created
		}
		at := NextAt(r, from)
		if at.IsZero() || at.After(now) {
			continue
		}
		if !r.Repeat {
			_ = s.store.DeleteRule(ctx, r.ID)
		}
		reason := "It's " + r.Target
		if r.Note != "" {
			reason = r.Note
		}
		if now.Sub(at) > missedGrace {
			s.cfg.Host.Notify(s.botName(ctx, r.BotID)+" missed a scheduled call", reason, "bot:"+r.BotID, "info")
			continue
		}
		if !s.track() {
			return
		}
		go func() {
			defer s.wg.Done()
			brief := "Scheduled call at " + r.Target + "."
			if r.Note != "" {
				brief += "\nThe owner's note for this rule: " + chat.Fence("rule note", r.Note)
			}
			s.fire(ctx, PlaceRequest{BotID: r.BotID, Reason: reason, Brief: brief, RuleID: r.ID, Urgent: r.Urgent, QuietOverride: r.QuietOverride},
				Ask{QuietOverride: r.QuietOverride})
		}()
	}
}

// CallOwner is the call_owner tool: a bot's own judgement, so the cool-down applies.
func (s *Service) CallOwner(ctx context.Context, botID, reason string, urgent bool) (string, error) {
	reason = clipRunes(reason, maxTitle)
	_, err := s.place(ctx, PlaceRequest{BotID: botID, Reason: reason, Brief: "Why you are calling: " + chat.Fence("call reason", reason), Urgent: urgent},
		Ask{Judgement: true})
	if errors.Is(err, ErrInjected) {
		return "You are already on a call; the owner will hear this there.", nil
	}
	if err != nil {
		return "", err
	}
	return "Calling the owner now.", nil
}

// TestCall places a call the owner asked for from settings: caps apply, quiet hours and the cool-down do not.
func (s *Service) TestCall(ctx context.Context, botID string) (string, error) {
	_, err := s.place(ctx, PlaceRequest{BotID: botID, Reason: "a test call you started from settings",
		Brief: "This is a test call the owner started from settings. Introduce yourself and ask if they can hear you."},
		Ask{QuietOverride: true})
	if errors.Is(err, ErrInjected) {
		return "", &Refusal{"A call is already in progress."}
	}
	if err != nil {
		return "", err
	}
	return "Calling you now.", nil
}

func (s *Service) AddRule(ctx context.Context, r JarvisRule) (JarvisRule, error) {
	if err := validateRule(r); err != nil {
		return JarvisRule{}, &Refusal{err.Error()}
	}
	return s.store.AddRule(ctx, r)
}

func (s *Service) Rules(ctx context.Context, botID string) ([]JarvisRule, error) {
	return s.store.Rules(ctx, botID)
}

func (s *Service) DeleteRule(ctx context.Context, id string) error {
	err := s.store.DeleteRule(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return &Refusal{"There is no rule with that id."}
	}
	return err
}

// Call returns one call record (ErrNotFound when there is none).
func (s *Service) Call(ctx context.Context, id string) (CallRecord, error) {
	return s.store.GetCall(ctx, id)
}

func (s *Service) SwitchBot(ctx context.Context, callID, name string) (string, error) {
	return s.calls.SwitchBot(ctx, callID, name)
}

func (s *Service) has(key string) bool {
	v, err := s.cfg.Creds.Get(key)
	return err == nil && len(v) > 0
}

func (s *Service) Status(ctx context.Context) StatusView {
	u := s.usage(ctx, "")
	s.mu.Lock()
	set, last := s.settings, s.lastErr
	pending := s.pendingCode != "" && s.now().Sub(s.pendingAt) < verifyTTL
	s.mu.Unlock()
	return StatusView{Settings: set, HasTwilio: s.has(KeyTwilioSID) && s.has(KeyTwilioToken), HasElevenLabs: s.has(KeyElevenLabs),
		HasRelaySecrets: s.has(KeyRelayToken) && s.has(KeyLLMSecret), HasWebhookSecret: s.has(KeyWebhookSecret),
		RelayConnected: s.sender.Connected(), VerificationPending: pending, CallsToday: u.Today, MonthCostMicros: u.MonthCostMicros, LastError: last}
}

func (s *Service) SetSecrets(in SecretsInput) error {
	for _, p := range []struct{ key, val string }{{KeyTwilioSID, in.TwilioSID}, {KeyTwilioToken, in.TwilioToken}, {KeyElevenLabs, in.ElevenLabs}} {
		v := strings.TrimSpace(p.val)
		if v == "" {
			continue
		}
		if len(v) > 512 {
			return &Refusal{"That credential is too long."}
		}
		if err := s.cfg.Creds.Set(p.key, []byte(v)); err != nil {
			return err
		}
	}
	return nil
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateRelaySecrets creates and stores a new relay token and LLM secret and returns them once. The webhook
// secret is not generated here: ElevenLabs creates it during Configure. Rotating means redeploying the relay
// secrets and running Configure again so the agent sends the new LLM secret.
func (s *Service) GenerateRelaySecrets() (RelaySecrets, error) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	var out RelaySecrets
	var err error
	if out.RelayToken, err = randomSecret(); err != nil {
		return RelaySecrets{}, err
	}
	if out.LLMSecret, err = randomSecret(); err != nil {
		return RelaySecrets{}, err
	}
	if err := s.cfg.Creds.Set(KeyRelayToken, []byte(out.RelayToken)); err != nil {
		return RelaySecrets{}, err
	}
	if err := s.cfg.Creds.Set(KeyLLMSecret, []byte(out.LLMSecret)); err != nil {
		return RelaySecrets{}, err
	}
	s.restartRelay()
	return out, nil
}

func validateSettings(in PhoneSettings) (PhoneSettings, error) {
	var err error
	if in.RelayURL, err = normalizeRelayURL(in.RelayURL); err != nil {
		return in, err
	}
	in.TwilioNumber = strings.TrimSpace(in.TwilioNumber)
	if in.TwilioNumber != "" && !e164.MatchString(in.TwilioNumber) {
		return in, &Refusal{"The Jarvis number must look like +32470123456."}
	}
	if (in.QuietStart == "") != (in.QuietEnd == "") || in.QuietStart != "" && (!hhmm.MatchString(in.QuietStart) || !hhmm.MatchString(in.QuietEnd)) {
		return in, &Refusal{"Quiet hours need a start and an end like 22:00."}
	}
	if in.DailyCap < 0 || in.DailyCap > 100 || in.MonthlyCapCents < 0 || in.MonthlyCapCents > 1_000_000 || in.CentsPerMinute < 0 || in.CentsPerMinute > 1000 {
		return in, &Refusal{"The call limits are out of range."}
	}
	if len(in.DefaultVoiceID) > 100 || len(in.DefaultTTSModel) > 100 {
		return in, &Refusal{"The voice or model id is too long."}
	}
	switch in.CallLanguage {
	case "", LangDutch, LangEnglish, LangAuto:
	default:
		return in, &Refusal{"The call language must be Dutch, English or Auto."}
	}
	return in, nil
}

// Configure saves the plain settings, provisions ElevenLabs when the relay URL and Jarvis number are set, and
// reconnects the relay if its URL changed. Owner number, verification and provider ids are never taken from in.
func (s *Service) Configure(ctx context.Context, in PhoneSettings) (ConfigureResult, error) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	in, err := validateSettings(in)
	if err != nil {
		return ConfigureResult{}, err
	}
	cur := s.Settings()
	in.OwnerNumber, in.OwnerVerified = cur.OwnerNumber, cur.OwnerVerified
	in.AgentID, in.PhoneNumberID, in.PostCallWebhookID = cur.AgentID, cur.PhoneNumberID, cur.PostCallWebhookID
	if in.TwilioNumber != cur.TwilioNumber {
		in.PhoneNumberID = "" // a different number has to be imported
	}
	orphan := ""
	if in.RelayURL != cur.RelayURL {
		in.PostCallWebhookID = "" // the post-call webhook points at the old relay
		orphan = cur.PostCallWebhookID
	}
	var webhookSecret string
	if in.RelayURL != "" && in.TwilioNumber != "" {
		el, err := s.elevenLabs()
		if err != nil {
			return ConfigureResult{}, err
		}
		sid, err1 := s.cfg.Creds.Get(KeyTwilioSID)
		tok, err2 := s.cfg.Creds.Get(KeyTwilioToken)
		llmSecret, err3 := s.cfg.Creds.Get(KeyLLMSecret)
		if err1 != nil || err2 != nil || err3 != nil || len(llmSecret) == 0 {
			return ConfigureResult{}, &Refusal{"Add the Twilio credentials and generate the relay secrets first."}
		}
		tw := &Twilio{SID: string(sid), Token: string(tok), BaseURL: s.cfg.TwilioURL, HTTP: s.cfg.HTTP}
		out, perr := Provision(ctx, el, tw, in, ProvisionSecrets{LLMSecret: string(llmSecret), TwilioSID: string(sid), TwilioToken: string(tok),
			StoreWebhookSecret: func(secret string) error {
				if err := s.cfg.Creds.Set(KeyWebhookSecret, []byte(secret)); err != nil {
					return err
				}
				webhookSecret = secret
				return nil
			}})
		if perr != nil {
			// Keep what was created so a retry reuses it instead of creating duplicates.
			msg := perr.Error()
			if err := s.apply(out, msg); err != nil {
				s.log.Warn("jarvis: cannot save settings", "err", err)
			}
			s.log.Warn("jarvis: provisioning failed", "err", msg)
			s.noticeOrphan(orphan)
			return ConfigureResult{}, perr
		}
		in = out
	}
	if err := s.apply(in, ""); err != nil {
		return ConfigureResult{}, err
	}
	s.noticeOrphan(orphan)
	return ConfigureResult{Status: s.Status(ctx), WebhookSecret: webhookSecret}, nil
}

// noticeOrphan tells the owner about a post-call webhook that no longer points at the relay; nothing deletes
// webhooks in ElevenLabs automatically.
func (s *Service) noticeOrphan(id string) {
	if id == "" {
		return
	}
	s.cfg.Host.Notify("Delete the old Jarvis webhook", "The relay URL changed, so ElevenLabs webhook "+id+
		" is no longer used. Delete it in the ElevenLabs workspace webhook settings.", "", "info")
	s.cfg.Host.Audit("jarvis.webhook.orphaned", id, "allowed", "relay url changed")
}

// apply saves and adopts settings, records the last provisioning error and reconnects the relay if needed.
func (s *Service) apply(set PhoneSettings, lastErr string) error {
	if s.cfg.SaveSettings != nil {
		if err := s.cfg.SaveSettings(set); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.settings, s.lastErr = set, lastErr
	s.mu.Unlock()
	s.restartRelay()
	return nil
}

// StartVerification phones number and speaks a six-digit code; ConfirmVerification with that code makes it the
// owner number. The stored owner number does not change until then.
func (s *Service) StartVerification(ctx context.Context, number string) error {
	number = strings.TrimSpace(number)
	if !e164.MatchString(number) {
		return &Refusal{"Enter your number like +32470123456."}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	if err := Allow(s.Settings(), s.now(), s.usage(ctx, ""), Ask{QuietOverride: true}); err != nil {
		return err
	}
	s.mu.Lock()
	if s.dialTo != "" {
		s.mu.Unlock()
		return &Refusal{"A verification call is already being placed."}
	}
	s.dialTo = number
	s.mu.Unlock()
	_, err = s.calls.Place(ctx, PlaceRequest{Reason: "phone verification", Verify: code})
	s.mu.Lock()
	s.dialTo = ""
	if err == nil {
		s.pendingCode, s.pendingNum, s.pendingAt, s.attempts = code, number, s.now(), 0
	}
	s.mu.Unlock()
	if err == nil {
		s.cfg.Host.Audit("jarvis.verify.started", "", "allowed", "")
	}
	return err
}

func (s *Service) ConfirmVerification(code string) error {
	s.cfgMu.Lock() // a concurrent Configure must not save over the new owner number
	defer s.cfgMu.Unlock()
	code = strings.TrimSpace(code)
	s.mu.Lock()
	if s.pendingCode == "" || s.now().Sub(s.pendingAt) >= verifyTTL {
		s.pendingCode, s.pendingNum = "", ""
		s.mu.Unlock()
		return &Refusal{"There is no verification in progress; call again."}
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(s.pendingCode)) != 1 {
		s.attempts++
		if s.attempts >= maxVerifyAttempts {
			s.pendingCode, s.pendingNum = "", ""
		}
		s.mu.Unlock()
		return &Refusal{"That code is not right."}
	}
	set := s.settings
	set.OwnerNumber, set.OwnerVerified = s.pendingNum, true
	s.mu.Unlock()
	if s.cfg.SaveSettings != nil {
		if err := s.cfg.SaveSettings(set); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.settings.OwnerNumber, s.settings.OwnerVerified = set.OwnerNumber, true
	s.pendingCode, s.pendingNum = "", ""
	s.mu.Unlock()
	s.cfg.Host.Audit("jarvis.verify.confirmed", "", "allowed", "")
	return nil
}

func (s *Service) Voices(ctx context.Context) ([]Voice, error) {
	el, err := s.elevenLabs()
	if err != nil {
		return nil, err
	}
	return el.Voices(ctx)
}

func (s *Service) Models(ctx context.Context) ([]TTSModel, error) {
	el, err := s.elevenLabs()
	if err != nil {
		return nil, err
	}
	return el.Models(ctx)
}

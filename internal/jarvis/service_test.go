package jarvis

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
)

type svcEnv struct {
	svc   *Service
	host  *fakeHost
	dial  *fakeDialer
	relay *fakeSender
	creds *credentials.Memory
	now   *time.Time
	saved []Settings
}

func newSvcEnv(t *testing.T, cfg func(*ServiceConfig)) *svcEnv {
	t.Helper()
	st, now := testStore(t)
	*now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	e := &svcEnv{host: newFakeHost(), dial: &fakeDialer{}, relay: &fakeSender{}, creds: credentials.NewMemory(), now: now}
	initial := Settings{OwnerNumber: "+32470000000", OwnerVerified: true, AgentID: "ag", PhoneNumberID: "pn"}
	c := ServiceConfig{DB: st.db, Creds: e.creds, Host: e.host, Log: slog.Default(), Now: func() time.Time { return *e.now },
		LoadSettings: func() (Settings, error) { return initial, nil },
		SaveSettings: func(s Settings) error { e.saved = append(e.saved, s); return nil },
		dialer:       e.dial, sender: e.relay}
	if cfg != nil {
		cfg(&c)
	}
	e.svc = NewService(c)
	t.Cleanup(e.svc.Close) // registered after testStore's cleanup, so it runs first
	return e
}

func newTestService(t *testing.T) (*Service, *fakeHost, *fakeDialer, *fakeSender, *time.Time) {
	e := newSvcEnv(t, nil)
	return e.svc, e.host, e.dial, e.relay, e.now
}

func (s *Service) edit(f func(*Settings)) {
	s.mu.Lock()
	f(&s.settings)
	s.mu.Unlock()
}

func TestRuleEventPlacesOneCallAndDeletesOnceRule(t *testing.T) {
	svc, _, dial, _, _ := newTestService(t)
	ctx := context.Background()
	r, _ := svc.AddRule(ctx, Rule{BotID: "b1", Trigger: TaskFailed, Target: "t1", Note: "deploy"})
	svc.OnEvent(Event{Trigger: TaskFailed, TargetID: "t1", Title: "deploy-api failed"})
	svc.wait()
	if len(dial.dialed()) != 1 {
		t.Fatalf("dials=%d", len(dial.dialed()))
	}
	rules, _ := svc.Rules(ctx, "b1")
	if len(rules) != 0 {
		t.Fatalf("once rule %s kept", r.ID)
	}
}

func TestRepeatRuleKeptAndOtherEventsIgnored(t *testing.T) {
	svc, _, dial, _, _ := newTestService(t)
	ctx := context.Background()
	_, _ = svc.AddRule(ctx, Rule{BotID: "b1", Trigger: AutomationFailed, Target: "any", Repeat: true})
	svc.OnEvent(Event{Trigger: AutomationFinished, TargetID: "a1", Title: "nightly succeeded"})
	svc.wait()
	if len(dial.dialed()) != 0 {
		t.Fatal("called on a non-matching event")
	}
	svc.OnEvent(Event{Trigger: AutomationFailed, TargetID: "a1", Title: "nightly failed"})
	svc.wait()
	if len(dial.dialed()) != 1 {
		t.Fatalf("dials=%d", len(dial.dialed()))
	}
	if rules, _ := svc.Rules(ctx, "b1"); len(rules) != 1 {
		t.Fatalf("repeat rule dropped: %+v", rules)
	}
}

func TestQuietHoursTurnRuleIntoNotice(t *testing.T) {
	svc, host, dial, _, _ := newTestService(t)
	svc.edit(func(s *Settings) { s.QuietStart, s.QuietEnd = "11:00", "13:00" })
	_, _ = svc.AddRule(context.Background(), Rule{BotID: "b1", Trigger: TaskFailed, Target: "any"})
	svc.OnEvent(Event{Trigger: TaskFailed, TargetID: "t1", OwnerBotID: "b1", Title: "x"})
	svc.wait()
	if len(dial.dialed()) != 0 || len(host.noticeList()) != 1 {
		t.Fatalf("dials=%d notices=%v", len(dial.dialed()), host.noticeList())
	}
}

func TestMissedAtTimeBecomesNotice(t *testing.T) {
	svc, host, dial, _, now := newTestService(t)
	_, _ = svc.AddRule(context.Background(), Rule{BotID: "b1", Trigger: AtTime, Target: "08:00"})
	svc.lastTick = now.Add(-6 * time.Hour) // asleep from 06:00 to 12:00
	svc.tick(context.Background())
	svc.wait()
	if len(dial.dialed()) != 0 || len(host.noticeList()) != 1 {
		t.Fatalf("dials=%d notices=%v", len(dial.dialed()), host.noticeList())
	}
}

func TestAtTimeFiresOnTick(t *testing.T) {
	svc, _, dial, _, now := newTestService(t)
	_, _ = svc.AddRule(context.Background(), Rule{BotID: "b1", Trigger: AtTime, Target: "12:01", Repeat: true})
	svc.lastTick = *now
	*now = now.Add(90 * time.Second)
	svc.tick(context.Background())
	svc.wait()
	if len(dial.dialed()) != 1 {
		t.Fatalf("dials=%d", len(dial.dialed()))
	}
}

func TestCallOwnerRespectsCoolDown(t *testing.T) {
	svc, _, dial, _, now := newTestService(t)
	ctx := context.Background()
	if _, err := svc.CallOwner(ctx, "b1", "first", false); err != nil {
		t.Fatal(err)
	}
	svc.calls.HandleFrame(ctx, Frame{Type: FrameCallStatus, CallID: dial.dialed()[0].CallID, Status: "done"})
	*now = now.Add(5 * time.Minute)
	if _, err := svc.CallOwner(ctx, "b1", "second", false); err == nil {
		t.Fatal("cool-down ignored")
	}
}

func TestCallOwnerDuringCallIsInjected(t *testing.T) {
	svc, _, dial, _, _ := newTestService(t)
	ctx := context.Background()
	if _, err := svc.CallOwner(ctx, "b1", "first", false); err != nil {
		t.Fatal(err)
	}
	msg, err := svc.CallOwner(ctx, "b2", "second", false)
	if err != nil || !strings.Contains(msg, "already on a call") || len(dial.dialed()) != 1 {
		t.Fatalf("msg=%q err=%v dials=%d", msg, err, len(dial.dialed()))
	}
}

func TestRetryGoesThroughPolicy(t *testing.T) {
	svc, host, dial, _, _ := newTestService(t)
	svc.edit(func(s *Settings) { s.QuietStart, s.QuietEnd = "11:00", "13:00" })
	svc.calls.Retry(context.Background(), PlaceRequest{BotID: "b1", Reason: "deploy failed"})
	if len(dial.dialed()) != 0 {
		t.Fatal("retry ignored quiet hours")
	}
	if n := host.noticeList(); len(n) != 1 || !strings.Contains(n[0], "quiet hours") {
		t.Fatalf("notices=%v", n)
	}
}

func TestVerificationFlow(t *testing.T) {
	svc, _, dial, _, _ := newTestService(t)
	ctx := context.Background()
	if err := svc.StartVerification(ctx, "+32471111111"); err != nil {
		t.Fatal(err)
	}
	d := dial.dialed()
	if len(d) != 1 || d[0].To != "+32471111111" {
		t.Fatalf("dials %+v", d)
	}
	if got := svc.Settings(); got.OwnerNumber != "+32470000000" || !got.OwnerVerified {
		t.Fatalf("settings changed before confirmation: %+v", got)
	}
	if svc.pendingCode != "000000" && svc.ConfirmVerification("000000") == nil {
		t.Fatal("wrong code accepted")
	}
	if err := svc.ConfirmVerification(svc.pendingCode); err != nil {
		t.Fatal(err)
	}
	if got := svc.Settings(); !got.OwnerVerified || got.OwnerNumber != "+32471111111" {
		t.Fatalf("settings=%+v", got)
	}
	if svc.ConfirmVerification("123456") == nil {
		t.Fatal("a used code stays valid")
	}
}

func TestVerificationLocksAfterWrongCodes(t *testing.T) {
	svc, _, dial, _, now := newTestService(t)
	ctx := context.Background()
	if err := svc.StartVerification(ctx, "+32471111111"); err != nil {
		t.Fatal(err)
	}
	svc.calls.HandleFrame(ctx, Frame{Type: FrameCallStatus, CallID: dial.dialed()[0].CallID, Status: "done"})
	code := svc.pendingCode
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for range maxVerifyAttempts {
		_ = svc.ConfirmVerification(wrong)
	}
	if svc.ConfirmVerification(code) == nil {
		t.Fatal("code still valid after too many wrong attempts")
	}
	if err := svc.StartVerification(ctx, "+32471111111"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(verifyTTL + time.Second)
	if svc.ConfirmVerification(svc.pendingCode) == nil {
		t.Fatal("expired code accepted")
	}
}

func TestVerificationRejectsBadNumber(t *testing.T) {
	svc, _, dial, _, _ := newTestService(t)
	if err := svc.StartVerification(context.Background(), "0470 12 34 56"); err == nil || len(dial.dialed()) != 0 {
		t.Fatalf("err=%v dials=%d", err, len(dial.dialed()))
	}
}

func TestGenerateRelaySecretsStoresOnlyRelayAndLLM(t *testing.T) {
	e := newSvcEnv(t, nil)
	out, err := e.svc.GenerateRelaySecrets()
	if err != nil || out.RelayToken == "" || out.LLMSecret == "" || out.RelayToken == out.LLMSecret {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if v, _ := e.creds.Get(KeyRelayToken); string(v) != out.RelayToken {
		t.Fatal("relay token not stored")
	}
	if v, _ := e.creds.Get(KeyLLMSecret); string(v) != out.LLMSecret {
		t.Fatal("llm secret not stored")
	}
	if _, err := e.creds.Get(KeyWebhookSecret); err == nil {
		t.Fatal("webhook secret generated locally")
	}
	st := e.svc.Status(context.Background())
	if !st.HasRelaySecrets || st.HasWebhookSecret {
		t.Fatalf("status %+v", st)
	}
}

func TestConfigureValidatesRelayURL(t *testing.T) {
	e := newSvcEnv(t, nil)
	ctx := context.Background()
	for _, bad := range []string{"http://relay.example", "ws://relay.example", "relay.example", "https://user:pw@relay.example", "https://relay.example?x=1", "http://localhost.evil.com"} {
		if _, err := e.svc.Configure(ctx, Settings{RelayURL: bad}); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, ok := range []string{"https://relay.example.workers.dev/", "http://localhost:8787", "http://127.0.0.1:8787"} {
		if _, err := e.svc.Configure(ctx, Settings{RelayURL: ok}); err != nil {
			t.Errorf("rejected %q: %v", ok, err)
		}
	}
	if got := e.svc.Settings().RelayURL; got != "http://127.0.0.1:8787" {
		t.Fatalf("relay url %q", got)
	}
}

func TestConfigureKeepsServerOwnedFields(t *testing.T) {
	e := newSvcEnv(t, nil)
	res, err := e.svc.Configure(context.Background(), Settings{OwnerNumber: "+19999999999", OwnerVerified: true, AgentID: "evil", QuietStart: "22:00", QuietEnd: "07:00"})
	if err != nil {
		t.Fatal(err)
	}
	got := e.svc.Settings()
	if got.OwnerNumber != "+32470000000" || got.AgentID != "ag" || got.QuietStart != "22:00" || res.WebhookSecret != "" {
		t.Fatalf("settings=%+v res=%+v", got, res)
	}
	if len(e.saved) != 1 || e.saved[0] != got {
		t.Fatalf("saved %+v", e.saved)
	}
	if _, err := e.svc.Configure(context.Background(), Settings{QuietStart: "25:00", QuietEnd: "07:00"}); err == nil {
		t.Fatal("bad quiet hours accepted")
	}
}

func TestConfigureProvisionsAndReturnsWebhookSecretOnce(t *testing.T) {
	srv, _ := provisionFake(t, map[string]string{"PATCH /v1/convai/agents/ag1": `{}`})
	e := newSvcEnv(t, func(c *ServiceConfig) {
		c.HTTP, c.ElevenLabsURL, c.TwilioURL = srv.Client(), srv.URL, srv.URL
		c.LoadSettings = func() (Settings, error) { return Settings{}, nil }
	})
	ctx := context.Background()
	in := Settings{RelayURL: "https://relay.example", TwilioNumber: "+3220000000"}
	if _, err := e.svc.Configure(ctx, in); err == nil {
		t.Fatal("provisioned without credentials")
	}
	_ = e.svc.SetSecrets(SecretsInput{TwilioSID: "AC1", TwilioToken: "t", ElevenLabs: "k"})
	if _, err := e.svc.GenerateRelaySecrets(); err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Configure(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.WebhookSecret != "whsec" || !res.Status.HasWebhookSecret {
		t.Fatalf("res=%+v", res)
	}
	if v, _ := e.creds.Get(KeyWebhookSecret); string(v) != "whsec" {
		t.Fatal("webhook secret not stored")
	}
	got := e.svc.Settings()
	if got.AgentID != "ag1" || got.PhoneNumberID != "pn1" || got.PostCallWebhookID != "wh1" {
		t.Fatalf("settings=%+v", got)
	}
	res, err = e.svc.Configure(ctx, in)
	if err != nil || res.WebhookSecret != "" {
		t.Fatalf("second run res=%+v err=%v", res, err)
	}
}

func TestConfigureFailureKeepsCreatedIDs(t *testing.T) {
	// Assigning the agent to the number fails (no reply registered: 404) after the agent and number were created.
	srv2, _ := fakeAPI(t, map[string]string{
		"POST /v1/convai/agents/create":                          `{"agent_id":"ag1"}`,
		"POST /v1/convai/phone-numbers":                          `{"phone_number_id":"pn1"}`,
		"GET /2010-04-01/Accounts/AC1/IncomingPhoneNumbers.json": `{"incoming_phone_numbers":[{"phone_number":"+3220000000"}]}`,
	})
	e := newSvcEnv(t, func(c *ServiceConfig) {
		c.HTTP, c.ElevenLabsURL, c.TwilioURL = srv2.Client(), srv2.URL, srv2.URL
		c.LoadSettings = func() (Settings, error) { return Settings{}, nil }
	})
	_ = e.svc.SetSecrets(SecretsInput{TwilioSID: "AC1", TwilioToken: "t", ElevenLabs: "k"})
	_, _ = e.svc.GenerateRelaySecrets()
	if _, err := e.svc.Configure(context.Background(), Settings{RelayURL: "https://relay.example", TwilioNumber: "+3220000000"}); err == nil {
		t.Fatal("expected failure")
	}
	got := e.svc.Settings()
	if got.AgentID != "ag1" || got.PhoneNumberID != "pn1" {
		t.Fatalf("created ids lost: %+v", got)
	}
	if e.svc.Status(context.Background()).LastError == "" {
		t.Fatal("no last error")
	}
}

func TestStatusNeverLeaksSecrets(t *testing.T) {
	e := newSvcEnv(t, nil)
	_ = e.svc.SetSecrets(SecretsInput{TwilioSID: "AC1", TwilioToken: "tok-secret", ElevenLabs: "el-secret"})
	st := e.svc.Status(context.Background())
	if !st.HasTwilio || !st.HasElevenLabs {
		t.Fatalf("status %+v", st)
	}
}

// panicHost panics on Bots, which an inbound lookup reaches.
type panicHost struct{ *fakeHost }

func (panicHost) Bots(context.Context) ([]chat.Bot, error) { panic("boom") }

func TestFrameHandlerRecoversFromPanic(t *testing.T) {
	e := newSvcEnv(t, func(c *ServiceConfig) { c.Host = panicHost{newFakeHost()} })
	e.svc.handleFrame(Frame{Type: FrameInboundLookup, ID: "r1", Caller: "+32470000000"})
}

func TestRelayRestartsOnNewURL(t *testing.T) {
	var aConns, bConns atomic.Int32
	aClosed := make(chan struct{}, 1)
	server := func(n *atomic.Int32, closed chan struct{}) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/daemon" || r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			n.Add(1)
			_, _, _ = c.Read(context.Background()) // blocks until the client goes away
			if closed != nil {
				closed <- struct{}{}
			}
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	a, b := server(&aConns, aClosed), server(&bConns, nil)
	e := newSvcEnv(t, func(c *ServiceConfig) {
		c.sender = nil // use the real relay holder
		c.LoadSettings = func() (Settings, error) { return Settings{RelayURL: a.URL}, nil }
	})
	_ = e.creds.Set(KeyRelayToken, []byte("tok"))
	e.svc.Start(context.Background())
	waitFor(t, func() bool { return e.svc.Status(context.Background()).RelayConnected && aConns.Load() == 1 })
	if _, err := e.svc.Configure(context.Background(), Settings{RelayURL: b.URL}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-aClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("old relay connection not closed")
	}
	waitFor(t, func() bool { return e.svc.Status(context.Background()).RelayConnected && bConns.Load() == 1 })
	// Unchanged settings keep the connection.
	if _, err := e.svc.Configure(context.Background(), Settings{RelayURL: b.URL, QuietStart: "22:00", QuietEnd: "07:00"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if bConns.Load() != 1 {
		t.Fatalf("reconnected without a change: %d", bConns.Load())
	}
	e.svc.Close()
	if e.svc.Status(context.Background()).RelayConnected {
		t.Fatal("relay still connected after Close")
	}
}

func TestCloseStopsEventHandling(t *testing.T) {
	svc, _, dial, _, _ := newTestService(t)
	_, _ = svc.AddRule(context.Background(), Rule{BotID: "b1", Trigger: TaskFailed, Target: "t1"})
	svc.Start(context.Background())
	svc.Close()
	svc.Close() // idempotent
	svc.OnEvent(Event{Trigger: TaskFailed, TargetID: "t1", Title: "x"})
	svc.wait()
	if len(dial.dialed()) != 0 {
		t.Fatal("event handled after Close")
	}
}

func TestRefusalsAreTyped(t *testing.T) {
	svc, _, _, _, _ := newTestService(t)
	_, err := svc.Voices(context.Background())
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("missing key error not a refusal: %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

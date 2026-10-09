package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
)

const secretKey = "sk-or-v1-0123456789abcdefSECRET1234"

type memSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSettings) Get(_ context.Context, k string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	return v, ok, nil
}

func (s *memSettings) Set(_ context.Context, k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[k] = v
	return nil
}

// fakeEnv builds a Manager over a scripted provider that accepts only secretKey.
type fakeEnv struct {
	mgr      *llm.Manager
	creds    *credentials.Memory
	settings *memSettings
	mu       sync.Mutex
	modelHit int
	now      time.Time
	verifier string
}

func newFakeEnv(t *testing.T) *fakeEnv {
	t.Helper()
	env := &fakeEnv{creds: credentials.NewMemory(), settings: &memSettings{}, now: time.Unix(1_800_000_000, 0)}
	desc := llm.Descriptor{
		ID: "openrouter", Name: "Fake Router",
		New: func(cfg llm.Config) (llm.Provider, error) {
			f := &llmtest.Fake{IDValue: "openrouter"}
			if cfg.APIKey != secretKey {
				// A real provider echoes the rejected key in nothing; make the fake careless.
				f.ValidateErr = &llm.Error{Code: llm.ErrAuth, Message: "invalid key " + cfg.APIKey, Status: 401}
			}
			f.ModelList = []llm.Model{{ID: "m1", Name: "M1"}}
			return &countingProvider{Fake: f, hit: func() { env.mu.Lock(); env.modelHit++; env.mu.Unlock() }}, nil
		},
		OAuth: &llm.OAuth{
			AuthURL: func(ch string) string {
				return "https://example.test/auth?code_challenge=" + ch + "&code_challenge_method=S256"
			},
			Exchange: func(_ context.Context, _ llm.HTTPDoer, code, verifier string) (string, error) {
				env.verifier = verifier
				if code != "good-code" {
					return "", &llm.Error{Code: llm.ErrAuth, Message: "bad code " + code, Status: 403}
				}
				return secretKey, nil
			},
		},
	}
	other := llm.Descriptor{ID: "openai", Name: "Other", New: desc.New}
	reg, err := llm.NewRegistry(desc, other)
	if err != nil {
		t.Fatal(err)
	}
	env.mgr, err = llm.NewManager(llm.ManagerConfig{
		Registry: reg, Credentials: env.creds, Settings: env.settings,
		Now: func() time.Time { env.mu.Lock(); defer env.mu.Unlock(); return env.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func (e *fakeEnv) advance(d time.Duration) { e.mu.Lock(); e.now = e.now.Add(d); e.mu.Unlock() }

type countingProvider struct {
	*llmtest.Fake
	hit func()
}

func (c *countingProvider) Models(ctx context.Context) ([]llm.Model, error) {
	c.hit()
	return c.Fake.Models(ctx)
}

func noKey(t *testing.T, what string, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if s := string(b); strings.Contains(s, secretKey) || strings.Contains(s, "0123456789abcdef") {
		t.Errorf("%s contains the API key: %s", what, s)
	}
}

func TestManagerConnectDisconnectTest(t *testing.T) {
	env := newFakeEnv(t)
	ctx := context.Background()

	sts, err := env.mgr.Statuses(ctx)
	if err != nil || len(sts) != 2 || sts[0].Connected || sts[0].ID != "openrouter" {
		t.Fatalf("initial statuses = %+v err=%v", sts, err)
	}
	if _, err := env.mgr.Provider(ctx, "openrouter"); !errors.Is(err, llm.ErrNotConfigured) {
		t.Fatalf("Provider before connect: %v", err)
	}

	// A rejected key is not stored and never echoed back.
	_, err = env.mgr.Connect(ctx, "openrouter", "sk-wrong-key-0000000000")
	if err == nil {
		t.Fatal("Connect accepted a rejected key")
	}
	if strings.Contains(err.Error(), "sk-wrong") {
		t.Errorf("error leaks the key: %v", err)
	}
	if _, gerr := env.creds.Get(llm.CredentialKey("openrouter")); !errors.Is(gerr, credentials.ErrNotFound) {
		t.Fatalf("rejected key was stored: %v", gerr)
	}

	st, err := env.mgr.Connect(ctx, "openrouter", "  "+secretKey+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.KeyHint != secretKey[len(secretKey)-4:] || st.Valid == nil || !*st.Valid || st.Account == nil || st.Account.Label != "fake" {
		t.Fatalf("status after connect = %+v", st)
	}
	noKey(t, "status", st)
	if raw, _ := env.creds.Get("llm/openrouter/key"); string(raw) != secretKey {
		t.Fatalf("stored key = %q", raw)
	}
	if p, err := env.mgr.Provider(ctx, "openrouter"); err != nil || p.ID() != "openrouter" {
		t.Fatalf("Provider after connect: %v %v", p, err)
	}

	// Test re-validates; a key that went bad is reported through the status.
	st, err = env.mgr.Test(ctx, "openrouter")
	if err != nil || st.Valid == nil || !*st.Valid {
		t.Fatalf("Test = %+v err=%v", st, err)
	}
	env.creds.Set("llm/openrouter/key", []byte("sk-revoked-key-000000000"))
	st, err = env.mgr.Test(ctx, "openrouter")
	if err != nil || st.Valid == nil || *st.Valid || st.Error == "" {
		t.Fatalf("Test after revoke = %+v err=%v", st, err)
	}
	if strings.Contains(st.Error, "sk-revoked") {
		t.Errorf("status error leaks key: %q", st.Error)
	}
	noKey(t, "revoked status", st)

	if err := env.mgr.Disconnect(ctx, "openrouter"); err != nil {
		t.Fatal(err)
	}
	if err := env.mgr.Disconnect(ctx, "openrouter"); err != nil {
		t.Fatalf("second Disconnect: %v", err)
	}
	sts, _ = env.mgr.Statuses(ctx)
	if sts[0].Connected || sts[0].KeyHint != "" || sts[0].Valid != nil {
		t.Fatalf("status after disconnect = %+v", sts[0])
	}
	if _, err := env.mgr.Test(ctx, "openrouter"); !errors.Is(err, llm.ErrNotConfigured) {
		t.Fatalf("Test after disconnect: %v", err)
	}
	if _, err := env.mgr.Connect(ctx, "nope", secretKey); err == nil {
		t.Error("unknown provider accepted")
	}
	if _, err := env.mgr.Connect(ctx, "openrouter", "   "); err == nil {
		t.Error("blank key accepted")
	}
}

func TestManagerModelsCache(t *testing.T) {
	env := newFakeEnv(t)
	ctx := context.Background()
	if _, err := env.mgr.Models(ctx, "openrouter"); !errors.Is(err, llm.ErrNotConfigured) {
		t.Fatalf("Models before connect: %v", err)
	}
	if _, err := env.mgr.Connect(ctx, "openrouter", secretKey); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		ms, err := env.mgr.Models(ctx, "openrouter")
		if err != nil || len(ms) != 1 || ms[0].ID != "m1" {
			t.Fatalf("Models = %+v err=%v", ms, err)
		}
		ms[0].ID = "mutated" // callers must not be able to corrupt the cache
	}
	if env.modelHit != 1 {
		t.Fatalf("provider hit %d times within TTL, want 1", env.modelHit)
	}
	env.advance(6 * time.Minute)
	if ms, _ := env.mgr.Models(ctx, "openrouter"); ms[0].ID != "m1" {
		t.Fatalf("cache returned mutated data: %+v", ms)
	}
	if env.modelHit != 2 {
		t.Fatalf("provider hit %d times after TTL, want 2", env.modelHit)
	}
	if err := env.mgr.Disconnect(ctx, "openrouter"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.mgr.Connect(ctx, "openrouter", secretKey); err != nil {
		t.Fatal(err)
	}
	env.mgr.Models(ctx, "openrouter")
	if env.modelHit != 3 {
		t.Fatalf("cache survived reconnect: %d hits", env.modelHit)
	}
}

func TestManagerDefaults(t *testing.T) {
	env := newFakeEnv(t)
	ctx := context.Background()
	if d, err := env.mgr.Defaults(ctx); err != nil || d != (llm.Defaults{}) {
		t.Fatalf("initial defaults = %+v err=%v", d, err)
	}
	want := llm.Defaults{ChatModel: "openrouter/anthropic/claude-sonnet-4", MemoryModel: "openai/gpt-5-mini"}
	if err := env.mgr.SetDefaults(ctx, want); err != nil {
		t.Fatal(err)
	}
	if d, _ := env.mgr.Defaults(ctx); d != want {
		t.Fatalf("defaults = %+v, want %+v", d, want)
	}
	if env.settings.m["llm.chatModel"] != `"openrouter/anthropic/claude-sonnet-4"` {
		t.Errorf("stored chatModel = %q", env.settings.m["llm.chatModel"])
	}
	for _, bad := range []llm.Defaults{{ChatModel: "no-slash"}, {MemoryModel: "ghost/model"}} {
		if err := env.mgr.SetDefaults(ctx, bad); err == nil {
			t.Errorf("SetDefaults(%+v) accepted", bad)
		}
	}
	if d, _ := env.mgr.Defaults(ctx); d != want {
		t.Errorf("rejected SetDefaults changed state: %+v", d)
	}
	if err := env.mgr.SetDefaults(ctx, llm.Defaults{}); err != nil {
		t.Fatal(err)
	}
	if d, _ := env.mgr.Defaults(ctx); d != (llm.Defaults{}) {
		t.Errorf("defaults not cleared: %+v", d)
	}
}

func TestManagerOpenRouterAuth(t *testing.T) {
	env := newFakeEnv(t)
	ctx := context.Background()

	if _, err := env.mgr.OpenRouterAuthFinish(ctx, "good-code"); err == nil {
		t.Fatal("finish without start accepted")
	}
	start, err := env.mgr.OpenRouterAuthStart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(start.URL)
	if err != nil {
		t.Fatal(err)
	}
	challenge := u.Query().Get("code_challenge")
	if challenge == "" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("auth url = %s", start.URL)
	}

	if _, err := env.mgr.OpenRouterAuthFinish(ctx, "wrong"); err == nil || strings.Contains(err.Error(), env.verifier+"x") {
		t.Fatalf("wrong code: %v", err)
	}
	if llm.PKCEChallenge(env.verifier) != challenge {
		t.Fatalf("verifier %q does not hash to challenge %q", env.verifier, challenge)
	}
	// The flow stays pending after a wrong code so the user can paste again.
	st, err := env.mgr.OpenRouterAuthFinish(ctx, " good-code\n")
	if err != nil || !st.Connected || st.ID != "openrouter" {
		t.Fatalf("finish = %+v err=%v", st, err)
	}
	noKey(t, "finish status", st)
	if _, err := env.mgr.OpenRouterAuthFinish(ctx, "good-code"); err == nil {
		t.Error("a second finish reused the consumed flow")
	}

	// Expiry after ten minutes.
	if _, err := env.mgr.OpenRouterAuthStart(ctx); err != nil {
		t.Fatal(err)
	}
	env.advance(11 * time.Minute)
	if _, err := env.mgr.OpenRouterAuthFinish(ctx, "good-code"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired flow: %v", err)
	}
	// A new start replaces the pending flow.
	env.mgr.OpenRouterAuthStart(ctx)
	first := ""
	env.mgr.OpenRouterAuthFinish(ctx, "x")
	first = env.verifier
	env.mgr.OpenRouterAuthStart(ctx)
	env.mgr.OpenRouterAuthFinish(ctx, "x")
	if env.verifier == first {
		t.Error("second start did not issue a fresh verifier")
	}
}

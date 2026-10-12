package pricing

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const testSnapshot = `{"fetchedAt":"2026-01-01T00:00:00Z","providers":{
"anthropic":{
"claude-opus-4-1":{"input":15,"output":75,"cache_read":1.5,"cache_write":18.75},
"claude-opus-4-1-20250805":{"input":15,"output":75,"cache_read":1.5,"cache_write":18.75},
"claude-sonnet-4-5":{"input":3,"output":15,"cache_read":0.3,"cache_write":3.75}},
"openai":{
"gpt-5":{"input":1.25,"output":10,"cache_read":0.125},
"gpt-4.1":{"input":2,"output":8}},
"google":{
"gemini-2.5-pro":{"input":1.25,"output":10,"cache_write":2}},
"openrouter":{
"anthropic/claude-sonnet-4.5":{"input":3.3,"output":16.5,"cache_read":0.33,"cache_write":4.125},
"moonshotai/kimi-k2":{"input":0.6,"output":2.5}}}}`

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Parse([]byte(testSnapshot))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLookupNormalisesProviderAndModelSpellings(t *testing.T) {
	c := testCatalog(t)
	opus := Price{Input: 15, Output: 75, CacheRead: 1.5, CacheWrite: 18.75}
	sonnet := Price{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}
	routed := Price{Input: 3.3, Output: 16.5, CacheRead: 0.33, CacheWrite: 4.125}
	for _, tc := range []struct {
		name, provider, model string
		want                  Price
		ok                    bool
	}{
		{"exact", "anthropic", "claude-opus-4-1", opus, true},
		{"dot version", "anthropic", "claude-opus-4.1", opus, true},
		{"release date", "anthropic", "claude-sonnet-4-5-20250929", sonnet, true},
		{"vertex date", "anthropic", "claude-sonnet-4-5@20250929", sonnet, true},
		{"context suffix and case", "anthropic", "Claude-Sonnet-4-5[1m]", sonnet, true},
		{"harness alias", "claude", "claude-sonnet-4-5", sonnet, true},
		{"gateway falls back to any", "vercel-ai-gateway", "openai/gpt-5", Price{Input: 1.25, Output: 10, CacheRead: 0.125}, true},
		{"local provider stays unpriced", "lmstudio", "openai/gpt-oss-20b", Price{}, false},
		{"local provider never borrows a cloud price", "ollama", "gpt-5", Price{}, false},
		{"unknown proxy stays unpriced", "some-proxy", "claude-opus-4-1", Price{}, false},
		{"provider prefix on model", "anthropic", "anthropic/claude-sonnet-4.5", sonnet, true},
		{"gateway keeps its own price", "openrouter", "anthropic/claude-sonnet-4.5", routed, true},
		{"gateway by bare id", "openrouter", "claude-sonnet-4.5", routed, true},
		{"no provider", "", "claude-opus-4-1-20250805", opus, true},
		{"codex alias", "openai-codex", "gpt-4.1", Price{Input: 2, Output: 8, CacheRead: 2, CacheWrite: 2}, true},
		{"provider miss searches the rest", "anthropic", "kimi-k2", Price{Input: 0.6, Output: 2.5, CacheRead: 0.6, CacheWrite: 0.6}, true},
		{"unknown model", "anthropic", "claude-imaginary-9", Price{}, false},
		{"empty model", "anthropic", "", Price{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := c.Lookup(tc.provider, tc.model)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("Lookup(%q, %q) = %+v, %v; want %+v, %v", tc.provider, tc.model, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// Cache prices: no cache field at all bills both cache buckets at the input price; one listed
// field leaves the other unbilled.
func TestCachePriceRule(t *testing.T) {
	c := testCatalog(t)
	for _, tc := range []struct {
		provider, model string
		read, write     float64
	}{
		{"openai", "gpt-4.1", 2, 2},        // neither listed
		{"openai", "gpt-5", 0.125, 0},      // cache_read only
		{"google", "gemini-2.5-pro", 0, 2}, // cache_write only
		{"anthropic", "claude-opus-4-1", 1.5, 18.75},
	} {
		p, ok := c.Lookup(tc.provider, tc.model)
		if !ok || p.CacheRead != tc.read || p.CacheWrite != tc.write {
			t.Errorf("%s/%s: %+v %v, want cacheRead %v cacheWrite %v", tc.provider, tc.model, p, ok, tc.read, tc.write)
		}
	}
}

func TestCost(t *testing.T) {
	p := Price{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}
	for _, tc := range []struct {
		tokens Tokens
		want   float64
	}{
		{Tokens{}, 0},
		{Tokens{Input: 1_000_000}, 3},
		{Tokens{Output: 2_000_000}, 30},
		{Tokens{Input: 1000, Output: 500, CacheRead: 100_000, CacheWrite: 20_000}, 0.003 + 0.0075 + 0.03 + 0.075},
	} {
		if got := p.Cost(tc.tokens); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("Cost(%+v) = %v, want %v", tc.tokens, got, tc.want)
		}
	}
}

func TestEmbeddedSnapshotPricesHarnessModels(t *testing.T) {
	c := Embedded()
	if c.UpdatedAt().IsZero() {
		t.Fatal("embedded snapshot has no fetch time")
	}
	for _, m := range [][2]string{{"anthropic", "claude-haiku-4-5-20251001"}, {"anthropic", "claude-sonnet-4-5"}, {"openai", "gpt-5"}} {
		if p, ok := c.Lookup(m[0], m[1]); !ok || p.Input <= 0 || p.Output <= 0 {
			t.Errorf("embedded snapshot cannot price %s/%s: %+v %v", m[0], m[1], p, ok)
		}
	}
}

func modelsDev(at string) string {
	return `{"anthropic":{"id":"anthropic","models":{"claude-new":{"id":"claude-new","cost":{"input":` + at + `,"output":1}},"free-text":{"id":"free-text"}}},
"openai":{"models":{"gpt-new":{"cost":{"input":1,"output":2,"cache_read":0.5}}}},
"unlisted":{"models":{"x":{"cost":{"input":1,"output":1}}}}}`
}

func TestTrimKeepsListedProvidersAndRoundTrips(t *testing.T) {
	out, err := Trim([]byte(modelsDev("7")), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := c.Lookup("anthropic", "claude-new"); !ok || p.Input != 7 {
		t.Fatalf("trimmed price: %+v %v", p, ok)
	}
	if _, ok := c.Lookup("", "x"); ok {
		t.Fatal("a provider outside Providers survived the trim")
	}
	if _, ok := c.Lookup("anthropic", "free-text"); ok {
		t.Fatal("a model without a cost survived the trim")
	}
	if _, err := Trim([]byte(`{"anthropic":{"models":{}}}`), time.Now()); err == nil {
		t.Fatal("a catalogue without anthropic and openai prices was accepted")
	}
}

func TestRefresherCachesDownloadsAndKeepsCatalogueOnFailure(t *testing.T) {
	var hits atomic.Int32
	fail := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(modelsDev("9")))
	}))
	defer srv.Close()
	dir := t.TempDir()
	old := testCatalog(t)
	r := &Refresher{Catalog: old, Dir: dir, URL: srv.URL}
	if !r.due(time.Now()) {
		t.Fatal("a stale catalogue without an attempt record is not due")
	}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p, ok := old.Lookup("anthropic", "claude-new"); !ok || p.Input != 9 {
		t.Fatalf("refresh did not swap the catalogue: %+v %v", p, ok)
	}
	if r.due(time.Now()) {
		t.Fatal("a refresh is due right after one")
	}
	// A fresh process loads the cached download instead of the older catalogue it starts with.
	restarted := testCatalog(t)
	(&Refresher{Catalog: restarted, Dir: dir}).LoadCache()
	if p, ok := restarted.Lookup("anthropic", "claude-new"); !ok || p.Input != 9 {
		t.Fatalf("cache not loaded: %+v %v", p, ok)
	}

	// A failed download keeps the catalogue and still counts as the day's attempt.
	stale := testCatalog(t)
	failDir := t.TempDir()
	fail.Store(true)
	r = &Refresher{Catalog: stale, Dir: failDir, URL: srv.URL}
	if err := r.Refresh(context.Background()); err == nil {
		t.Fatal("a failed download reported success")
	}
	if _, ok := stale.Lookup("anthropic", "claude-opus-4-1"); !ok {
		t.Fatal("a failed refresh dropped the catalogue")
	}
	if r.due(time.Now()) {
		t.Fatal("a failed attempt is retried within the refresh interval")
	}
	if !r.due(time.Now().Add(RefreshInterval + time.Minute)) {
		t.Fatal("no new attempt after the refresh interval")
	}
	if _, err := os.Stat(filepath.Join(failDir, cacheFile)); !os.IsNotExist(err) {
		t.Fatalf("a failed download wrote a cache file: %v", err)
	}
	// An unreadable cache file keeps the catalogue in use.
	if err := os.WriteFile(filepath.Join(failDir, cacheFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.LoadCache()
	if _, ok := stale.Lookup("anthropic", "claude-opus-4-1"); !ok {
		t.Fatal("a corrupt cache replaced the catalogue")
	}
	if hits.Load() != 2 {
		t.Fatalf("downloads = %d, want 2", hits.Load())
	}
}

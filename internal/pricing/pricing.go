// Package pricing prices model token usage from the models.dev catalogue.
//
// The catalogue ships as an embedded, trimmed snapshot of https://models.dev/api.json
// (snapshot.json, regenerated with `go generate ./internal/pricing`). The daemon refreshes it
// from models.dev at most once per 24 hours (Refresher) and caches the trimmed result in its
// data directory; any failure keeps the newest catalogue it already has.
//
// Prices are list prices in USD per million tokens. Tiered and long-context surcharges that
// models.dev lists separately are ignored: a token is priced at the model's base rate.
//
// Cache prices: when models.dev lists neither cache_read nor cache_write for a model, both cache
// buckets are priced at the input price (the provider bills cached tokens like input as far as the
// catalogue knows). When it lists one of them, the other is treated as not billed (0): OpenAI-style
// models list only cache_read because the provider does not bill cache writes.
package pricing

//go:generate go run ./gen

import (
	_ "embed"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// SourceURL is the models.dev catalogue the snapshot and refreshes come from.
const SourceURL = "https://models.dev/api.json"

// Providers are the models.dev providers kept in the trimmed catalogue: the vendors the agent
// harnesses talk to directly (Claude Code: anthropic, Codex: openai, Oh My Pi: its pi-ai
// providers) plus the common gateways. The order is the fallback search order of Lookup.
var Providers = []string{
	"anthropic", "openai", "google", "xai", "deepseek", "mistral", "moonshotai", "zai",
	"groq", "cerebras", "minimax", "alibaba", "fireworks-ai", "togetherai", "deepinfra",
	"perplexity", "cohere", "google-vertex", "google-vertex-anthropic", "amazon-bedrock", "openrouter",
}

// providerAliases maps harness provider ids (pi-ai, Codex) onto models.dev provider ids.
var providerAliases = map[string]string{
	"claude":             "anthropic",
	"codex":              "openai",
	"openai-codex":       "openai",
	"google-gemini-cli":  "google",
	"google-antigravity": "google",
	"gemini":             "google",
	"xai-oauth":          "xai",
	"moonshot":           "moonshotai",
	"kimi-code":          "moonshotai",
	"fireworks":          "fireworks-ai",
	"together":           "togetherai",
	"bedrock":            "amazon-bedrock",
}

// gateways are hosted routers that bill a cloud model at (about) its vendor's price, so a model
// they route is priced from whichever kept provider lists it. Any provider neither kept, aliased
// nor listed here (lmstudio, ollama, llama.cpp, a custom proxy...) may run a model locally or at
// its own rates, so its usage stays unpriced rather than borrowing a cloud price.
var gateways = map[string]bool{
	"vercel":                true,
	"vercel-ai-gateway":     true,
	"cloudflare-ai-gateway": true,
	"requesty":              true,
	"zenmux":                true,
}

// Price is a model's list price in USD per million tokens.
type Price struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// Tokens are billed token counts. Input excludes cache reads and writes.
type Tokens struct {
	Input, Output, CacheRead, CacheWrite int64
}

// Cost prices t in USD.
func (p Price) Cost(t Tokens) float64 {
	return (float64(t.Input)*p.Input + float64(t.Output)*p.Output +
		float64(t.CacheRead)*p.CacheRead + float64(t.CacheWrite)*p.CacheWrite) / 1e6
}

// rawCost is one model's models.dev cost object; absent fields stay nil.
type rawCost struct {
	Input      *float64 `json:"input,omitempty"`
	Output     *float64 `json:"output,omitempty"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

func (c rawCost) price() (Price, bool) {
	if c.Input == nil || c.Output == nil {
		return Price{}, false
	}
	p := Price{Input: *c.Input, Output: *c.Output}
	switch {
	case c.CacheRead == nil && c.CacheWrite == nil:
		p.CacheRead, p.CacheWrite = p.Input, p.Input
	default:
		if c.CacheRead != nil {
			p.CacheRead = *c.CacheRead
		}
		if c.CacheWrite != nil {
			p.CacheWrite = *c.CacheWrite
		}
	}
	return p, true
}

// snapshot is the trimmed catalogue: provider -> model id -> cost, as fetched at FetchedAt.
type snapshot struct {
	FetchedAt time.Time                     `json:"fetchedAt"`
	Providers map[string]map[string]rawCost `json:"providers"`
}

// Trim reduces a models.dev api.json document to the kept providers' model costs.
func Trim(raw []byte, fetchedAt time.Time) ([]byte, error) {
	var doc map[string]struct {
		Models map[string]struct {
			Cost *rawCost `json:"cost"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	snap := snapshot{FetchedAt: fetchedAt.UTC(), Providers: map[string]map[string]rawCost{}}
	for _, p := range Providers {
		models := map[string]rawCost{}
		for id, m := range doc[p].Models {
			if m.Cost != nil && m.Cost.Input != nil && m.Cost.Output != nil {
				models[id] = *m.Cost
			}
		}
		if len(models) > 0 {
			snap.Providers[p] = models
		}
	}
	if len(snap.Providers["anthropic"]) == 0 || len(snap.Providers["openai"]) == 0 {
		return nil, errors.New("pricing: catalogue has no anthropic or openai prices")
	}
	return encode(snap)
}

// index is a parsed catalogue. byProvider maps normalised lookup keys to prices.
type index struct {
	at         time.Time
	byProvider map[string]map[string]Price
}

// encode writes snap with one model per line: small, sorted and diff-friendly.
func encode(snap snapshot) ([]byte, error) {
	at, err := json.Marshal(snap.FetchedAt)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(`{"fetchedAt":` + string(at) + `,"providers":{`)
	providers := slices.Sorted(maps.Keys(snap.Providers))
	for i, p := range providers {
		if i > 0 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(p)
		b.WriteString("\n" + string(name) + ":{")
		models := slices.Sorted(maps.Keys(snap.Providers[p]))
		for j, id := range models {
			if j > 0 {
				b.WriteByte(',')
			}
			key, _ := json.Marshal(id)
			cost, err := json.Marshal(snap.Providers[p][id])
			if err != nil {
				return nil, err
			}
			b.WriteString("\n" + string(key) + ":" + string(cost))
		}
		b.WriteString("}")
	}
	b.WriteString("}}")
	return []byte(b.String()), nil
}

func parse(data []byte) (*index, error) {
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	if snap.FetchedAt.IsZero() || len(snap.Providers) == 0 {
		return nil, errors.New("pricing: empty catalogue")
	}
	ix := &index{at: snap.FetchedAt, byProvider: map[string]map[string]Price{}}
	for provider, models := range snap.Providers {
		ids := make([]string, 0, len(models))
		for id := range models {
			ids = append(ids, id)
		}
		slices.Sort(ids) // deterministic winners for keys several ids share
		keys := map[string]Price{}
		// Exact ids win over every derived key; derived keys fill in only where still free.
		for _, derive := range []func(string) string{normalize, undated, segment, func(s string) string { return undated(segment(s)) }} {
			for _, id := range ids {
				price, ok := models[id].price()
				if !ok {
					continue
				}
				if k := derive(normalize(id)); k != "" {
					if _, taken := keys[k]; !taken {
						keys[k] = price
					}
				}
			}
		}
		ix.byProvider[provider] = keys
	}
	return ix, nil
}

var (
	bracketSuffix = regexp.MustCompile(`\[[^\]]*\]$`)
	dateSuffix    = regexp.MustCompile(`(?:[-@]\d{8}|-\d{4}-\d{2}-\d{2})$`)
)

// normalize lowercases a model id, drops a `[1m]`-style suffix and spells version dots as dashes
// (claude-opus-4.1 = claude-opus-4-1).
func normalize(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.TrimSpace(bracketSuffix.ReplaceAllString(id, ""))
	return strings.ReplaceAll(id, ".", "-")
}

// undated drops a release-date suffix (-20250514, -2025-05-14, @20250514).
func undated(key string) string { return dateSuffix.ReplaceAllString(key, "") }

// segment drops provider prefixes ("anthropic/claude-x" -> "claude-x").
func segment(key string) string {
	if i := strings.LastIndexByte(key, '/'); i >= 0 {
		return key[i+1:]
	}
	return key
}

//go:embed snapshot.json
var embedded []byte

// Catalog is a swappable price catalogue, safe for concurrent use.
type Catalog struct {
	mu sync.RWMutex
	ix *index
}

// embeddedIndex parses the built-in snapshot once; an index is never mutated after parse.
var embeddedIndex = sync.OnceValue(func() *index {
	ix, err := parse(embedded)
	if err != nil {
		panic("pricing: embedded snapshot: " + err.Error())
	}
	return ix
})

// Embedded returns a catalogue holding the snapshot built into the binary.
func Embedded() *Catalog { return &Catalog{ix: embeddedIndex()} }

// Parse returns a catalogue from trimmed snapshot JSON (the format Trim writes).
func Parse(data []byte) (*Catalog, error) {
	ix, err := parse(data)
	if err != nil {
		return nil, err
	}
	return &Catalog{ix: ix}, nil
}

// UpdatedAt is when the catalogue in use was fetched from models.dev.
func (c *Catalog) UpdatedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ix.at
}

// replace swaps in data when it parses and is newer than the catalogue in use.
func (c *Catalog) replace(data []byte) (bool, error) {
	ix, err := parse(data)
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !ix.at.After(c.ix.at) {
		return false, nil
	}
	c.ix = ix
	return true, nil
}

// Lookup finds the price of model as billed by provider. provider may be a models.dev id, a
// harness alias (claude, codex, openai-codex...) or empty; model may carry a provider prefix
// ("anthropic/claude-sonnet-4.5"), a release date or a `[1m]` suffix. When provider is empty, a
// kept provider, an alias or a gateway and has no such model, every kept provider is searched in
// Providers order; any other provider prices nothing (its models may be local or custom-priced).
func (c *Catalog) Lookup(provider, model string) (Price, bool) {
	key := normalize(model)
	if key == "" {
		return Price{}, false
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if alias, ok := providerAliases[provider]; ok {
		provider = alias
	}
	c.mu.RLock()
	ix := c.ix
	c.mu.RUnlock()
	candidates := []string{key, undated(key), segment(key), undated(segment(key))}
	find := func(p string) (Price, bool) {
		keys := ix.byProvider[p]
		for _, k := range candidates {
			if price, ok := keys[k]; ok {
				return price, true
			}
		}
		return Price{}, false
	}
	if provider != "" {
		if price, ok := find(provider); ok {
			return price, true
		}
		if !slices.Contains(Providers, provider) && !gateways[provider] {
			return Price{}, false
		}
	}
	for _, p := range Providers {
		if p != provider {
			if price, ok := find(p); ok {
				return price, true
			}
		}
	}
	return Price{}, false
}

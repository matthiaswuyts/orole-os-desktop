// Package tools implements the built-in Chat tools that are not memory related: hosted web search
// and fetch (provider-run), the client fallbacks that run in the daemon (fetch_url, datetime), the
// provider-agnostic research loop, bot-scoped MCP tools and registry skills.
//
// Every tool follows the chat.Tool contract: Specs never offers a tool that cannot run for the bot
// and model at hand, and everything that comes from the outside world is returned Untrusted.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Bounds applied to bot supplied hosted tool parameters.
const (
	maxResultsCap       = 10
	maxUsesCap          = 10
	maxContentTokensCap = 100000
	maxDomains          = 20
)

// FetchURLName is the function name of the client side fetch fallback.
const FetchURLName = "fetch_url"

// botTool returns the bot's configuration of a built-in tool.
func botTool(bot chat.Bot, id string) (chat.BotTool, bool) {
	for _, t := range bot.Config.Tools {
		if t.ID == id {
			return t, true
		}
	}
	return chat.BotTool{}, false
}

func hasHosted(caps llm.Caps, id string) bool { return slices.Contains(caps.HostedTools, id) }

// SanitizeParams whitelists and bounds the hosted tool parameters a bot may carry. Unknown keys,
// wrong types and out-of-range values are dropped or clamped; the result never exceeds the caps
// documented on llm.ToolSpec.Params.
func SanitizeParams(hosted string, in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	var allowed []string
	switch hosted {
	case llm.HostedWebSearch:
		allowed = []string{"max_results", "allowed_domains", "blocked_domains", "max_uses"}
	case llm.HostedWebFetch:
		allowed = []string{"allowed_domains", "blocked_domains", "max_uses", "max_content_tokens"}
	default:
		return nil
	}
	out := make(map[string]any, len(allowed))
	for _, key := range allowed {
		v, ok := in[key]
		if !ok {
			continue
		}
		switch key {
		case "max_results":
			if n, ok := boundedInt(v, maxResultsCap); ok {
				out[key] = n
			}
		case "max_uses":
			if n, ok := boundedInt(v, maxUsesCap); ok {
				out[key] = n
			}
		case "max_content_tokens":
			if n, ok := boundedInt(v, maxContentTokensCap); ok {
				out[key] = n
			}
		case "allowed_domains", "blocked_domains":
			if list := Domains(v); len(list) > 0 {
				out[key] = list
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// boundedInt converts a JSON number to an int in [1, limit]. Non-numbers and values below 1 are
// rejected; larger values are clamped.
func boundedInt(v any, limit int) (int, bool) {
	var f float64
	switch n := v.(type) {
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	case float64:
		f = n
	case json.Number:
		var err error
		if f, err = n.Float64(); err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if math.IsNaN(f) || f < 1 {
		return 0, false
	}
	return int(min(f, float64(limit))), true
}

// Domains reads a list of host names from a JSON value ([]string or []any), keeping at most 20 valid,
// lower-cased, de-duplicated hostnames. Invalid entries are dropped.
func Domains(v any) []string {
	var raw []string
	switch l := v.(type) {
	case []string:
		raw = l
	case []any:
		for _, e := range l {
			if s, ok := e.(string); ok {
				raw = append(raw, s)
			}
		}
	default:
		return nil
	}
	var out []string
	for _, s := range raw {
		d := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
		if !validHostname(d) || slices.Contains(out, d) {
			continue
		}
		out = append(out, d)
		if len(out) == maxDomains {
			break
		}
	}
	return out
}

// validHostname accepts DNS names (letters, digits, hyphens, at least two labels) and rejects
// everything that could carry a scheme, path, port, wildcard or IP literal.
func validHostname(h string) bool {
	if len(h) == 0 || len(h) > 253 || !strings.Contains(h, ".") {
		return false
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return false
	}
	for label := range strings.SplitSeq(h, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range len(label) {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// hostedSpec builds the provider-run spec for a built-in tool, or nil.
func hostedSpec(bot chat.Bot, caps llm.Caps, toolID, hosted string) []llm.ToolSpec {
	cfg, ok := botTool(bot, toolID)
	if !ok || !hasHosted(caps, hosted) {
		return nil
	}
	return []llm.ToolSpec{{Hosted: hosted, Params: SanitizeParams(hosted, cfg.Params)}}
}

var errHostedCall = errors.New("hosted tools run at the provider and cannot be called by the daemon")

// WebSearch offers the provider's hosted web search. There is no client fallback: Orole has no search
// backend, so a model whose endpoint cannot run it simply does not get the tool.
type WebSearch struct{}

func (WebSearch) ID() string { return chat.ToolWebSearch }

func (WebSearch) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	return hostedSpec(bot, caps, chat.ToolWebSearch, llm.HostedWebSearch)
}

func (WebSearch) Call(context.Context, chat.RunContext, string, json.RawMessage) (chat.ToolResult, error) {
	return chat.ToolResult{}, errHostedCall
}

// WebFetch offers hosted page fetching, or the daemon-side fetch_url function when the model's
// endpoint cannot fetch pages itself but can call tools.
type WebFetch struct {
	// Fetcher runs the fetch_url fallback. Nil disables the fallback.
	Fetcher *Fetcher
}

func (WebFetch) ID() string { return chat.ToolWebFetch }

func (w WebFetch) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	cfg, ok := botTool(bot, chat.ToolWebFetch)
	if !ok {
		return nil
	}
	if hasHosted(caps, llm.HostedWebFetch) {
		return []llm.ToolSpec{{Hosted: llm.HostedWebFetch, Params: SanitizeParams(llm.HostedWebFetch, cfg.Params)}}
	}
	if w.Fetcher == nil || !caps.Tools {
		return nil
	}
	return []llm.ToolSpec{FetchURLSpec()}
}

func (w WebFetch) Call(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if name != FetchURLName || w.Fetcher == nil {
		return chat.ToolResult{}, errHostedCall
	}
	cfg, _ := botTool(rc.Bot, chat.ToolWebFetch)
	return w.Fetcher.Call(ctx, rc, DomainFilterFromParams(cfg.Params), args)
}

// Datetime offers the hosted clock where the endpoint has one, otherwise a client function that
// reads the daemon's clock.
type Datetime struct {
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
}

// DatetimeName is the function name of the client clock tool.
const DatetimeName = "datetime"

func (Datetime) ID() string { return chat.ToolDatetime }

func (Datetime) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if _, ok := botTool(bot, chat.ToolDatetime); !ok {
		return nil
	}
	if hasHosted(caps, llm.HostedDatetime) {
		return []llm.ToolSpec{{Hosted: llm.HostedDatetime}}
	}
	if !caps.Tools {
		return nil
	}
	return []llm.ToolSpec{{
		Name:        DatetimeName,
		Description: "Get the current date and time. Optionally pass an IANA time zone (for example \"Europe/Paris\"); the default is the user's local time zone.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string","description":"IANA time zone name, e.g. America/New_York"}},"additionalProperties":false}`),
	}}
}

func (d Datetime) Call(_ context.Context, _ chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if name != DatetimeName {
		return chat.ToolResult{}, errHostedCall
	}
	var a struct {
		Timezone string `json:"timezone"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			return chat.ToolResult{Text: "invalid arguments: " + err.Error(), IsError: true}, nil
		}
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	t := now()
	if a.Timezone != "" {
		loc, err := time.LoadLocation(a.Timezone)
		if err != nil || len(a.Timezone) > 64 {
			return chat.ToolResult{Text: "unknown time zone; use an IANA name such as Europe/Paris", IsError: true}, nil
		}
		t = t.In(loc)
	}
	zone, offset := t.Zone()
	sign := '+'
	if offset < 0 {
		sign, offset = '-', -offset
	}
	label := t.Location().String()
	if label == "Local" || label == "" {
		label = zone
	} else {
		label += " (" + zone + ")"
	}
	text := fmt.Sprintf("Current time: %s\nWeekday: %s\nTime zone: %s, UTC%c%02d:%02d",
		t.Format(time.RFC3339), t.Weekday(), label, sign, offset/3600, offset%3600/60)
	return chat.ToolResult{Text: text}, nil
}

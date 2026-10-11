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
	"slices"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Fixed options for the provider-run web tools. They are not user settings: every bot gets the same,
// and a bot's stored tool params are ignored.
//
// Search returns 5 results per call: that is OpenRouter's default for its search tool (range 1-25) and
// the figure our own research workers already use; more results mostly add tokens, since a model that
// needs more simply searches again. Fetch keeps up to 25,000 tokens of a page: Anthropic's web fetch
// documentation puts an average page (10 kB) at about 2,500 tokens and a large documentation page
// (100 kB) at about 25,000, so this reads such a page whole and truncates only book-length content.
// All domains are allowed; the daemon-side fetcher still refuses non-public addresses.
const (
	defaultMaxResults       = 5
	defaultMaxContentTokens = 25_000
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

// HostedParams returns the fixed options of a provider-run web tool, or nil for tools without any.
func HostedParams(hosted string) map[string]any {
	switch hosted {
	case llm.HostedWebSearch:
		return map[string]any{"max_results": defaultMaxResults}
	case llm.HostedWebFetch:
		return map[string]any{"max_content_tokens": defaultMaxContentTokens}
	default:
		return nil
	}
}

// hostedSpec builds the provider-run spec for a built-in tool, or nil.
func hostedSpec(bot chat.Bot, caps llm.Caps, toolID, hosted string) []llm.ToolSpec {
	if _, ok := botTool(bot, toolID); !ok || !hasHosted(caps, hosted) {
		return nil
	}
	return []llm.ToolSpec{{Hosted: hosted, Params: HostedParams(hosted)}}
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
	if _, ok := botTool(bot, chat.ToolWebFetch); !ok {
		return nil
	}
	if hasHosted(caps, llm.HostedWebFetch) {
		return []llm.ToolSpec{{Hosted: llm.HostedWebFetch, Params: HostedParams(llm.HostedWebFetch)}}
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
	return w.Fetcher.Call(ctx, rc, args)
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

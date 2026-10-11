package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func botWith(ids ...string) chat.Bot {
	var b chat.Bot
	for _, id := range ids {
		b.Config.Tools = append(b.Config.Tools, chat.BotTool{ID: id})
	}
	return b
}

func TestSpecsCapabilityMatrix(t *testing.T) {
	fetcher := &Fetcher{}
	cases := []struct {
		name string
		tool chat.Tool
		bot  chat.Bot
		caps llm.Caps
		want string // "" = no specs, "hosted:<id>", "fn:<name>"
	}{
		{"search hosted", WebSearch{}, botWith(chat.ToolWebSearch), llm.Caps{Tools: true, HostedTools: []string{llm.HostedWebSearch}}, "hosted:web_search"},
		{"search hosted without tools cap still offered", WebSearch{}, botWith(chat.ToolWebSearch), llm.Caps{HostedTools: []string{llm.HostedWebSearch}}, "hosted:web_search"},
		{"search not enabled", WebSearch{}, botWith(), llm.Caps{HostedTools: []string{llm.HostedWebSearch}}, ""},
		{"search unsupported has no fallback", WebSearch{}, botWith(chat.ToolWebSearch), llm.Caps{Tools: true}, ""},
		{"fetch hosted", WebFetch{Fetcher: fetcher}, botWith(chat.ToolWebFetch), llm.Caps{Tools: true, HostedTools: []string{llm.HostedWebFetch}}, "hosted:web_fetch"},
		{"fetch fallback", WebFetch{Fetcher: fetcher}, botWith(chat.ToolWebFetch), llm.Caps{Tools: true}, "fn:fetch_url"},
		{"fetch fallback needs tools", WebFetch{Fetcher: fetcher}, botWith(chat.ToolWebFetch), llm.Caps{}, ""},
		{"fetch fallback needs fetcher", WebFetch{}, botWith(chat.ToolWebFetch), llm.Caps{Tools: true}, ""},
		{"fetch not enabled", WebFetch{Fetcher: fetcher}, botWith(), llm.Caps{Tools: true}, ""},
		{"datetime hosted", Datetime{}, botWith(chat.ToolDatetime), llm.Caps{Tools: true, HostedTools: []string{llm.HostedDatetime}}, "hosted:datetime"},
		{"datetime client", Datetime{}, botWith(chat.ToolDatetime), llm.Caps{Tools: true}, "fn:datetime"},
		{"datetime no tools", Datetime{}, botWith(chat.ToolDatetime), llm.Caps{}, ""},
		{"datetime not enabled", Datetime{}, botWith(), llm.Caps{Tools: true}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			specs := c.tool.Specs(c.bot, c.caps)
			got := ""
			if len(specs) > 1 {
				t.Fatalf("specs = %+v", specs)
			}
			if len(specs) == 1 {
				if specs[0].Hosted != "" {
					got = "hosted:" + specs[0].Hosted
				} else {
					got = "fn:" + specs[0].Name
					if !json.Valid(specs[0].Schema) {
						t.Fatalf("schema is not JSON: %s", specs[0].Schema)
					}
				}
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestHostedToolsNeverReachCall(t *testing.T) {
	if _, err := (WebSearch{}).Call(context.Background(), chat.RunContext{}, "web_search", nil); err == nil {
		t.Fatal("WebSearch.Call must refuse")
	}
	if _, err := (WebFetch{}).Call(context.Background(), chat.RunContext{}, "web_fetch", nil); err == nil {
		t.Fatal("hosted WebFetch.Call must refuse")
	}
}

func TestHostedParamsAreFixedAndIgnoreBotSettings(t *testing.T) {
	if got := HostedParams(llm.HostedWebSearch); len(got) != 1 || got["max_results"] != 5 {
		t.Fatalf("search = %v, want only max_results 5", got)
	}
	if got := HostedParams(llm.HostedWebFetch); len(got) != 1 || got["max_content_tokens"] != 25_000 {
		t.Fatalf("fetch = %v, want only max_content_tokens 25000", got)
	}
	if HostedParams(llm.HostedDatetime) != nil {
		t.Fatal("datetime has no options")
	}
	// Params saved by older versions (limits, domain lists) never reach the provider.
	saved := map[string]any{"max_results": 99.0, "max_content_tokens": 1.0, "allowed_domains": []any{"example.com"}, "blocked_domains": []any{"bad.test"}}
	bot := chat.Bot{Config: chat.BotConfig{Tools: []chat.BotTool{{ID: chat.ToolWebSearch, Params: saved}, {ID: chat.ToolWebFetch, Params: saved}}}}
	caps := llm.Caps{HostedTools: []string{llm.HostedWebSearch, llm.HostedWebFetch}}
	search := WebSearch{}.Specs(bot, caps)
	if len(search) != 1 || !reflect.DeepEqual(search[0].Params, map[string]any{"max_results": 5}) {
		t.Fatalf("search specs = %+v", search)
	}
	fetch := WebFetch{}.Specs(bot, caps)
	if len(fetch) != 1 || !reflect.DeepEqual(fetch[0].Params, map[string]any{"max_content_tokens": 25_000}) {
		t.Fatalf("fetch specs = %+v", fetch)
	}
}

func TestDatetimeCall(t *testing.T) {
	d := Datetime{Now: func() time.Time { return time.Date(2026, 3, 4, 12, 30, 0, 0, time.UTC) }}
	res, err := d.Call(context.Background(), chat.RunContext{}, "datetime", json.RawMessage(`{"timezone":"UTC"}`))
	if err != nil || res.IsError {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	for _, want := range []string{"2026-03-04T12:30:00Z", "Wednesday", "UTC+00:00"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("%q missing from %q", want, res.Text)
		}
	}
	res, _ = d.Call(context.Background(), chat.RunContext{}, "datetime", json.RawMessage(`{"timezone":"Nowhere/Land"}`))
	if !res.IsError {
		t.Fatalf("unknown zone accepted: %+v", res)
	}
	res, _ = d.Call(context.Background(), chat.RunContext{}, "datetime", nil)
	if res.IsError || !strings.Contains(res.Text, "Current time:") {
		t.Fatalf("local time: %+v", res)
	}
}

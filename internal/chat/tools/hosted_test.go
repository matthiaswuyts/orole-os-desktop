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

func TestSanitizeParams(t *testing.T) {
	long := make([]any, 30)
	for i := range long {
		long[i] = "site" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".example.com"
	}
	in := map[string]any{
		"max_results":        float64(500),
		"max_uses":           json.Number("3"),
		"max_content_tokens": 10_000_000,
		"allowed_domains":    append([]any{"Example.COM", "https://evil.example/path", "a b.com", "localhost", "10.0.0.1", "ok-1.example.org.", 7}, long...),
		"blocked_domains":    []string{"bad.test", "bad.test", "-x.com"},
		"secret":             "nope",
	}
	got := SanitizeParams(llm.HostedWebSearch, in)
	if got["max_results"] != 10 || got["max_uses"] != 3 {
		t.Fatalf("numbers = %v", got)
	}
	if _, ok := got["max_content_tokens"]; ok {
		t.Fatal("max_content_tokens is a fetch parameter")
	}
	if _, ok := got["secret"]; ok {
		t.Fatal("unknown key survived")
	}
	allowed := got["allowed_domains"].([]string)
	if len(allowed) != 20 || allowed[0] != "example.com" || allowed[1] != "ok-1.example.org" {
		t.Fatalf("allowed = %v", allowed)
	}
	if !reflect.DeepEqual(got["blocked_domains"], []string{"bad.test"}) {
		t.Fatalf("blocked = %v", got["blocked_domains"])
	}

	fetch := SanitizeParams(llm.HostedWebFetch, in)
	if fetch["max_content_tokens"] != 100000 {
		t.Fatalf("fetch = %v", fetch)
	}
	if _, ok := fetch["max_results"]; ok {
		t.Fatal("max_results is a search parameter")
	}
	if SanitizeParams(llm.HostedWebSearch, map[string]any{"max_results": "ten", "max_uses": -1.0}) != nil {
		t.Fatal("invalid values must be dropped")
	}
	if SanitizeParams(llm.HostedDatetime, in) != nil || SanitizeParams(llm.HostedWebSearch, nil) != nil {
		t.Fatal("expected nil")
	}
}

func TestHostedSpecCarriesSanitizedParams(t *testing.T) {
	bot := chat.Bot{Config: chat.BotConfig{Tools: []chat.BotTool{{ID: chat.ToolWebSearch, Params: map[string]any{"max_results": 99.0, "x": 1}}}}}
	specs := WebSearch{}.Specs(bot, llm.Caps{HostedTools: []string{llm.HostedWebSearch}})
	if len(specs) != 1 || specs[0].Params["max_results"] != 10 || len(specs[0].Params) != 1 {
		t.Fatalf("specs = %+v", specs)
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

func TestDomainFilter(t *testing.T) {
	f := DomainFilter{Allowed: []string{"example.com"}, Blocked: []string{"bad.example.com"}}
	for host, want := range map[string]bool{
		"example.com": true, "www.example.com": true, "EXAMPLE.com.": true,
		"bad.example.com": false, "x.bad.example.com": false, "notexample.com": false, "evil.com": false,
	} {
		if got := f.Permits(host); got != want {
			t.Errorf("Permits(%q) = %v, want %v", host, got, want)
		}
	}
	if !(DomainFilter{}).Permits("anything.test") {
		t.Error("empty filter must permit")
	}
}

package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// The tests in this package are deliberately not parallel: the leak check counts goroutines.

const testKey = "sk-or-v1-0123456789abcdefSECRET1234"

func newTestProvider(t *testing.T, h http.HandlerFunc) (llm.Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := New(llm.Config{APIKey: testKey, BaseURL: srv.URL + "/api/v1", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return p, srv
}

func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, e := range events {
		fmt.Fprintf(w, "data: %s\n\n", e)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func collect(t *testing.T, ch <-chan llm.Delta) []llm.Delta {
	t.Helper()
	var out []llm.Delta
	timeout := time.After(5 * time.Second)
	for {
		select {
		case d, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, d)
		case <-timeout:
			t.Fatalf("stream did not finish; got %+v", out)
		}
	}
}

// capture runs a Stream against a server that records the request body and answers with a bare stop.
func capture(t *testing.T, req llm.Request) (map[string]any, http.Header, string) {
	t.Helper()
	var (
		body []byte
		hdr  http.Header
		path string
	)
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		hdr, path = r.Header.Clone(), r.URL.Path
		sse(w, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`, `[DONE]`)
	})
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("request body is not JSON: %v\n%s", err, body)
	}
	return m, hdr, path
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%#v is not an object", v)
	}
	return m
}

func asList(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("%#v is not an array", v)
	}
	return l
}

func TestRequestHeadersAndHostedTools(t *testing.T) {
	req := llm.Request{
		Model:  "anthropic/claude-sonnet-4",
		System: "be brief",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: "hi"}}},
		},
		Tools: []llm.ToolSpec{
			{Hosted: llm.HostedWebSearch, Params: map[string]any{
				"max_results": 3, "allowed_domains": []string{"arxiv.org"}, "excluded_domains": []string{"reddit.com"},
				"max_content_tokens": 99, // not a web_search option: dropped
			}},
			{Hosted: llm.HostedWebFetch, Params: map[string]any{
				"max_uses": 2, "max_content_tokens": 5000,
				"allowed_domains": []string{"docs.example.com"}, "blocked_domains": []string{"private.example.com"},
				"max_results": 3, // not a web_fetch option: dropped
			}},
			{Hosted: llm.HostedDatetime},
			{Name: "get_weather", Description: "weather", Schema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)},
		},
		MaxHostedToolCalls: 4,
		MaxOutputTokens:    256,
		Reasoning:          "high",
	}
	body, hdr, path := capture(t, req)

	if path != "/api/v1/chat/completions" {
		t.Errorf("path = %s", path)
	}
	if got := hdr.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization = %q", got)
	}
	if hdr.Get("X-Title") != "Orole OS" || !strings.HasPrefix(hdr.Get("HTTP-Referer"), "https://") {
		t.Errorf("attribution headers = %q / %q", hdr.Get("X-Title"), hdr.Get("HTTP-Referer"))
	}
	if body["stream"] != true || body["model"] != "anthropic/claude-sonnet-4" {
		t.Errorf("stream/model = %v / %v", body["stream"], body["model"])
	}
	if strings.Contains(fmt.Sprint(body["model"]), ":online") {
		t.Error(":online model suffix is deprecated")
	}
	if _, ok := body["plugins"]; ok {
		t.Errorf("plugins sent without a file part: %v", body["plugins"])
	}
	if body["max_tool_calls"] != float64(4) || body["max_tokens"] != float64(256) {
		t.Errorf("max_tool_calls/max_tokens = %v / %v", body["max_tool_calls"], body["max_tokens"])
	}
	if r := asMap(t, body["reasoning"]); r["effort"] != "high" {
		t.Errorf("reasoning = %v", r)
	}

	msgs := asList(t, body["messages"])
	if m := asMap(t, msgs[0]); m["role"] != "system" || m["content"] != "be brief" {
		t.Errorf("system message = %v", m)
	}
	if m := asMap(t, msgs[1]); m["role"] != "user" || m["content"] != "hi" {
		t.Errorf("user message = %v", m)
	}

	tools := asList(t, body["tools"])
	if len(tools) != 4 {
		t.Fatalf("tools = %v", tools)
	}
	ws := asMap(t, tools[0])
	if ws["type"] != "openrouter:web_search" {
		t.Errorf("web search type = %v", ws["type"])
	}
	wsp := asMap(t, ws["parameters"])
	if wsp["max_results"] != float64(3) || fmt.Sprint(wsp["allowed_domains"]) != "[arxiv.org]" || fmt.Sprint(wsp["excluded_domains"]) != "[reddit.com]" {
		t.Errorf("web_search parameters = %v", wsp)
	}
	if _, ok := wsp["max_content_tokens"]; ok {
		t.Errorf("web_search got a web_fetch option: %v", wsp)
	}
	wf := asMap(t, tools[1])
	if wf["type"] != "openrouter:web_fetch" {
		t.Errorf("web fetch type = %v", wf["type"])
	}
	wfp := asMap(t, wf["parameters"])
	if wfp["max_uses"] != float64(2) || wfp["max_content_tokens"] != float64(5000) ||
		fmt.Sprint(wfp["allowed_domains"]) != "[docs.example.com]" || fmt.Sprint(wfp["blocked_domains"]) != "[private.example.com]" {
		t.Errorf("web_fetch parameters = %v", wfp)
	}
	if _, ok := wfp["max_results"]; ok {
		t.Errorf("web_fetch got a web_search option: %v", wfp)
	}
	if dt := asMap(t, tools[2]); dt["type"] != "openrouter:datetime" {
		t.Errorf("datetime type = %v", dt["type"])
	} else if _, ok := dt["parameters"]; ok {
		t.Errorf("datetime without options has parameters: %v", dt)
	}
	fn := asMap(t, tools[3])
	if fn["type"] != "function" {
		t.Fatalf("client tool = %v", fn)
	}
	if f := asMap(t, fn["function"]); f["name"] != "get_weather" || asMap(t, f["parameters"])["type"] != "object" {
		t.Errorf("function = %v", f)
	}
}

func TestMaxToolCallsOnlyWithHostedTools(t *testing.T) {
	body, _, _ := capture(t, llm.Request{
		Model:              "openai/gpt-5",
		Messages:           []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: "hi"}}}},
		Tools:              []llm.ToolSpec{{Name: "t"}},
		MaxHostedToolCalls: 4,
	})
	if _, ok := body["max_tool_calls"]; ok {
		t.Errorf("max_tool_calls sent without a hosted tool: %v", body["max_tool_calls"])
	}
	if _, ok := body["reasoning"]; ok {
		t.Errorf("reasoning sent without a level: %v", body["reasoning"])
	}
	tool := asMap(t, asList(t, body["tools"])[0])
	if p := asMap(t, tool["function"])["parameters"]; p == nil {
		t.Error("client tool without a schema must still send an object schema")
	}
}

func TestPartsMapping(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	pdf := []byte("%PDF-1.4 test")
	user := func(parts ...llm.Part) llm.Request {
		return llm.Request{Model: "openai/gpt-5", Messages: []llm.Message{{Role: llm.RoleUser, Parts: parts}}}
	}

	// Image only: image_url data URL, still no plugin.
	body, _, _ := capture(t, user(
		llm.Part{Kind: llm.PartText, Text: "what is this"},
		llm.Part{Kind: llm.PartImage, MIME: "image/png", Data: png},
	))
	if _, ok := body["plugins"]; ok {
		t.Errorf("plugins sent for an image-only request: %v", body["plugins"])
	}
	content := asList(t, asMap(t, asList(t, body["messages"])[0])["content"])
	if p := asMap(t, content[0]); p["type"] != "text" || p["text"] != "what is this" {
		t.Errorf("text part = %v", p)
	}
	img := asMap(t, content[1])
	if img["type"] != "image_url" || !strings.HasPrefix(asMap(t, img["image_url"])["url"].(string), "data:image/png;base64,iVBORw0KGgo") {
		t.Errorf("image part = %v", img)
	}

	// File: {type:file,file:{filename,file_data}} plus the file-parser plugin with the default engine.
	body, _, _ = capture(t, user(
		llm.Part{Kind: llm.PartText, Text: "summarise"},
		llm.Part{Kind: llm.PartFile, Name: "report.pdf", Data: pdf},
	))
	content = asList(t, asMap(t, asList(t, body["messages"])[0])["content"])
	file := asMap(t, content[1])
	fp := asMap(t, file["file"])
	if file["type"] != "file" || fp["filename"] != "report.pdf" || !strings.HasPrefix(fp["file_data"].(string), "data:application/pdf;base64,") {
		t.Errorf("file part = %v", file)
	}
	plugins := asList(t, body["plugins"])
	if len(plugins) != 1 {
		t.Fatalf("plugins = %v", plugins)
	}
	pl := asMap(t, plugins[0])
	if pl["id"] != "file-parser" || asMap(t, pl["pdf"])["engine"] != "cloudflare-ai" {
		t.Errorf("plugin = %v", pl)
	}

	// Explicit engine; an unknown value falls back to the default instead of failing the request.
	for engine, want := range map[string]string{"mistral-ocr": "mistral-ocr", "native": "native", "bogus": "cloudflare-ai"} {
		req := user(llm.Part{Kind: llm.PartFile, Data: pdf})
		req.PDFEngine = engine
		body, _, _ = capture(t, req)
		got := asMap(t, asMap(t, asList(t, body["plugins"])[0])["pdf"])["engine"]
		if got != want {
			t.Errorf("PDFEngine %q -> %v, want %s", engine, got, want)
		}
	}
}

func TestToolRoundTripMessages(t *testing.T) {
	body, _, _ := capture(t, llm.Request{
		Model: "openai/gpt-5",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: "weather?"}}},
			{Role: llm.RoleAssistant, Parts: []llm.Part{
				{Kind: llm.PartText, Text: "checking"},
				{Kind: llm.PartToolCall, ToolCallID: "call_1", ToolName: "get_weather", Args: json.RawMessage(`{"city":"Paris"}`)},
				{Kind: llm.PartToolCall, ToolCallID: "call_2", ToolName: "noargs"},
			}},
			{Role: llm.RoleTool, Parts: []llm.Part{
				{Kind: llm.PartToolResult, ToolCallID: "call_1", Text: "sunny"},
				{Kind: llm.PartToolResult, ToolCallID: "call_2", Text: "boom", IsError: true},
			}},
		},
	})
	msgs := asList(t, body["messages"])
	if len(msgs) != 4 {
		t.Fatalf("messages = %v", msgs)
	}
	as := asMap(t, msgs[1])
	calls := asList(t, as["tool_calls"])
	if as["role"] != "assistant" || as["content"] != "checking" || len(calls) != 2 {
		t.Fatalf("assistant = %v", as)
	}
	c1 := asMap(t, calls[0])
	if c1["id"] != "call_1" || c1["type"] != "function" || asMap(t, c1["function"])["arguments"] != `{"city":"Paris"}` {
		t.Errorf("call 1 = %v", c1)
	}
	if asMap(t, asMap(t, calls[1])["function"])["arguments"] != "{}" {
		t.Errorf("empty arguments must be {}: %v", calls[1])
	}
	if m := asMap(t, msgs[2]); m["role"] != "tool" || m["tool_call_id"] != "call_1" || m["content"] != "sunny" {
		t.Errorf("tool result 1 = %v", m)
	}
	if m := asMap(t, msgs[3]); m["tool_call_id"] != "call_2" {
		t.Errorf("tool result 2 = %v", m)
	}
}

func TestStreamTextReasoningUsageAndServerTools(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"id":"gen-1","choices":[{"delta":{"role":"assistant","content":""}}]}`,
			`{"choices":[{"delta":{"reasoning":"let me "}}]}`,
			`{"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","text":"think"}]}}]}`,
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"cost":0.0012,`+
				`"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":2},`+
				`"server_tool_use":{"web_search_requests":2,"web_fetch_requests":1}}}`,
			`[DONE]`,
		)
	})
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: "hi"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	ds := collect(t, ch)

	var text, reasoning strings.Builder
	var usage *llm.Usage
	var servers []llm.ServerTool
	var stops []llm.StopReason
	for _, d := range ds {
		switch d.Kind {
		case llm.DeltaText:
			text.WriteString(d.Text)
		case llm.DeltaReasoning:
			reasoning.WriteString(d.Text)
		case llm.DeltaUsage:
			usage = d.Usage
		case llm.DeltaServer:
			servers = append(servers, *d.Server)
		case llm.DeltaStop:
			stops = append(stops, d.Stop)
		case llm.DeltaError:
			t.Fatalf("unexpected error: %+v", d.Err)
		}
	}
	if text.String() != "Hello" || reasoning.String() != "let me think" {
		t.Errorf("text=%q reasoning=%q", text.String(), reasoning.String())
	}
	if usage == nil || usage.InputTokens == nil || *usage.InputTokens != 10 || *usage.OutputTokens != 5 ||
		*usage.CachedTokens != 4 || *usage.ReasoningTokens != 2 || usage.CostUSD == nil || *usage.CostUSD != 0.0012 {
		t.Errorf("usage = %+v", usage)
	}
	if len(servers) != 2 || servers[0].Tool != llm.HostedWebSearch || servers[0].Status != "done" || servers[0].Detail != "2 calls" ||
		servers[1].Tool != llm.HostedWebFetch || servers[1].Detail != "1 call" {
		t.Errorf("server tools = %+v", servers)
	}
	if len(stops) != 1 || stops[0] != llm.StopEnd || ds[len(ds)-1].Kind != llm.DeltaStop {
		t.Errorf("stop = %v, last = %v", stops, ds[len(ds)-1].Kind)
	}
}

func TestStreamUsageIsNilWhenNotReported(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"x"},"finish_reason":"length"}]}`, `[DONE]`)
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "m"})
	ds := collect(t, ch)
	for _, d := range ds {
		if d.Kind == llm.DeltaUsage {
			t.Fatalf("usage invented: %+v", d.Usage)
		}
	}
	if last := ds[len(ds)-1]; last.Kind != llm.DeltaStop || last.Stop != llm.StopLength {
		t.Errorf("last = %+v", last)
	}
}

func TestStreamToolCallAssembly(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ci"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"noargs","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Par"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"is\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"cost":0.5}}`,
			`[DONE]`,
		)
	})
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	ds := collect(t, ch)
	var calls []llm.ToolCall
	var order []llm.DeltaKind
	for _, d := range ds {
		order = append(order, d.Kind)
		if d.Kind == llm.DeltaToolCall {
			calls = append(calls, *d.Call)
		}
		if d.Kind == llm.DeltaError {
			t.Fatalf("error: %+v", d.Err)
		}
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %+v (%v)", calls, order)
	}
	if calls[0].ID != "call_a" || calls[0].Name != "get_weather" || string(calls[0].Args) != `{"city":"Paris"}` {
		t.Errorf("call 0 = %+v (%s)", calls[0], calls[0].Args)
	}
	if calls[1].ID != "call_b" || calls[1].Name != "noargs" || string(calls[1].Args) != `{}` {
		t.Errorf("call 1 = %+v (%s)", calls[1], calls[1].Args)
	}
	if last := ds[len(ds)-1]; last.Kind != llm.DeltaStop || last.Stop != llm.StopToolCalls {
		t.Errorf("last = %+v", last)
	}
	var usage bool
	for _, d := range ds {
		if d.Kind == llm.DeltaUsage && d.Usage.CostUSD != nil && *d.Usage.CostUSD == 0.5 {
			usage = true
		}
	}
	if !usage {
		t.Error("usage chunk after finish_reason was lost")
	}
}

func TestStreamMalformedToolArgumentsFail(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"bad","arguments":"{\"a\":"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`)
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "m"})
	ds := collect(t, ch)
	last := ds[len(ds)-1]
	if last.Kind != llm.DeltaError || last.Err == nil {
		t.Fatalf("last = %+v, want a terminal error", last)
	}
	for _, d := range ds {
		if d.Kind == llm.DeltaToolCall || d.Kind == llm.DeltaStop {
			t.Errorf("unexpected %s after malformed arguments", d.Kind)
		}
	}
}

func TestStreamCitations(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		long := strings.Repeat("é", 2000)
		sse(w,
			`{"choices":[{"delta":{"content":"Go 1.26 is out"}}]}`,
			`{"choices":[{"delta":{"annotations":[`+
				`{"type":"url_citation","url_citation":{"url":"https://go.dev/doc/go1.26","title":"Go 1.26 Release Notes","content":"`+long+`","start_index":0,"end_index":5}},`+
				`{"type":"url_citation","url_citation":{"url":"https://go.dev/doc/go1.26","title":"duplicate"}},`+
				`{"type":"file_citation","url_citation":{"url":"https://ignored.example"}},`+
				`{"type":"url_citation","url_citation":{"url":""}}]}}]}`,
			`{"choices":[{"delta":{"annotations":[{"type":"url_citation","url_citation":{"url":"https://example.org/a","title":"A"}}]},"finish_reason":"stop"}]}`,
			`[DONE]`)
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "m"})
	var cites []llm.Citation
	for _, d := range collect(t, ch) {
		if d.Kind == llm.DeltaCitation {
			cites = append(cites, *d.Citation)
		}
	}
	if len(cites) != 2 {
		t.Fatalf("citations = %+v", cites)
	}
	if cites[0].URL != "https://go.dev/doc/go1.26" || cites[0].Title != "Go 1.26 Release Notes" {
		t.Errorf("citation 0 = %+v", cites[0])
	}
	if n := len(cites[0].Snippet); n > snippetMax+len("…") || !strings.HasSuffix(cites[0].Snippet, "…") {
		t.Errorf("snippet length = %d, want truncated to %d", n, snippetMax)
	}
	if cites[1].URL != "https://example.org/a" || cites[1].Title != "A" {
		t.Errorf("citation 1 = %+v", cites[1])
	}
}

func TestStreamHTTPErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		hdr    map[string]string
		body   string
		code   llm.ErrorCode
		retry  time.Duration
	}{
		{"auth", 401, nil, `{"error":{"code":401,"message":"No auth credentials found for ` + testKey + `"}}`, llm.ErrAuth, 0},
		{"quota", 402, nil, `{"error":{"code":402,"message":"Insufficient credits. Add more using https://openrouter.ai/credits"}}`, llm.ErrQuota, 0},
		{"rate limit", 429, map[string]string{"Retry-After": "7"}, `{"error":{"code":429,"message":"Rate limit exceeded: free-models-per-min"}}`, llm.ErrRateLimit, 7 * time.Second},
		{"context length", 400, nil, `{"error":{"code":400,"message":"This endpoint's maximum context length is 8192 tokens. However, you requested about 20000 tokens"}}`, llm.ErrContextLength, 0},
		{"moderation", 403, nil, `{"error":{"code":403,"message":"Your input was flagged","metadata":{"reasons":["violence"]}}}`, llm.ErrFilter, 0},
		{"bad request", 400, nil, `{"error":{"code":400,"message":"model does not support tools"}}`, llm.ErrBadRequest, 0},
		{"server", 502, nil, `<html>bad gateway</html>`, llm.ErrServer, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.hdr {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			ch, err := p.Stream(context.Background(), llm.Request{Model: "m"})
			if ch != nil {
				t.Fatalf("channel returned with error status: %v", ch)
			}
			le := llm.AsError(err)
			if le.Code != tc.code || le.Status != tc.status {
				t.Fatalf("error = %+v, want %s/%d", le, tc.code, tc.status)
			}
			if tc.retry > 0 && le.RetryAfter != tc.retry {
				t.Errorf("RetryAfter = %v, want %v", le.RetryAfter, tc.retry)
			}
			if strings.Contains(err.Error(), testKey) || strings.Contains(le.Message, "0123456789abcdef") {
				t.Errorf("error leaks the key: %v", err)
			}
		})
	}
}

func TestStreamMidStreamError(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"content":"partial"}}]}`,
			`{"error":{"code":429,"message":"Provider rate limited"},"choices":[{"delta":{"content":""},"finish_reason":"error"}]}`)
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "m"})
	ds := collect(t, ch)
	last := ds[len(ds)-1]
	if last.Kind != llm.DeltaError || last.Err.Code != llm.ErrRateLimit {
		t.Fatalf("last = %+v", last)
	}
	if ds[0].Kind != llm.DeltaText || ds[0].Text != "partial" {
		t.Errorf("first = %+v", ds[0])
	}
}

func TestStreamTruncatedStreamIsAnError(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"half"}}]}`) // connection ends: no finish_reason, no [DONE]
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "m"})
	ds := collect(t, ch)
	last := ds[len(ds)-1]
	if last.Kind != llm.DeltaError || last.Err.Code != llm.ErrNetwork {
		t.Fatalf("last = %+v, want a network error rather than a clean stop", last)
	}
}

func TestStreamIgnoresKeepAliveComments(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "m"})
	ds := collect(t, ch)
	if len(ds) != 2 || ds[0].Text != "ok" || ds[1].Kind != llm.DeltaStop {
		t.Fatalf("deltas = %+v", ds)
	}
}

func TestStreamRequiresModel(t *testing.T) {
	p, _ := newTestProvider(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent without a model") })
	if _, err := p.Stream(context.Background(), llm.Request{}); llm.AsError(err).Code != llm.ErrBadRequest {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamCancelNoGoroutineLeak(t *testing.T) {
	// Let goroutines of earlier tests (idle connections of closed servers) exit first.
	waitGoroutines(t, -1)
	base := runtime.NumGoroutine()

	handlerDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		sse(w, `{"choices":[{"delta":{"content":"first"}}]}`)
		<-r.Context().Done() // the client going away must reach the server
	}))
	tr := &http.Transport{}
	p, err := New(llm.Config{APIKey: testKey, BaseURL: srv.URL, HTTP: &http.Client{Transport: tr}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-ch:
		if d.Kind != llm.DeltaText || d.Text != "first" {
			t.Fatalf("first delta = %+v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no first delta")
	}
	cancel()

	deadline := time.After(3 * time.Second)
	for closed := false; !closed; {
		select {
		case d, ok := <-ch:
			if !ok {
				closed = true
			} else if d.Kind == llm.DeltaError && d.Err.Code != llm.ErrCanceled {
				t.Fatalf("error after cancel = %+v", d.Err)
			}
		case <-deadline:
			t.Fatal("channel not closed after cancel")
		}
	}
	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("server handler still running: connection was not closed")
	}
	srv.Close()
	tr.CloseIdleConnections()
	waitGoroutines(t, base)
}

// waitGoroutines waits until the goroutine count is at most limit (limit < 0: until stable).
func waitGoroutines(t *testing.T, limit int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	last := -1
	for time.Now().Before(deadline) {
		n := runtime.NumGoroutine()
		if limit >= 0 && n <= limit {
			return
		}
		if limit < 0 && n == last {
			return
		}
		last = n
		time.Sleep(50 * time.Millisecond)
	}
	if limit >= 0 {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines: %d, want <= %d\n%s", runtime.NumGoroutine(), limit, buf[:runtime.Stack(buf, true)])
	}
}

func TestStreamConsumerStopsReading(t *testing.T) {
	// A consumer that cancels without draining must not block the producer forever.
	waitGoroutines(t, -1)
	base := runtime.NumGoroutine()
	var events []string
	for range 50 {
		events = append(events, `{"choices":[{"delta":{"content":"x"}}]}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { sse(w, events...) }))
	tr := &http.Transport{}
	p, _ := New(llm.Config{APIKey: testKey, BaseURL: srv.URL, HTTP: &http.Client{Transport: tr}})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	<-ch
	cancel() // never read again
	srv.Close()
	tr.CloseIdleConnections()
	waitGoroutines(t, base)
}

const catalogue = `{"data":[
 {"id":"anthropic/claude-sonnet-4","name":"Anthropic: Claude Sonnet 4","context_length":200000,
  "architecture":{"input_modalities":["text","image","file"],"output_modalities":["text"]},
  "supported_parameters":["tools","tool_choice","reasoning","max_tokens"]},
 {"id":"openai/gpt-5","name":"OpenAI: GPT-5","context_length":400000,
  "architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},
  "supported_parameters":["tools","reasoning","reasoning_effort"],
  "reasoning":{"supported_efforts":["high","low","medium","minimal"]}},
 {"id":"meta/plain","name":"","context_length":8192,
  "architecture":{"input_modalities":["text"],"output_modalities":["text"]},
  "supported_parameters":["max_tokens"]},
 {"id":"google/imagen","name":"Imagen","context_length":0,
  "architecture":{"input_modalities":["text"],"output_modalities":["image"]},
  "supported_parameters":[]}
]}`

func TestModelsCatalogue(t *testing.T) {
	var gotAuth, gotPath string
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		io.WriteString(w, catalogue)
	})
	ms, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/models" || gotAuth != "Bearer "+testKey {
		t.Errorf("path=%s auth=%q", gotPath, gotAuth)
	}
	if len(ms) != 3 {
		t.Fatalf("models = %+v (image-only output must be dropped)", ms)
	}
	sonnet, gpt, plain := ms[0], ms[1], ms[2]
	if sonnet.Name != "Anthropic: Claude Sonnet 4" || !sonnet.Caps.Tools || !sonnet.Caps.Vision || !sonnet.Caps.PDF ||
		!sonnet.Caps.Reasoning || sonnet.Caps.ContextWindow != 200000 {
		t.Errorf("sonnet = %+v", sonnet)
	}
	if fmt.Sprint(sonnet.Reasoning) != "[low medium high]" {
		t.Errorf("default reasoning levels = %v", sonnet.Reasoning)
	}
	if fmt.Sprint(gpt.Reasoning) != "[minimal low medium high]" {
		t.Errorf("supported_efforts must be ordered low to high: %v", gpt.Reasoning)
	}
	if plain.Name != "meta/plain" || plain.Caps.Tools || plain.Caps.Vision || plain.Caps.Reasoning || len(plain.Reasoning) != 0 {
		t.Errorf("plain = %+v", plain)
	}
	for _, m := range ms { // server tools run on OpenRouter, so every model gets them
		if fmt.Sprint(m.Caps.HostedTools) != "[web_search web_fetch datetime]" {
			t.Errorf("%s hosted tools = %v", m.ID, m.Caps.HostedTools)
		}
		if m.Caps.DeepResearch {
			t.Errorf("%s marked deep research", m.ID)
		}
	}
}

func TestValidate(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/key" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":401,"message":"User not found."}}`)
			return
		}
		io.WriteString(w, `{"data":{"label":"sk-or-v1-012...234","limit":10,"limit_remaining":7.25,"usage":2.75,"is_free_tier":false}}`)
	})
	acc, err := p.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acc.Limit == nil || *acc.Limit != 10 || acc.Remaining == nil || *acc.Remaining != 7.25 || acc.Label == "" {
		t.Errorf("account = %+v", acc)
	}
	if b, _ := json.Marshal(acc); strings.Contains(string(b), testKey) {
		t.Errorf("account leaks the key: %s", b)
	}

	bad, _ := New(llm.Config{APIKey: "sk-or-v1-wrongwrong", BaseURL: strings.TrimSuffix(p.(*Provider).base, "/api/v1") + "/api/v1", HTTP: p.(*Provider).http})
	if _, err := bad.Validate(context.Background()); llm.AsError(err).Code != llm.ErrAuth {
		t.Fatalf("bad key: %v", err)
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New(llm.Config{APIKey: "  "}); err == nil {
		t.Fatal("blank key accepted")
	}
}

func TestAuthURLAndExchange(t *testing.T) {
	verifier, challenge, err := llm.NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(BuildAuthURL(challenge))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme+"://"+u.Host+u.Path != "https://openrouter.ai/auth" {
		t.Errorf("auth url = %s", u)
	}
	if q.Get("code_challenge") != challenge || q.Get("code_challenge_method") != "S256" || q.Get("key_label") != "Orole OS" {
		t.Errorf("query = %v", q)
	}
	if q.Has("callback_url") {
		t.Error("headless flow must not send callback_url")
	}
	if strings.Contains(u.String(), verifier) {
		t.Error("auth url contains the verifier")
	}
	if !strings.Contains(u.RawQuery, "key_label=Orole%20OS") {
		t.Errorf("space must be %%20, got %s", u.RawQuery)
	}

	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/keys" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		if got["code"] != "the-code" {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":{"code":403,"message":"Invalid code the-bad-code"}}`)
			return
		}
		io.WriteString(w, `{"key":"`+testKey+`","user_id":"user_1"}`)
	}))
	defer srv.Close()

	key, err := Exchange(context.Background(), srv.Client(), srv.URL+"/api/v1", "the-code", verifier)
	if err != nil || key != testKey {
		t.Fatalf("Exchange = %q, %v", key, err)
	}
	if got["code_verifier"] != verifier || got["code_challenge_method"] != "S256" {
		t.Errorf("exchange body = %v", got)
	}
	_, err = Exchange(context.Background(), srv.Client(), srv.URL+"/api/v1", "the-bad-code", verifier)
	if le := llm.AsError(err); err == nil || le.Code != llm.ErrAuth {
		t.Fatalf("bad code: %v", err)
	} else if strings.Contains(le.Message, "the-bad-code") || strings.Contains(le.Message, verifier) {
		t.Errorf("error echoes the code or verifier: %q", le.Message)
	}
}

func TestDescriptor(t *testing.T) {
	d := Descriptor()
	if d.ID != "openrouter" || d.Kind != "builtin" || d.OAuth == nil || d.New == nil {
		t.Fatalf("descriptor = %+v", d)
	}
}

func TestKeyNeverInDeltasOrErrors(t *testing.T) {
	// A hostile or buggy gateway that echoes the Authorization header everywhere.
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		echo := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.URL.Query().Get("fail") != "" || strings.Contains(r.URL.Path, "models") {
			w.WriteHeader(500)
			io.WriteString(w, `{"error":{"message":"oops `+echo+`"}}`)
			return
		}
		sse(w, `{"error":{"code":500,"message":"upstream said `+echo+`"}}`)
	})
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	ds := collect(t, ch)
	b, _ := json.Marshal(ds)
	if strings.Contains(string(b), "SECRET1234") {
		t.Errorf("stream error leaks the key: %s", b)
	}
	_, err = p.Models(context.Background())
	if err == nil || strings.Contains(err.Error(), "SECRET1234") {
		t.Errorf("Models error = %v", err)
	}
}

func TestLive(t *testing.T) {
	if os.Getenv("OROLE_OPENROUTER_LIVE") != "1" {
		t.Skip("set OROLE_OPENROUTER_LIVE=1 and OPENROUTER_API_KEY to run against the real API")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY is not set")
	}
	model := os.Getenv("OROLE_OPENROUTER_MODEL")
	if model == "" {
		model = "openai/gpt-4o-mini"
	}
	p, err := New(llm.Config{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	acc, err := p.Validate(ctx)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	t.Logf("key label %q", acc.Label)
	ms, err := p.Models(ctx)
	if err != nil || len(ms) == 0 {
		t.Fatalf("Models: %d, %v", len(ms), err)
	}
	ch, err := p.Stream(ctx, llm.Request{
		Model:           model,
		Messages:        []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: "Reply with the single word: ok"}}}},
		MaxOutputTokens: 16,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	var last llm.Delta
	for d := range ch {
		if d.Kind == llm.DeltaText {
			text.WriteString(d.Text)
		}
		if d.Kind == llm.DeltaUsage {
			t.Logf("usage: %+v", d.Usage)
		}
		last = d
	}
	if last.Kind != llm.DeltaStop {
		t.Fatalf("stream ended with %+v", last)
	}
	if strings.TrimSpace(text.String()) == "" {
		t.Error("empty answer")
	}
}

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// The tests in this package are deliberately not parallel: the leak checks count goroutines.

const testKey = "sk-proj-0123456789abcdefSECRET1234"

func newTestProvider(t *testing.T, h http.HandlerFunc) llm.Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := New(llm.Config{APIKey: testKey, BaseURL: srv.URL + "/v1", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// sse writes events as "event: <type>\ndata: <json>"; each event JSON carries its own type.
func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, e := range events {
		var head struct {
			Type string `json:"type"`
		}
		json.Unmarshal([]byte(e), &head)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", head.Type, e)
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

func capture(t *testing.T, req llm.Request) (map[string]any, http.Header, string) {
	t.Helper()
	var (
		body []byte
		hdr  http.Header
		path string
	)
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		hdr, path = r.Header.Clone(), r.URL.Path
		sse(w, `{"type":"response.completed","response":{"status":"completed"}}`)
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

func userText(s string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: s}}}
}

func TestRequestShaping(t *testing.T) {
	body, hdr, path := capture(t, llm.Request{
		Model:    "gpt-5.5",
		System:   "be brief",
		Messages: []llm.Message{userText("hi")},
		Tools: []llm.ToolSpec{
			{Hosted: llm.HostedWebSearch, Params: map[string]any{
				"allowed_domains": []string{"openai.com"}, "excluded_domains": []string{"reddit.com"}, "search_context_size": "low",
			}},
			{Hosted: llm.HostedWebFetch},
			{Hosted: llm.HostedDatetime},
			{Name: "get_weather", Description: "weather", Schema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)},
		},
		MaxHostedToolCalls: 3,
		MaxOutputTokens:    512,
		Reasoning:          "high",
	})
	if path != "/v1/responses" {
		t.Errorf("path = %s", path)
	}
	if hdr.Get("Authorization") != "Bearer "+testKey {
		t.Errorf("Authorization = %q", hdr.Get("Authorization"))
	}
	if body["stream"] != true || body["store"] != false || body["model"] != "gpt-5.5" || body["instructions"] != "be brief" {
		t.Errorf("stream/store/model/instructions = %v/%v/%v/%v", body["stream"], body["store"], body["model"], body["instructions"])
	}
	if body["max_output_tokens"] != float64(512) || body["max_tool_calls"] != float64(3) {
		t.Errorf("max_output_tokens/max_tool_calls = %v/%v", body["max_output_tokens"], body["max_tool_calls"])
	}
	if r := asMap(t, body["reasoning"]); r["effort"] != "high" || r["summary"] != "auto" {
		t.Errorf("reasoning = %v", r)
	}
	tools := asList(t, body["tools"])
	if len(tools) != 2 {
		t.Fatalf("web_fetch and datetime are not OpenAI tools and must be dropped: %v", tools)
	}
	ws := asMap(t, tools[0])
	if ws["type"] != "web_search" || ws["search_context_size"] != "low" {
		t.Errorf("web_search = %v", ws)
	}
	filters := asMap(t, ws["filters"])
	if fmt.Sprint(filters["allowed_domains"]) != "[openai.com]" {
		t.Errorf("filters = %v", filters)
	}
	if _, both := filters["blocked_domains"]; both {
		t.Errorf("allowed and blocked domains are mutually exclusive: %v", filters)
	}
	fn := asMap(t, tools[1])
	if fn["type"] != "function" || fn["name"] != "get_weather" || fn["strict"] != false || asMap(t, fn["parameters"])["type"] != "object" {
		t.Errorf("function tool = %v", fn)
	}
	in := asList(t, body["input"])
	m := asMap(t, in[0])
	c := asMap(t, asList(t, m["content"])[0])
	if m["type"] != "message" || m["role"] != "user" || c["type"] != "input_text" || c["text"] != "hi" {
		t.Errorf("input = %v", in)
	}
}

func TestBlockedDomainsAndMaxToolCalls(t *testing.T) {
	body, _, _ := capture(t, llm.Request{
		Model:              "gpt-4o",
		Messages:           []llm.Message{userText("hi")},
		Tools:              []llm.ToolSpec{{Hosted: llm.HostedWebSearch, Params: map[string]any{"blocked_domains": []any{"quora.com"}}}},
		Reasoning:          "",
		MaxHostedToolCalls: 0,
	})
	ws := asMap(t, asList(t, body["tools"])[0])
	if fmt.Sprint(asMap(t, ws["filters"])["blocked_domains"]) != "[quora.com]" {
		t.Errorf("web_search = %v", ws)
	}
	if _, ok := body["reasoning"]; ok {
		t.Errorf("reasoning sent for a non-reasoning model: %v", body["reasoning"])
	}
	if _, ok := body["max_tool_calls"]; ok {
		t.Errorf("max_tool_calls = %v", body["max_tool_calls"])
	}

	body, _, _ = capture(t, llm.Request{Model: "gpt-4o", Messages: []llm.Message{userText("hi")}, Tools: []llm.ToolSpec{{Hosted: llm.HostedWebSearch}}})
	if ws := asMap(t, asList(t, body["tools"])[0]); ws["type"] != "web_search" || ws["filters"] != nil {
		t.Errorf("unconfigured web_search = %v", ws)
	}
}

func TestPartsMapping(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	pdf := []byte("%PDF-1.4 test")
	body, _, _ := capture(t, llm.Request{
		Model: "gpt-5.5",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Parts: []llm.Part{
				{Kind: llm.PartText, Text: "look"},
				{Kind: llm.PartImage, MIME: "image/png", Data: png},
				{Kind: llm.PartFile, Name: "report.pdf", Data: pdf},
			}},
			{Role: llm.RoleAssistant, Parts: []llm.Part{
				{Kind: llm.PartText, Text: "calling"},
				{Kind: llm.PartToolCall, ToolCallID: "call_1", ToolName: "get_weather", Args: json.RawMessage(`{"city":"Paris"}`)},
				{Kind: llm.PartToolCall, ToolCallID: "call_2", ToolName: "noargs"},
			}},
			{Role: llm.RoleTool, Parts: []llm.Part{
				{Kind: llm.PartToolResult, ToolCallID: "call_1", Text: "sunny"},
				{Kind: llm.PartToolResult, ToolCallID: "call_2", Text: "boom", IsError: true},
			}},
		},
	})
	in := asList(t, body["input"])
	if len(in) != 6 {
		t.Fatalf("input items = %d: %v", len(in), in)
	}
	parts := asList(t, asMap(t, in[0])["content"])
	img, file := asMap(t, parts[1]), asMap(t, parts[2])
	if img["type"] != "input_image" || !strings.HasPrefix(img["image_url"].(string), "data:image/png;base64,iVBORw0KGgo") {
		t.Errorf("image part = %v", img)
	}
	if file["type"] != "input_file" || file["filename"] != "report.pdf" || !strings.HasPrefix(file["file_data"].(string), "data:application/pdf;base64,") {
		t.Errorf("file part = %v", file)
	}
	as := asMap(t, in[1])
	if c := asMap(t, asList(t, as["content"])[0]); as["role"] != "assistant" || c["type"] != "output_text" || c["text"] != "calling" {
		t.Errorf("assistant message = %v", as)
	}
	fc := asMap(t, in[2])
	if fc["type"] != "function_call" || fc["call_id"] != "call_1" || fc["name"] != "get_weather" || fc["arguments"] != `{"city":"Paris"}` {
		t.Errorf("function_call = %v", fc)
	}
	if asMap(t, in[3])["arguments"] != "{}" {
		t.Errorf("empty arguments must be {}: %v", in[3])
	}
	if out := asMap(t, in[4]); out["type"] != "function_call_output" || out["call_id"] != "call_1" || out["output"] != "sunny" {
		t.Errorf("function_call_output = %v", out)
	}
	if asMap(t, in[5])["call_id"] != "call_2" {
		t.Errorf("second output = %v", in[5])
	}
}

func TestStreamFullTurn(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w,
			`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
			`{"type":"response.reasoning_summary_part.added","item_id":"rs_1","summary_index":0}`,
			`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","summary_index":0,"delta":"Checking "}`,
			`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","summary_index":0,"delta":"sources"}`,
			`{"type":"response.reasoning_summary_part.added","item_id":"rs_1","summary_index":1}`,
			`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","summary_index":1,"delta":"Done"}`,
			`{"type":"response.output_item.added","output_index":1,"item":{"type":"web_search_call","id":"ws_1","status":"in_progress"}}`,
			`{"type":"response.web_search_call.searching","item_id":"ws_1"}`,
			`{"type":"response.output_item.done","output_index":1,"item":{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"go 1.26 release"}}}`,
			`{"type":"response.output_item.added","output_index":2,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
			`{"type":"response.output_text.delta","item_id":"msg_1","content_index":0,"delta":"Go 1.26 "}`,
			`{"type":"response.output_text.delta","item_id":"msg_1","content_index":0,"delta":"is out."}`,
			`{"type":"response.output_text.annotation.added","item_id":"msg_1","annotation_index":0,"annotation":{"type":"url_citation","url":"https://go.dev/doc/go1.26","title":"Go 1.26 Release Notes","start_index":0,"end_index":6}}`,
			`{"type":"response.output_item.done","output_index":2,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Go 1.26 is out.","annotations":[`+
				`{"type":"url_citation","url":"https://go.dev/doc/go1.26","title":"dup"},`+
				`{"type":"url_citation","url":"https://example.org/a","title":"A"},`+
				`{"type":"file_citation","file_id":"f","filename":"x"}]}]}}`,
			`{"type":"response.output_item.added","output_index":3,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":3,"delta":"{\"city\":"}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":3,"delta":"\"Paris\"}"}`,
			`{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":3,"arguments":"{\"city\":\"Paris\"}"}`,
			`{"type":"response.output_item.done","output_index":3,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}","status":"completed"}}`,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":40},"output_tokens":50,"output_tokens_details":{"reasoning_tokens":20},"total_tokens":150}}}`,
		)
	})
	ch, err := p.Stream(context.Background(), llm.Request{Model: "gpt-5.5", Messages: []llm.Message{userText("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	ds := collect(t, ch)

	var text, reasoning strings.Builder
	var cites []llm.Citation
	var servers []llm.ServerTool
	var calls []llm.ToolCall
	var usage *llm.Usage
	for _, d := range ds {
		switch d.Kind {
		case llm.DeltaText:
			text.WriteString(d.Text)
		case llm.DeltaReasoning:
			reasoning.WriteString(d.Text)
		case llm.DeltaCitation:
			cites = append(cites, *d.Citation)
		case llm.DeltaServer:
			servers = append(servers, *d.Server)
		case llm.DeltaToolCall:
			calls = append(calls, *d.Call)
		case llm.DeltaUsage:
			usage = d.Usage
		case llm.DeltaError:
			t.Fatalf("error: %+v", d.Err)
		}
	}
	if text.String() != "Go 1.26 is out." {
		t.Errorf("text = %q", text.String())
	}
	if reasoning.String() != "Checking sources\n\nDone" {
		t.Errorf("reasoning = %q", reasoning.String())
	}
	if len(cites) != 2 || cites[0].URL != "https://go.dev/doc/go1.26" || cites[0].Title != "Go 1.26 Release Notes" || cites[1].URL != "https://example.org/a" {
		t.Errorf("citations = %+v", cites)
	}
	if len(servers) != 2 || servers[0].Status != "running" || servers[1].Status != "done" || servers[1].Tool != llm.HostedWebSearch ||
		servers[1].ID != "ws_1" || servers[1].Detail != "go 1.26 release" {
		t.Errorf("server tools = %+v", servers)
	}
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "get_weather" || string(calls[0].Args) != `{"city":"Paris"}` {
		t.Errorf("calls = %+v", calls)
	}
	if usage == nil || *usage.InputTokens != 100 || *usage.OutputTokens != 50 || *usage.CachedTokens != 40 || *usage.ReasoningTokens != 20 {
		t.Errorf("usage = %+v", usage)
	}
	if usage != nil && usage.CostUSD != nil {
		t.Errorf("cost invented: %v", *usage.CostUSD)
	}
	if last := ds[len(ds)-1]; last.Kind != llm.DeltaStop || last.Stop != llm.StopToolCalls {
		t.Errorf("last = %+v", last)
	}
	n := 0
	for _, d := range ds {
		if d.Kind == llm.DeltaStop {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d stop deltas", n)
	}
}

func TestStreamPlainEnd(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w,
			`{"type":"response.output_text.delta","item_id":"m","delta":"ok"}`,
			`{"type":"response.completed","response":{"status":"completed"}}`)
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "gpt-4o"})
	ds := collect(t, ch)
	if len(ds) != 2 || ds[0].Text != "ok" || ds[1].Kind != llm.DeltaStop || ds[1].Stop != llm.StopEnd {
		t.Fatalf("deltas = %+v (no usage must be reported when the API sent none)", ds)
	}
}

func TestStreamIncomplete(t *testing.T) {
	for reason, want := range map[string]llm.StopReason{"max_output_tokens": llm.StopLength, "content_filter": llm.StopFilter} {
		p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
			sse(w, `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"`+reason+`"}}}`)
		})
		ch, _ := p.Stream(context.Background(), llm.Request{Model: "gpt-4o"})
		ds := collect(t, ch)
		if last := ds[len(ds)-1]; last.Kind != llm.DeltaStop || last.Stop != want {
			t.Errorf("%s: last = %+v, want stop %s", reason, last, want)
		}
	}
}

func TestStreamFailureEvents(t *testing.T) {
	tests := []struct {
		name  string
		event string
		code  llm.ErrorCode
	}{
		{"failed context", `{"type":"response.failed","response":{"status":"failed","error":{"code":"context_length_exceeded","message":"Your input exceeds the context window of this model."}}}`, llm.ErrContextLength},
		{"failed rate limit", `{"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"Rate limit reached"}}}`, llm.ErrRateLimit},
		{"failed server", `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"The server had an error"}}}`, llm.ErrServer},
		{"error event", `{"type":"error","code":"insufficient_quota","message":"You exceeded your current quota","param":null}`, llm.ErrQuota},
		{"failed without error", `{"type":"response.failed","response":{"status":"failed"}}`, llm.ErrServer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
				sse(w, `{"type":"response.output_text.delta","item_id":"m","delta":"partial"}`, tc.event)
			})
			ch, _ := p.Stream(context.Background(), llm.Request{Model: "gpt-4o"})
			ds := collect(t, ch)
			last := ds[len(ds)-1]
			if last.Kind != llm.DeltaError || last.Err.Code != tc.code {
				t.Fatalf("last = %+v (%+v), want error %s", last, last.Err, tc.code)
			}
			for _, d := range ds {
				if d.Kind == llm.DeltaStop {
					t.Error("stop after an error")
				}
			}
		})
	}
}

func TestStreamTruncatedStreamIsAnError(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w, `{"type":"response.output_text.delta","item_id":"m","delta":"half"}`)
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "gpt-4o"})
	ds := collect(t, ch)
	if last := ds[len(ds)-1]; last.Kind != llm.DeltaError || last.Err.Code != llm.ErrNetwork {
		t.Fatalf("last = %+v, want a network error rather than a clean stop", last)
	}
}

func TestStreamMalformedToolArgumentsFail(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		sse(w,
			`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"c1","name":"bad"}}`,
			`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"a\":"}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"c1","name":"bad"}}`,
			`{"type":"response.completed","response":{"status":"completed"}}`)
	})
	ch, _ := p.Stream(context.Background(), llm.Request{Model: "gpt-4o"})
	ds := collect(t, ch)
	if last := ds[len(ds)-1]; last.Kind != llm.DeltaError {
		t.Fatalf("last = %+v, want a terminal error", last)
	}
	for _, d := range ds {
		if d.Kind == llm.DeltaToolCall {
			t.Errorf("a call with broken arguments was delivered: %+v", d.Call)
		}
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
		{"auth", 401, nil, `{"error":{"message":"Incorrect API key provided: ` + testKey + `.","type":"invalid_request_error","code":"invalid_api_key"}}`, llm.ErrAuth, 0},
		{"quota", 429, nil, `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`, llm.ErrQuota, 0},
		{"rate limit", 429, map[string]string{"Retry-After": "7"}, `{"error":{"message":"Rate limit reached for gpt-5","type":"requests","code":"rate_limit_exceeded"}}`, llm.ErrRateLimit, 7 * time.Second},
		{"context length", 400, nil, `{"error":{"message":"This model's maximum context length is 128000 tokens.","type":"invalid_request_error","code":"context_length_exceeded"}}`, llm.ErrContextLength, 0},
		{"model not found", 404, nil, `{"error":{"message":"The model does not exist","type":"invalid_request_error","code":"model_not_found"}}`, llm.ErrBadRequest, 0},
		{"server", 503, nil, `{"error":{"message":"The engine is currently overloaded"}}`, llm.ErrServer, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.hdr {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			ch, err := p.Stream(context.Background(), llm.Request{Model: "gpt-4o"})
			if ch != nil {
				t.Fatal("channel returned with an error status")
			}
			le := llm.AsError(err)
			if le.Code != tc.code || le.Status != tc.status {
				t.Fatalf("error = %+v, want %s/%d", le, tc.code, tc.status)
			}
			if tc.retry > 0 && le.RetryAfter != tc.retry {
				t.Errorf("RetryAfter = %v, want %v", le.RetryAfter, tc.retry)
			}
			if strings.Contains(err.Error(), "SECRET1234") || strings.Contains(err.Error(), "0123456789abcdef") {
				t.Errorf("error leaks the key: %v", err)
			}
		})
	}
}

func TestReasoningSummaryRetry(t *testing.T) {
	var mu sync.Mutex
	var summaries []bool
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Reasoning *struct {
				Effort  string `json:"effort"`
				Summary string `json:"summary"`
			} `json:"reasoning"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		has := b.Reasoning != nil && b.Reasoning.Summary != ""
		mu.Lock()
		summaries = append(summaries, has)
		mu.Unlock()
		if has {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"Your organization must be verified to generate reasoning summaries.","type":"invalid_request_error","param":"reasoning.summary","code":"unsupported_value"}}`)
			return
		}
		if b.Reasoning == nil || b.Reasoning.Effort != "medium" {
			t.Errorf("retry lost the effort: %+v", b.Reasoning)
		}
		sse(w, `{"type":"response.output_text.delta","item_id":"m","delta":"ok"}`, `{"type":"response.completed","response":{"status":"completed"}}`)
	})
	ch, err := p.Stream(context.Background(), llm.Request{Model: "gpt-5.5", Reasoning: "medium", Messages: []llm.Message{userText("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	ds := collect(t, ch)
	if fmt.Sprint(summaries) != "[true false]" {
		t.Errorf("requests had summary = %v, want [true false]", summaries)
	}
	if len(ds) != 2 || ds[0].Text != "ok" {
		t.Errorf("deltas = %+v", ds)
	}
}

func TestStreamRequiresModel(t *testing.T) {
	p := newTestProvider(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent without a model") })
	if _, err := p.Stream(context.Background(), llm.Request{}); llm.AsError(err).Code != llm.ErrBadRequest {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamCancelNoGoroutineLeak(t *testing.T) {
	waitGoroutines(t, -1)
	base := runtime.NumGoroutine()

	handlerDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		sse(w, `{"type":"response.output_text.delta","item_id":"m","delta":"first"}`)
		<-r.Context().Done()
	}))
	tr := &http.Transport{}
	p, err := New(llm.Config{APIKey: testKey, BaseURL: srv.URL, HTTP: &http.Client{Transport: tr}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, llm.Request{Model: "gpt-4o"})
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

func TestStreamConsumerStopsReading(t *testing.T) {
	waitGoroutines(t, -1)
	base := runtime.NumGoroutine()
	var events []string
	for range 50 {
		events = append(events, `{"type":"response.output_text.delta","item_id":"m","delta":"x"}`)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { sse(w, events...) }))
	tr := &http.Transport{}
	p, _ := New(llm.Config{APIKey: testKey, BaseURL: srv.URL, HTTP: &http.Client{Transport: tr}})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, llm.Request{Model: "gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	<-ch
	cancel()
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

func TestModelsAndValidate(t *testing.T) {
	var paths []string
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"message":"Incorrect API key provided","code":"invalid_api_key"}}`)
			return
		}
		io.WriteString(w, `{"object":"list","data":[
			{"id":"gpt-4o","object":"model","created":100,"owned_by":"system"},
			{"id":"gpt-5.5","object":"model","created":300,"owned_by":"system"},
			{"id":"o3-deep-research","object":"model","created":200,"owned_by":"system"},
			{"id":"text-embedding-3-small","object":"model","created":400,"owned_by":"system"},
			{"id":"gpt-4o-realtime-preview","object":"model","created":400,"owned_by":"system"},
			{"id":"gpt-4o-mini-tts","object":"model","created":400,"owned_by":"system"},
			{"id":"gpt-image-1","object":"model","created":400,"owned_by":"system"},
			{"id":"dall-e-3","object":"model","created":400,"owned_by":"system"},
			{"id":"whisper-1","object":"model","created":400,"owned_by":"system"},
			{"id":"omni-moderation-latest","object":"model","created":400,"owned_by":"system"},
			{"id":"davinci-002","object":"model","created":400,"owned_by":"system"}]}`)
	})
	acc, err := p.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(acc); strings.Contains(string(b), testKey) {
		t.Errorf("account leaks the key: %s", b)
	}
	ms, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(paths) != "[GET /v1/models GET /v1/models]" {
		t.Errorf("requests = %v", paths)
	}
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if fmt.Sprint(ids) != "[gpt-5.5 o3-deep-research gpt-4o]" {
		t.Fatalf("models = %v, want chat-capable ones newest first", ids)
	}
	gpt5, deep, gpt4o := ms[0], ms[1], ms[2]
	if !gpt5.Caps.Tools || !gpt5.Caps.Vision || !gpt5.Caps.PDF || !gpt5.Caps.Reasoning || gpt5.Caps.DeepResearch ||
		fmt.Sprint(gpt5.Caps.HostedTools) != "[web_search]" || gpt5.Caps.ContextWindow == 0 || len(gpt5.Reasoning) == 0 {
		t.Errorf("gpt-5.5 = %+v", gpt5)
	}
	if !deep.Caps.DeepResearch || !deep.Caps.Reasoning || fmt.Sprint(deep.Caps.HostedTools) != "[web_search]" {
		t.Errorf("o3-deep-research = %+v", deep)
	}
	if gpt4o.Caps.Reasoning || len(gpt4o.Reasoning) != 0 || !gpt4o.Caps.Tools {
		t.Errorf("gpt-4o = %+v", gpt4o)
	}

	bad, _ := New(llm.Config{APIKey: "sk-wrong-0000000000", BaseURL: p.(*Provider).base, HTTP: p.(*Provider).http})
	if _, err := bad.Validate(context.Background()); llm.AsError(err).Code != llm.ErrAuth {
		t.Fatalf("bad key: %v", err)
	}
}

func TestCapsLookup(t *testing.T) {
	for id, want := range map[string]bool{
		"gpt-5.5": true, "gpt-5.5-pro-2026-01-01": true, "gpt-5-mini": true, "gpt-4.1-nano": true, "o4-mini-deep-research-2025-06-26": true,
		"gpt-5-search-api": false, "gpt-4o-audio-preview": false, "gpt-realtime": false, "gpt-image-1": false, "text-embedding-3-large": false,
		"tts-1": false, "whisper-1": false, "babbage-002": false, "": false,
	} {
		if _, ok := modelFor(id); ok != want {
			t.Errorf("modelFor(%q) = %v, want %v", id, ok, want)
		}
	}
	// The longest prefix wins: the pro variant must not inherit the base family's effort levels.
	pro, _ := modelFor("gpt-5.5-pro")
	base, _ := modelFor("gpt-5.5")
	if fmt.Sprint(pro.Reasoning) == fmt.Sprint(base.Reasoning) {
		t.Errorf("gpt-5.5-pro reasoning = %v (same as base)", pro.Reasoning)
	}
	if nano, _ := modelFor("gpt-4.1-nano"); len(nano.Caps.HostedTools) != 0 {
		t.Errorf("gpt-4.1-nano has no web search: %v", nano.Caps.HostedTools)
	}
	for _, id := range []string{"o3-deep-research", "o4-mini-deep-research"} {
		if m, _ := modelFor(id); !m.Caps.DeepResearch {
			t.Errorf("%s is not marked deep research", id)
		}
	}
	if capsTableVersion == "" {
		t.Error("capability table has no version")
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New(llm.Config{APIKey: ""}); err == nil {
		t.Fatal("blank key accepted")
	}
	if d := Descriptor(); d.ID != "openai" || d.Kind != "builtin" || d.New == nil || d.OAuth != nil {
		t.Fatalf("descriptor = %+v", d)
	}
}

func TestKeyNeverInDeltasOrErrors(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		echo := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.WriteHeader(500)
			io.WriteString(w, `{"error":{"message":"oops `+echo+`"}}`)
			return
		}
		sse(w, `{"type":"error","code":"server_error","message":"upstream said `+echo+`"}`)
	})
	ch, err := p.Stream(context.Background(), llm.Request{Model: "gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(collect(t, ch))
	if strings.Contains(string(b), "SECRET1234") {
		t.Errorf("stream error leaks the key: %s", b)
	}
	if _, err := p.Models(context.Background()); err == nil || strings.Contains(err.Error(), "SECRET1234") {
		t.Errorf("Models error = %v", err)
	}
}

func TestLive(t *testing.T) {
	if os.Getenv("OROLE_OPENAI_LIVE") != "1" {
		t.Skip("set OROLE_OPENAI_LIVE=1 and OPENAI_API_KEY to run against the real API")
	}
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY is not set")
	}
	model := os.Getenv("OROLE_OPENAI_MODEL")
	if model == "" {
		model = "gpt-4.1-nano"
	}
	p, err := New(llm.Config{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := p.Validate(ctx); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	ms, err := p.Models(ctx)
	if err != nil || len(ms) == 0 {
		t.Fatalf("Models: %d, %v", len(ms), err)
	}
	ch, err := p.Stream(ctx, llm.Request{
		Model:           model,
		Messages:        []llm.Message{userText("Reply with the single word: ok")},
		MaxOutputTokens: 32,
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

package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/manifest"
)

const testKey = "gateway-secret-0123456789"

// seen records what the server received.
type seen struct {
	mu      sync.Mutex
	headers []http.Header
	paths   []string
	bodies  []map[string]any
}

func (s *seen) record(r *http.Request) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headers = append(s.headers, r.Header.Clone())
	s.paths = append(s.paths, r.Method+" "+r.URL.Path)
	var body map[string]any
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
	}
	s.bodies = append(s.bodies, body)
	return body
}

func (s *seen) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.paths)
}

func (s *seen) last() (http.Header, map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers[len(s.headers)-1], s.bodies[len(s.bodies)-1]
}

func sse(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func testManifest(srv *httptest.Server) manifest.ProviderManifest {
	return manifest.ProviderManifest{
		SchemaVersion: 1, ID: "gateway", Name: "Gateway", BaseURL: srv.URL + "/v1",
		Auth: manifest.ManifestAuth{Type: manifest.AuthBearer},
	}
}

func newProvider(t *testing.T, m manifest.ProviderManifest, key string) *Provider {
	t.Helper()
	p, err := New(Options{Manifest: m, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func collect(t *testing.T, ch <-chan llm.Delta) []llm.Delta {
	t.Helper()
	var out []llm.Delta
	timeout := time.After(10 * time.Second)
	for {
		select {
		case d, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, d)
		case <-timeout:
			t.Fatal("stream did not finish")
		}
	}
}

func userMsg(text string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: text}}}
}

func TestStreamTextUsageAndRequestShape(t *testing.T) {
	var rec seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		sse(w,
			`{"choices":[{"delta":{"role":"assistant","content":"Hel"}}]}`,
			`{"choices":[{"delta":{"reasoning_content":"think"}}]}`,
			`{"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`,
			`[DONE]`)
	}))
	defer srv.Close()
	m := testManifest(srv)
	m.Headers = map[string]string{"OpenAI-Organization": "org-7"}
	p := newProvider(t, m, testKey)
	ch, err := p.Stream(context.Background(), llm.Request{
		Model: "m1", System: "be brief", MaxOutputTokens: 64,
		Messages: []llm.Message{userMsg("hi")},
		Tools: []llm.ToolSpec{
			{Name: "lookup", Description: "d", Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)},
			{Hosted: llm.HostedWebSearch},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text, reasoning strings.Builder
	var usage *llm.Usage
	var stop llm.StopReason
	for _, d := range collect(t, ch) {
		switch d.Kind {
		case llm.DeltaText:
			text.WriteString(d.Text)
		case llm.DeltaReasoning:
			reasoning.WriteString(d.Text)
		case llm.DeltaUsage:
			usage = d.Usage
		case llm.DeltaStop:
			stop = d.Stop
		case llm.DeltaError:
			t.Fatalf("error delta: %+v", d.Err)
		}
	}
	if text.String() != "Hello" || reasoning.String() != "think" || stop != llm.StopEnd {
		t.Fatalf("text=%q reasoning=%q stop=%q", text.String(), reasoning.String(), stop)
	}
	if usage == nil || *usage.InputTokens != 12 || *usage.OutputTokens != 3 || *usage.CachedTokens != 2 || *usage.ReasoningTokens != 1 || usage.CostUSD != nil {
		t.Fatalf("usage: %+v", usage)
	}
	h, body := rec.last()
	if rec.paths[0] != "POST /v1/chat/completions" {
		t.Fatalf("path %s", rec.paths[0])
	}
	if h.Get("Authorization") != "Bearer "+testKey || h.Get("OpenAI-Organization") != "org-7" || h.Get("Content-Type") != "application/json" {
		t.Fatalf("headers: %v", h)
	}
	if body["stream"] != true || body["model"] != "m1" || body["max_tokens"] != float64(64) {
		t.Fatalf("body: %v", body)
	}
	if so, _ := body["stream_options"].(map[string]any); so["include_usage"] != true {
		t.Fatalf("stream_options: %v", body["stream_options"])
	}
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("hosted tool was sent or function tool missing: %v", body["tools"])
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if tools[0].(map[string]any)["type"] != "function" || fn["name"] != "lookup" {
		t.Fatalf("tool: %v", tools[0])
	}
	msgs := body["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["content"] != "hi" {
		t.Fatalf("messages: %v", msgs)
	}
}

func TestStreamToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"go\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","function":{"name":"other","arguments":"{}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`)
	}))
	defer srv.Close()
	p := newProvider(t, testManifest(srv), testKey)
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m1", Messages: []llm.Message{userMsg("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	var calls []llm.ToolCall
	var stop llm.StopReason
	for _, d := range collect(t, ch) {
		switch d.Kind {
		case llm.DeltaToolCall:
			calls = append(calls, *d.Call)
		case llm.DeltaStop:
			stop = d.Stop
		}
	}
	if len(calls) != 2 || calls[0].ID != "call_1" || calls[0].Name != "lookup" || string(calls[0].Args) != `{"q":"go"}` || calls[1].ID != "call_2" || stop != llm.StopToolCalls {
		t.Fatalf("calls=%+v stop=%q", calls, stop)
	}
}

func TestToolHistoryAndImagesAreGatedByManifest(t *testing.T) {
	var rec seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		sse(w, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`, `[DONE]`)
	}))
	defer srv.Close()
	m := testManifest(srv)
	m.Models = []manifest.ManifestModel{{ID: "vision", Vision: true, Tools: true}, {ID: "plain"}}
	p := newProvider(t, m, testKey)
	image := llm.Message{Role: llm.RoleUser, Parts: []llm.Part{
		{Kind: llm.PartText, Text: "what is this"},
		{Kind: llm.PartImage, MIME: "image/png", Data: []byte{1, 2, 3}},
	}}

	if _, err := p.Stream(context.Background(), llm.Request{Model: "plain", Messages: []llm.Message{image}}); err == nil {
		t.Fatal("image sent to a model without declared vision")
	} else if le := llm.AsError(err); le.Code != llm.ErrBadRequest {
		t.Fatalf("err = %v", err)
	}
	if _, err := p.Stream(context.Background(), llm.Request{Model: "undeclared", Messages: []llm.Message{image}}); err == nil {
		t.Fatal("image sent to an undeclared model")
	}
	if rec.count() != 0 {
		t.Fatal("a refused request reached the server")
	}
	file := llm.Message{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartFile, Data: []byte("%PDF")}}}
	if _, err := p.Stream(context.Background(), llm.Request{Model: "vision", Messages: []llm.Message{file}}); err == nil {
		t.Fatal("PDF sent to a model without declared pdf support")
	}

	history := []llm.Message{
		image,
		{Role: llm.RoleAssistant, Parts: []llm.Part{{Kind: llm.PartToolCall, ToolCallID: "c1", ToolName: "lookup", Args: json.RawMessage(`{"q":"x"}`)}}},
		{Role: llm.RoleTool, Parts: []llm.Part{{Kind: llm.PartToolResult, ToolCallID: "c1", Text: "result"}}},
	}
	ch, err := p.Stream(context.Background(), llm.Request{Model: "vision", Messages: history})
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	_, body := rec.last()
	msgs := body["messages"].([]any)
	user := msgs[0].(map[string]any)["content"].([]any)
	if user[1].(map[string]any)["type"] != "image_url" || !strings.HasPrefix(user[1].(map[string]any)["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,") {
		t.Fatalf("image part: %v", user)
	}
	asst := msgs[1].(map[string]any)
	call := asst["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "c1" || call["type"] != "function" || call["function"].(map[string]any)["arguments"] != `{"q":"x"}` {
		t.Fatalf("assistant tool call: %v", asst)
	}
	if tool := msgs[2].(map[string]any); tool["role"] != "tool" || tool["tool_call_id"] != "c1" || tool["content"] != "result" {
		t.Fatalf("tool result: %v", tool)
	}
}

func TestAuthPlacement(t *testing.T) {
	cases := []struct {
		name string
		auth manifest.ManifestAuth
		key  string
		want func(http.Header) bool
	}{
		{"bearer", manifest.ManifestAuth{Type: manifest.AuthBearer}, testKey, func(h http.Header) bool {
			return h.Get("Authorization") == "Bearer "+testKey
		}},
		{"header", manifest.ManifestAuth{Type: manifest.AuthHeader, Header: "X-Gateway-Key"}, testKey, func(h http.Header) bool {
			return h.Get("X-Gateway-Key") == testKey && h.Get("Authorization") == ""
		}},
		{"none", manifest.ManifestAuth{Type: manifest.AuthNone}, "", func(h http.Header) bool {
			return h.Get("Authorization") == "" && len(h.Values("X-Gateway-Key")) == 0
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec seen
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.record(r)
				fmt.Fprint(w, `{"data":[{"id":"a"}]}`)
			}))
			defer srv.Close()
			m := testManifest(srv)
			m.Auth = tc.auth
			p := newProvider(t, m, tc.key)
			if _, err := p.Validate(context.Background()); err != nil {
				t.Fatal(err)
			}
			h, _ := rec.last()
			if !tc.want(h) {
				t.Fatalf("headers: %v", h)
			}
		})
	}
	// A key with auth none, or no key with auth bearer, is a construction error.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	none := testManifest(srv)
	none.Auth = manifest.ManifestAuth{Type: manifest.AuthNone}
	if _, err := New(Options{Manifest: none, Key: "k"}); err == nil {
		t.Fatal("auth none accepted a key")
	}
	if _, err := New(Options{Manifest: testManifest(srv)}); !errors.Is(err, llm.ErrNotConfigured) {
		t.Fatalf("missing key: %v", err)
	}
}

func TestModelsEndpointMergeAndFallback(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			http.Error(w, `{"error":{"message":"nope"}}`, status)
			return
		}
		fmt.Fprint(w, `{"object":"list","data":[{"id":"llama3:8b"},{"id":"other","name":"Other"},{"id":"other"},{"id":""}]}`)
	}))
	defer srv.Close()
	m := testManifest(srv)
	m.Models = []manifest.ManifestModel{
		{ID: "llama3:8b", Name: "Llama 3", Vision: true, Tools: true, Reasoning: true, ContextWindow: 8192},
		{ID: "declared-only", Tools: true},
	}
	p := newProvider(t, m, testKey)

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "llama3:8b" || models[0].Name != "Llama 3" || !models[0].Caps.Vision || !models[0].Caps.Tools ||
		models[0].Caps.ContextWindow != 8192 || len(models[0].Reasoning) != 3 {
		t.Fatalf("models: %+v", models)
	}
	if models[1].ID != "other" || models[1].Name != "Other" || models[1].Caps.Tools || models[1].Caps.Vision || models[1].Reasoning == nil {
		t.Fatalf("undeclared model got capabilities or nil slices: %+v", models[1])
	}

	status = http.StatusNotFound
	models, err = p.Models(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "llama3:8b" || models[1].ID != "declared-only" {
		t.Fatalf("fallback: %+v %v", models, err)
	}
	if _, err := p.Validate(context.Background()); err != nil {
		t.Fatalf("validate with static list: %v", err)
	}

	// An auth failure is never hidden by the static list.
	status = http.StatusUnauthorized
	if _, err := p.Models(context.Background()); llm.AsError(err).Code != llm.ErrAuth {
		t.Fatalf("auth error hidden: %v", err)
	}
	if _, err := p.Validate(context.Background()); llm.AsError(err).Code != llm.ErrAuth {
		t.Fatalf("validate must fail on auth: %v", err)
	}

	// Without a static list the endpoint error surfaces.
	m.Models = nil
	status = http.StatusNotFound
	if _, err := newProvider(t, m, testKey).Models(context.Background()); err == nil {
		t.Fatal("missing models endpoint without a static list succeeded")
	}
}

func TestErrorsNeverEchoTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad key `+testKey+` for you"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	p := newProvider(t, testManifest(srv), testKey)
	_, err := p.Stream(context.Background(), llm.Request{Model: "m", Messages: []llm.Message{userMsg("x")}})
	if err == nil || strings.Contains(err.Error(), testKey) || llm.AsError(err).Code != llm.ErrAuth {
		t.Fatalf("err = %v", err)
	}
}

func TestRedirectToAnotherHostIsRefusedAndKeyNotSent(t *testing.T) {
	var elsewhere seen
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.record(r)
		fmt.Fprint(w, `{"data":[{"id":"stolen"}]}`)
	}))
	defer other.Close()
	var origin seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin.record(r)
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	for _, auth := range []manifest.ManifestAuth{
		{Type: manifest.AuthBearer},
		{Type: manifest.AuthHeader, Header: "X-Gateway-Key"},
	} {
		m := testManifest(srv)
		m.Auth = auth
		p := newProvider(t, m, testKey)
		if _, err := p.Validate(context.Background()); err == nil || !strings.Contains(err.Error(), "different host") {
			t.Fatalf("%s: models redirect followed: %v", auth.Type, err)
		}
		if _, err := p.Stream(context.Background(), llm.Request{Model: "m", Messages: []llm.Message{userMsg("secret prompt")}}); err == nil || !strings.Contains(err.Error(), "different host") {
			t.Fatalf("%s: chat redirect followed: %v", auth.Type, err)
		}
	}
	if elsewhere.count() != 0 {
		t.Fatalf("the other host received %d request(s): %v", elsewhere.count(), elsewhere.paths)
	}
	if origin.count() == 0 {
		t.Fatal("the reviewed host was never contacted")
	}
}

func TestSameHostRedirectIsFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			http.Redirect(w, r, "/v1/models2", http.StatusFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"a"}]}`)
	}))
	defer srv.Close()
	p := newProvider(t, testManifest(srv), testKey)
	models, err := p.Models(context.Background())
	if err != nil || len(models) != 1 {
		t.Fatalf("%+v %v", models, err)
	}
}

// A provider built on any client still refuses to send a request outside the reviewed origin.
func TestPinnedOriginRefusesOtherHosts(t *testing.T) {
	var elsewhere seen
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { elsewhere.record(r) }))
	defer other.Close()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	p, err := New(Options{Manifest: testManifest(srv), Key: testKey, HTTP: http.DefaultClient})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, other.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+testKey)
	if _, err := p.http.Do(req); !errors.Is(err, errCrossHost) {
		t.Fatalf("err = %v", err)
	}
	if elsewhere.count() != 0 {
		t.Fatal("request left the reviewed origin")
	}
}

func TestNonPublicHostsAreNotDialled(t *testing.T) {
	for _, base := range []string{"https://10.0.0.5/v1", "https://192.168.1.10", "https://169.254.169.254", "https://[fd00::1]", "https://203.0.113.9"} {
		m := manifest.ProviderManifest{SchemaVersion: 1, ID: "lan", Name: "LAN", BaseURL: base, Auth: manifest.ManifestAuth{Type: manifest.AuthBearer}}
		p := newProvider(t, m, testKey)
		_, err := p.Validate(context.Background())
		if err == nil || !strings.Contains(err.Error(), "non-public") {
			t.Errorf("%s: err = %v", base, err)
		}
	}
}

func TestPublicAddress(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "::1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.0.1": false,
		"169.254.1.1": false, "100.64.0.1": false, "0.0.0.0": false, "224.0.0.1": false, "fe80::1": false,
		"::ffff:10.0.0.1": false, "2001:db8::1": false, "fc00::1": false,
	} {
		if got := publicAddress(netip.MustParseAddr(addr)); got != want {
			t.Errorf("publicAddress(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestLoopbackManifestReachesOnlyLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":[{"id":"a"}]}`) }))
	defer srv.Close()
	// The httptest listener is on 127.0.0.1, so a loopback manifest reaches it.
	if _, err := newProvider(t, testManifest(srv), testKey).Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The same server addressed by localhost is also loopback.
	m := testManifest(srv)
	m.BaseURL = strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/v1"
	if _, err := newProvider(t, m, testKey).Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A non-loopback manifest can never reach a loopback listener, even with a loopback answer.
	if err := func() error {
		_, err := dialChecked(context.Background(), nil, "tcp", "127.0.0.1", "80", false)
		return err
	}(); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("remote manifest may dial loopback: %v", err)
	}
	if _, err := dialChecked(context.Background(), nil, "tcp", "8.8.8.8", "80", true); err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("loopback manifest may dial a public address: %v", err)
	}
}

func TestRevokedLifeCancelsStreamAndRefusesNewProviders(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sse(w, `{"choices":[{"delta":{"content":"partial"}}]}`)
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	life, revoke := context.WithCancel(context.Background())
	p, err := New(Options{Manifest: testManifest(srv), Key: testKey, Life: life})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m", Messages: []llm.Message{userMsg("x")}})
	if err != nil {
		t.Fatal(err)
	}
	first := <-ch
	if first.Kind != llm.DeltaText {
		t.Fatalf("first delta: %+v", first)
	}
	<-started
	revoke()
	rest := collect(t, ch)
	if len(rest) != 1 || rest[0].Kind != llm.DeltaError || rest[0].Err.Code != llm.ErrCanceled {
		t.Fatalf("after revoke: %+v", rest)
	}
	if _, err := New(Options{Manifest: testManifest(srv), Key: testKey, Life: life}); err == nil {
		t.Fatal("provider built from a revoked manifest")
	}
	if _, err := p.Models(context.Background()); llm.AsError(err).Code != llm.ErrCanceled {
		t.Fatalf("models after revoke: %v", err)
	}
}

func TestStreamEndsWithoutDoneAndInStreamError(t *testing.T) {
	t.Run("eof after finish", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sse(w, `{"choices":[{"delta":{"content":"x"},"finish_reason":"length"}]}`)
		}))
		defer srv.Close()
		ch, err := newProvider(t, testManifest(srv), testKey).Stream(context.Background(), llm.Request{Model: "m", Messages: []llm.Message{userMsg("x")}})
		if err != nil {
			t.Fatal(err)
		}
		ds := collect(t, ch)
		if last := ds[len(ds)-1]; last.Kind != llm.DeltaStop || last.Stop != llm.StopLength {
			t.Fatalf("%+v", ds)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sse(w, `{"choices":[{"delta":{"content":"x"}}]}`)
		}))
		defer srv.Close()
		ch, _ := newProvider(t, testManifest(srv), testKey).Stream(context.Background(), llm.Request{Model: "m", Messages: []llm.Message{userMsg("x")}})
		ds := collect(t, ch)
		if last := ds[len(ds)-1]; last.Kind != llm.DeltaError || last.Err.Code != llm.ErrNetwork {
			t.Fatalf("%+v", ds)
		}
	})
	t.Run("error event", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sse(w, `{"error":{"message":"model overloaded","code":429}}`)
		}))
		defer srv.Close()
		ch, _ := newProvider(t, testManifest(srv), testKey).Stream(context.Background(), llm.Request{Model: "m", Messages: []llm.Message{userMsg("x")}})
		ds := collect(t, ch)
		if len(ds) != 1 || ds[0].Kind != llm.DeltaError || ds[0].Err.Code != llm.ErrRateLimit {
			t.Fatalf("%+v", ds)
		}
	})
}

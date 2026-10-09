package llm

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReadSSE(t *testing.T) {
	t.Parallel()
	body := ": OPENROUTER PROCESSING\r\n\r\n" +
		"data: {\"a\":1}\r\n\r\n" +
		"event: custom\ndata: line1\ndata: line2\n\n" +
		"id: 7\nretry: 100\ndata: [DONE]\n\n" +
		"data: unterminated"
	type got struct{ event, data string }
	var evs []got
	err := ReadSSE(context.Background(), io.NopCloser(strings.NewReader(body)), 0, func(e SSEEvent) error {
		evs = append(evs, got{e.Event, string(e.Data)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []got{{"", `{"a":1}`}, {"custom", "line1\nline2"}, {"", "[DONE]"}, {"", "unterminated"}}
	if len(evs) != len(want) {
		t.Fatalf("events = %#v", evs)
	}
	for i := range want {
		if evs[i] != want[i] {
			t.Errorf("event %d = %#v, want %#v", i, evs[i], want[i])
		}
	}
}

func TestReadSSEStopAndLineLimit(t *testing.T) {
	t.Parallel()
	n := 0
	err := ReadSSE(context.Background(), io.NopCloser(strings.NewReader("data: 1\n\ndata: 2\n\n")), 0, func(SSEEvent) error {
		n++
		return ErrSSEDone
	})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	long := "data: " + strings.Repeat("x", 5000) + "\n\n"
	err = ReadSSE(context.Background(), io.NopCloser(strings.NewReader(long)), 1024, func(SSEEvent) error { return nil })
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("err = %v, want ErrTooLong", err)
	}
}

func TestReadSSECancelNoLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ReadSSE(ctx, pr, 0, func(SSEEvent) error { return nil })
	}()
	pw.Write([]byte("data: hello\n\n"))
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadSSE did not return after cancel")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		t.Fatalf("goroutines: %d > baseline %d", n, base)
	}
}

func TestMapHTTPError(t *testing.T) {
	t.Parallel()
	hdr := func(k, v string) http.Header { h := http.Header{}; h.Set(k, v); return h }
	tests := []struct {
		name   string
		status int
		h      http.Header
		body   string
		want   ErrorCode
		retry  time.Duration
		msg    string
	}{
		{"openai invalid key", 401, nil, `{"error":{"message":"Incorrect API key provided: sk-abcdef1234567890abcd.","type":"invalid_request_error","code":"invalid_api_key"}}`, ErrAuth, 0, ""},
		{"openrouter 401", 401, nil, `{"error":{"code":401,"message":"No auth credentials found"}}`, ErrAuth, 0, "No auth credentials found"},
		{"openrouter 402", 402, nil, `{"error":{"code":402,"message":"Insufficient credits. Add more using https://openrouter.ai/credits"}}`, ErrQuota, 0, ""},
		{"openai quota 429", 429, nil, `{"error":{"message":"You exceeded your current quota","type":"insufficient_quota","code":"insufficient_quota"}}`, ErrQuota, 0, ""},
		{"rate limit seconds", 429, hdr("Retry-After", "7"), `{"error":{"code":429,"message":"Rate limit exceeded: free-models-per-min"}}`, ErrRateLimit, 7 * time.Second, ""},
		{"rate limit date", 429, hdr("Retry-After", time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat)), `{"error":{"message":"slow down"}}`, ErrRateLimit, 30 * time.Second, ""},
		{"openai context", 400, nil, `{"error":{"message":"This model's maximum context length is 128000 tokens.","type":"invalid_request_error","code":"context_length_exceeded"}}`, ErrContextLength, 0, ""},
		{"openrouter context", 400, nil, `{"error":{"code":400,"message":"This endpoint's maximum context length is 8192 tokens. However, you requested about 20000 tokens"}}`, ErrContextLength, 0, ""},
		{"openrouter context in metadata", 400, nil, `{"error":{"code":400,"message":"Provider returned error","metadata":{"raw":"{\"error\":{\"message\":\"prompt is too long: 300000 tokens\"}}"}}}`, ErrContextLength, 0, ""},
		{"moderation", 403, nil, `{"error":{"code":403,"message":"Your input was flagged","metadata":{"reasons":["violence"]}}}`, ErrFilter, 0, ""},
		{"bad model", 404, nil, `{"error":{"message":"The model does not exist","code":"model_not_found"}}`, ErrBadRequest, 0, ""},
		{"server", 502, nil, `<html>bad gateway</html>`, ErrServer, 0, ""},
		{"midstream numeric code", 0, nil, `{"error":{"code":429,"message":"Provider rate limited"}}`, ErrRateLimit, 0, ""},
		{"midstream string code", 0, nil, `{"error":{"code":"rate_limit_exceeded","message":"Too fast"}}`, ErrRateLimit, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := MapHTTPError(tc.status, tc.h, []byte(tc.body), "sk-abcdef1234567890abcd")
			if e.Code != tc.want {
				t.Fatalf("code = %s (%q), want %s", e.Code, e.Message, tc.want)
			}
			if tc.retry > 0 && (e.RetryAfter < tc.retry-2*time.Second || e.RetryAfter > tc.retry) {
				t.Errorf("RetryAfter = %v, want ~%v", e.RetryAfter, tc.retry)
			}
			if tc.msg != "" && e.Message != tc.msg {
				t.Errorf("message = %q, want %q", e.Message, tc.msg)
			}
			if strings.Contains(e.Message, "abcdef1234567890") {
				t.Errorf("message leaks key: %q", e.Message)
			}
		})
	}
}

func TestNetworkErrorMapping(t *testing.T) {
	t.Parallel()
	if e := NetworkError(context.Canceled); e.Code != ErrCanceled {
		t.Errorf("canceled: %v", e)
	}
	if e := NetworkError(context.DeadlineExceeded); e.Code != ErrNetwork {
		t.Errorf("deadline: %v", e)
	}
	e := NetworkError(errors.New("dial tcp: lookup sk-secret-key-123456789 failed"), "sk-secret-key-123456789")
	if e.Code != ErrNetwork || strings.Contains(e.Message, "secret-key") {
		t.Errorf("redaction failed: %v", e)
	}
}

func TestToolAssembler(t *testing.T) {
	t.Parallel()
	var a ToolAssembler
	a.Begin("0", "call_a", "search")
	a.Append("0", `{"q":`)
	a.Begin("1", "call_b", "noargs")
	a.Append("0", `"go"}`)
	if !a.Pending() {
		t.Fatal("Pending = false")
	}
	calls, err := a.Drain()
	if err != nil || len(calls) != 2 {
		t.Fatalf("calls=%v err=%v", calls, err)
	}
	if calls[0].ID != "call_a" || calls[0].Name != "search" || string(calls[0].Args) != `{"q":"go"}` {
		t.Errorf("call 0 = %+v", calls[0])
	}
	if string(calls[1].Args) != `{}` {
		t.Errorf("empty args = %q, want {}", calls[1].Args)
	}
	if a.Pending() {
		t.Error("Pending after Drain")
	}

	var b ToolAssembler
	b.Begin("0", "c1", "one")
	b.Append("0", `{"a":1}`)
	b.Begin("0", "c2", "two") // a gateway reusing index 0 for a second call
	b.Append("0", `{"b":2}`)
	calls, err = b.Drain()
	if err != nil || len(calls) != 2 || calls[0].Name != "one" || calls[1].Name != "two" {
		t.Fatalf("reused index: %+v err=%v", calls, err)
	}

	var c ToolAssembler
	c.Begin("0", "c1", "bad")
	c.Append("0", `{"a":`)
	if _, err := c.Drain(); err == nil {
		t.Fatal("malformed arguments accepted")
	}
	var d ToolAssembler
	d.Begin("0", "", "named")
	call, ok, err := d.Take("0")
	if err != nil || !ok || call.ID == "" {
		t.Fatalf("synthesized id: %+v ok=%v err=%v", call, ok, err)
	}
}

func TestPKCEChallenge(t *testing.T) {
	t.Parallel()
	// RFC 7636 appendix B.
	got := PKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"; got != want {
		t.Fatalf("challenge = %s, want %s", got, want)
	}
	v, c, err := NewPKCE()
	if err != nil || len(v) < 43 || PKCEChallenge(v) != c {
		t.Fatalf("NewPKCE: v=%q c=%q err=%v", v, c, err)
	}
}

func TestDataURLAndTruncate(t *testing.T) {
	t.Parallel()
	if got := DataURL("image/png", []byte{1, 2, 3}); got != "data:image/png;base64,AQID" {
		t.Errorf("DataURL = %q", got)
	}
	if got := DataURL("", []byte("%PDF-1.4")); !strings.HasPrefix(got, "data:application/pdf;base64,") {
		t.Errorf("sniffed DataURL = %q", got)
	}
	if got := Truncate("héllo wörld", 2); got != "h…" {
		t.Errorf("Truncate = %q", got)
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()
	r, err := NewRegistry(Descriptor{ID: "a", New: func(Config) (Provider, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Descriptor{ID: "a", New: func(Config) (Provider, error) { return nil, nil }}); err == nil {
		t.Error("duplicate id accepted")
	}
	if err := r.Register(Descriptor{ID: "b"}); err == nil {
		t.Error("descriptor without constructor accepted")
	}
	d, ok := r.Get("a")
	if !ok || d.Kind != "builtin" || d.Name != "a" {
		t.Errorf("descriptor = %+v ok=%v", d, ok)
	}
}

package llm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var fastRetry = RetryPolicy{Attempts: 3, Base: time.Millisecond, Cap: 4 * time.Millisecond, MaxRetryAfter: 200 * time.Millisecond}

func postReq(t *testing.T, ctx context.Context, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestDoRetry(t *testing.T) {
	t.Parallel()
	const quota = `{"error":{"message":"You exceeded your current quota","code":"insufficient_quota"}}`
	tests := []struct {
		name     string
		statuses []int // response per request; the last one repeats
		bodies   []string
		hdr      map[string]string
		policy   RetryPolicy
		wantReqs int32
		wantOK   bool
		wantCode ErrorCode
	}{
		{name: "429 then success", statuses: []int{429, 200}, policy: fastRetry, wantReqs: 2, wantOK: true},
		{name: "503 twice then success", statuses: []int{503, 502, 200}, policy: fastRetry, wantReqs: 3, wantOK: true},
		{name: "gives up after the attempts", statuses: []int{503}, policy: fastRetry, wantReqs: 3, wantCode: ErrServer},
		{name: "retry-after is waited out", statuses: []int{429, 200}, hdr: map[string]string{"Retry-After": "0.03"}, policy: fastRetry, wantReqs: 2, wantOK: true},
		{name: "retry-after beyond the limit is returned", statuses: []int{429, 200}, hdr: map[string]string{"Retry-After": "30"}, policy: fastRetry, wantReqs: 1, wantCode: ErrRateLimit},
		{name: "quota is final", statuses: []int{429, 200}, bodies: []string{quota}, policy: fastRetry, wantReqs: 1, wantCode: ErrQuota},
		{name: "auth is final", statuses: []int{401, 200}, policy: fastRetry, wantReqs: 1, wantCode: ErrAuth},
		{name: "bad request is final", statuses: []int{400, 200}, policy: fastRetry, wantReqs: 1, wantCode: ErrBadRequest},
		{name: "retry disabled", statuses: []int{503, 200}, policy: NoRetry, wantReqs: 1, wantCode: ErrServer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var n atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				i := int(n.Add(1)) - 1
				if body, _ := io.ReadAll(r.Body); string(body) != "payload" {
					t.Errorf("request %d body = %q, want it replayed", i, body)
				}
				st := tc.statuses[min(i, len(tc.statuses)-1)]
				if st != 200 {
					for k, v := range tc.hdr {
						w.Header().Set(k, v)
					}
					w.WriteHeader(st)
					if len(tc.bodies) > 0 {
						io.WriteString(w, tc.bodies[0])
					}
					return
				}
				io.WriteString(w, "data: ok\n\n")
			}))
			defer srv.Close()
			ctx := WithRetry(context.Background(), tc.policy)
			resp, err := DoRetry(srv.Client(), postReq(t, ctx, srv.URL, "payload"))
			if got := n.Load(); got != tc.wantReqs {
				t.Fatalf("requests = %d, want %d (err %v)", got, tc.wantReqs, err)
			}
			if tc.wantOK {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				resp.Body.Close()
				return
			}
			var le *Error
			if !errors.As(err, &le) || le.Code != tc.wantCode {
				t.Fatalf("err = %v, want code %s", err, tc.wantCode)
			}
		})
	}
}

func TestDoRetryWaitsForRetryAfter(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "0.15")
			w.WriteHeader(429)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	start := time.Now()
	resp, err := DoRetry(srv.Client(), postReq(t, WithRetry(context.Background(), fastRetry), srv.URL, "x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if d := time.Since(start); d < 140*time.Millisecond {
		t.Fatalf("retried after %v, want >= the 150ms Retry-After", d)
	}
}

func TestDoRetryCancelDuringBackoff(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(503)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(WithRetry(context.Background(), RetryPolicy{Attempts: 5, Base: time.Hour, Cap: time.Hour, MaxRetryAfter: time.Hour}))
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := DoRetry(srv.Client(), postReq(t, ctx, srv.URL, "x"))
	var le *Error
	if !errors.As(err, &le) || le.Code != ErrCanceled {
		t.Fatalf("err = %v, want canceled", err)
	}
	if n.Load() != 1 || time.Since(start) > 5*time.Second {
		t.Fatalf("requests = %d after %v", n.Load(), time.Since(start))
	}
}

func TestRetryWaitIsJitteredAndCapped(t *testing.T) {
	t.Parallel()
	p := RetryPolicy{Attempts: 10, Base: 100 * time.Millisecond, Cap: time.Second, MaxRetryAfter: 5 * time.Second}
	seen := map[time.Duration]bool{}
	for range 200 {
		d, ok := p.wait(3, &Error{Code: ErrServer, Status: 503}) // 400ms nominal
		if !ok || d < 200*time.Millisecond || d > 400*time.Millisecond {
			t.Fatalf("attempt 3 wait = %v, %v; want within [200ms, 400ms]", d, ok)
		}
		seen[d] = true
		if d, _ := p.wait(9, &Error{Code: ErrServer, Status: 503}); d > time.Second {
			t.Fatalf("wait %v exceeds the cap", d)
		}
	}
	if len(seen) < 20 {
		t.Fatalf("only %d distinct waits: no jitter", len(seen))
	}
	if d, _ := p.wait(1, &Error{Code: ErrRateLimit, Status: 429, RetryAfter: 3 * time.Second}); d < 3*time.Second {
		t.Fatalf("wait %v ignores Retry-After", d)
	}
	if _, ok := p.wait(10, &Error{Code: ErrServer, Status: 503}); ok {
		t.Fatal("waited after the last attempt")
	}
}

func TestDoRetryNeverReplaysAStartedStream(t *testing.T) {
	t.Parallel()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		// Break the stream mid-way: the client sees an unexpected EOF after a delta.
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()
	ctx := WithRetry(context.Background(), fastRetry)
	resp, err := DoRetry(srv.Client(), postReq(t, ctx, srv.URL, "x"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	err = ReadSSE(ctx, resp.Body, 0, func(e SSEEvent) error { got = append(got, string(e.Data)); return nil })
	resp.Body.Close()
	if err == nil || strings.Join(got, ",") != "first" {
		t.Fatalf("events %v err %v: want one delta then a read error", got, err)
	}
	if n.Load() != 1 {
		t.Fatalf("requests = %d: a stream that delivered data was retried", n.Load())
	}
}

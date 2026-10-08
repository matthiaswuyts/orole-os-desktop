package automations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func TestHTTPApprovalDisclosesRenderedRecipientAndHeaders(t *testing.T) {
	var effects atomic.Int32
	httpClient, dials := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != `{"recipient":"intended@example.com"}` || r.Header.Get("X-Operation") != "send" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("approved request changed: body=%q headers=%v error=%v", body, r.Header, err)
		}
		effects.Add(1)
		w.WriteHeader(http.StatusCreated)
	})
	e, shared := openPolicyEngine(t, filepath.Join(t.TempDir(), "db"))
	e.http = *httpClient
	d := saveDef(t, e, Definition{Name: "rendered recipient", Enabled: true, AllowedHosts: []string{"public.example"}, AllowHTTP: true, Steps: []Step{
		{ID: "send", Type: "http.request", Params: json.RawMessage(`{"method":"POST","url":"http://public.example/send","headers":{"Content-Type":"application/json","X-Operation":"{{ trigger.data.operation }}"},"body":"{\"recipient\":\"{{ trigger.data.recipient }}\"}"}`)},
	}})
	var previousDetail string
	for _, recipient := range []string{"intended@example.com", "attacker@example.com"} {
		r, err := e.trigger(context.Background(), d.ID, "webhook", map[string]any{"recipient": recipient, "operation": "send"}, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		pending := awaitPolicyApproval(t, shared)
		var disclosed httpRequestSnapshot
		if err := json.Unmarshal([]byte(pending.Detail), &disclosed); err != nil {
			t.Fatal(err)
		}
		wantBody := `{"recipient":"` + recipient + `"}`
		if disclosed.Method != "POST" || disclosed.URL != "http://public.example/send" || disclosed.Body != wantBody || disclosed.Headers.Get("X-Operation") != "send" || disclosed.Headers.Get("Content-Type") != "application/json" || disclosed.Headers.Get("X-Orole-Chain") == "" {
			t.Fatalf("incomplete concrete request disclosure: %s", pending.Detail)
		}
		allow := recipient == "intended@example.com"
		wantBefore := int32(0)
		if !allow {
			wantBefore = 1
			if pending.Detail == previousDetail {
				t.Fatal("body-only recipient change did not change approval")
			}
		}
		if dials.Load() != wantBefore || effects.Load() != wantBefore {
			t.Fatal("request dispatched before approval")
		}
		previousDetail = pending.Detail
		if err := shared.Resolve(context.Background(), pending.ID, policy.Resolution{Allow: allow}); err != nil {
			t.Fatal(err)
		}
		status := "failed"
		if allow {
			status = "succeeded"
		}
		awaitStatus(t, e, r.ID, status)
	}
	if dials.Load() != 1 || effects.Load() != 1 {
		t.Fatalf("denied body-only recipient dispatched: dials=%d effects=%d", dials.Load(), effects.Load())
	}
}

func TestHTTPAuthorityBodyAndHeadersFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		headers map[string]string
	}{
		{name: "registered body secret", body: `{"recipient":"private-value"}`},
		{name: "recognized body secret", body: `{"message":"Bearer opaque-value"}`},
		{name: "registered header secret", headers: map[string]string{"X-Recipient": "private-value"}},
		{name: "incomplete body", body: strings.Repeat("x", 2001)},
		{name: "incomplete header", headers: map[string]string{"X-Recipient": strings.Repeat("x", 2001)}},
		{name: "invisible body", body: "safe\u202eevil"},
		{name: "invisible header", headers: map[string]string{"X-Recipient": "safe\u202eevil"}},
		{name: "invalid UTF-8 body", body: "safe\xffevil"},
		{name: "invalid UTF-8 header", headers: map[string]string{"X-Recipient": "safe\xffevil"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var effects atomic.Int32
			httpClient, dials := httpTestClient(t, func(http.ResponseWriter, *http.Request) { effects.Add(1) })
			var approvals int
			e := &Engine{authorize: func(context.Context, policy.Request) error { approvals++; return nil }}
			e.RegisterSecret("private-value")
			_, err := httpClient.do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}, AllowHTTP: true}, HTTPRequest{
				Method: "POST", URL: "http://public.example/send", Body: tc.body, Headers: tc.headers,
			}, func(ctx context.Context, snapshot httpRequestSnapshot) error {
				return e.authorizeHTTP(ctx, "automation:test", snapshot)
			})
			var denied *authorizationError
			if !errors.As(err, &denied) || approvals != 0 || dials.Load() != 0 || effects.Load() != 0 {
				t.Fatalf("unsafe disclosure reached approval or dispatch: error=%v approvals=%d dials=%d effects=%d", err, approvals, dials.Load(), effects.Load())
			}
			if strings.Contains(err.Error(), "private-value") || strings.Contains(err.Error(), "opaque-value") {
				t.Fatalf("secret leaked through authorization error: %v", err)
			}
		})
	}
}

func TestHTTPRedirectApprovalUsesTransformedRequest(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, allowRedirect := range []bool{false, true} {
			name := http.StatusText(status) + "/denied"
			if allowRedirect {
				name = http.StatusText(status) + "/allowed"
			}
			t.Run(name, func(t *testing.T) {
				const body = `{"recipient":"intended@example.com"}`
				wantMethod, wantBody := "GET", ""
				if status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect {
					wantMethod, wantBody = "POST", body
				}
				var targetEffects atomic.Int32
				httpClient, dials := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/start" {
						w.Header().Set("Location", "/target")
						w.WriteHeader(status)
						return
					}
					gotBody, err := io.ReadAll(r.Body)
					if err != nil || r.Method != wantMethod || string(gotBody) != wantBody || r.Header.Get("X-Recipient") != "" {
						t.Errorf("redirect dispatch differs from approval: method=%s body=%q headers=%v error=%v", r.Method, gotBody, r.Header, err)
					}
					targetEffects.Add(1)
				})
				approvals := 0
				e := &Engine{authorize: func(_ context.Context, request policy.Request) error {
					approvals++
					var disclosed httpRequestSnapshot
					if err := json.Unmarshal([]byte(request.Detail), &disclosed); err != nil {
						t.Fatal(err)
					}
					if approvals == 1 {
						if disclosed.Method != "POST" || disclosed.Body != body || disclosed.Headers.Get("X-Recipient") != "original" {
							t.Fatalf("initial disclosure changed: %s", request.Detail)
						}
						return nil
					}
					if disclosed.URL != "http://public.example/target" || disclosed.Method != wantMethod || disclosed.Body != wantBody || len(disclosed.Headers) != 0 {
						t.Fatalf("redirect disclosure does not reflect transformation: %s", request.Detail)
					}
					if dials.Load() != 1 || targetEffects.Load() != 0 {
						t.Fatal("redirect dispatched before authorization")
					}
					if !allowRedirect {
						return errors.New("redirect denied")
					}
					return nil
				}}
				_, err := httpClient.do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}, AllowHTTP: true}, HTTPRequest{
					Method: "POST", URL: "http://public.example/start", Body: body, Headers: map[string]string{"X-Recipient": "original"},
				}, func(ctx context.Context, snapshot httpRequestSnapshot) error {
					return e.authorizeHTTP(ctx, "automation:test", snapshot)
				})
				wantDials, wantEffects := int32(1), int32(0)
				if allowRedirect {
					wantDials, wantEffects = 2, 1
				}
				if (err == nil) != allowRedirect || approvals != 2 || dials.Load() != wantDials || targetEffects.Load() != wantEffects {
					t.Fatalf("redirect authorization mismatch: error=%v approvals=%d dials=%d effects=%d", err, approvals, dials.Load(), targetEffects.Load())
				}
			})
		}
	}
}

func TestHTTPAuthorizationSnapshotCannotRewriteDispatch(t *testing.T) {
	const body = "original body"
	headers := map[string]string{"X-Recipient": "original"}
	httpClient, _ := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, err := io.ReadAll(r.Body)
		if err != nil || r.Method != "POST" || r.URL.Path != "/original" || string(gotBody) != body || r.Header.Get("X-Recipient") != "original" {
			t.Errorf("authorization mutated dispatch: method=%s url=%s body=%q headers=%v error=%v", r.Method, r.URL, gotBody, r.Header, err)
		}
	})
	_, err := httpClient.do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}, AllowHTTP: true}, HTTPRequest{
		Method: "POST", URL: "http://public.example/original", Body: body, Headers: headers,
	}, func(_ context.Context, snapshot httpRequestSnapshot) error {
		snapshot.Method, snapshot.URL, snapshot.Body = "DELETE", "http://public.example/changed", "changed"
		snapshot.Headers.Set("X-Recipient", "changed")
		headers["X-Recipient"] = "changed again"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

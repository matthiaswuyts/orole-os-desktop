package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func TestInvokeCheckedRejectsIncompleteDisclosure(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true, Setup: func(s *sdk.Server) {
		mcptest.AddJSONTool(s, "delete", "Delete a target", mcptest.Destructive(), nil, nil, func(map[string]any) (any, error) {
			calls.Add(1)
			return map[string]any{"ok": true}, nil
		})
	}})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	for _, args := range []map[string]any{
		{"aaa_padding": strings.Repeat("x", 1600), "zzz_target": "valuable-file"},
		{"target": "visible\u202ehidden"},
		{"unsupported": make(chan int)},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := h.m.InvokeChecked(ctx, st.ID, "delete", args, CallOptions{})
		cancel()
		if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "disclos") {
			t.Fatalf("expected immediate disclosure refusal, got %v", err)
		}
		if len(h.pol.Pending()) != 0 || calls.Load() != 0 {
			t.Fatal("incomplete disclosure reached approval or dispatch")
		}
	}
}

func TestInvokeCheckedDispatchesImmutableApprovedDocument(t *testing.T) {
	h := newHarness(t)
	received := make(chan map[string]any, 1)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true, Setup: func(s *sdk.Server) {
		mcptest.AddJSONTool(s, "delete", "Delete a target", mcptest.Destructive(), nil, nil, func(args map[string]any) (any, error) {
			received <- args
			return map[string]any{"ok": true}, nil
		})
	}})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	args := map[string]any{"nested": map[string]any{"target": "approved-file"}, "sequence": json.Number("9007199254740993")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := h.m.InvokeChecked(ctx, st.ID, "delete", args, CallOptions{})
		done <- err
	}()
	for {
		pending := h.pol.Pending()
		if len(pending) == 1 {
			if !strings.Contains(pending[0].Detail, `"target":"approved-file"`) || !strings.Contains(pending[0].Detail, "9007199254740993") {
				t.Fatalf("approval missing full document: %s", pending[0].Detail)
			}
			args["nested"].(map[string]any)["target"] = "unapproved-file"
			if err := h.pol.Resolve(ctx, pending[0].ID, policy.Resolution{Allow: true}); err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("call ended before approval: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := <-received
	if got["nested"].(map[string]any)["target"] != "approved-file" {
		t.Fatalf("dispatched caller mutation: %+v", got)
	}
}

type secretEventRecorder struct {
	mu   sync.Mutex
	data []byte
}

func (r *secretEventRecorder) Publish(event string, value any) {
	b, _ := json.Marshal(value)
	r.mu.Lock()
	r.data = append(r.data, append([]byte(event), b...)...)
	r.mu.Unlock()
}

func (r *secretEventRecorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.data)
}

func TestOAuthExchangeDoesNotReflectProviderErrors(t *testing.T) {
	for _, field := range []string{"code", "client_secret", "code_verifier"} {
		t.Run(field, func(t *testing.T) {
			h := newHarness(t)
			var logs bytes.Buffer
			h.m.log = slog.New(slog.NewTextHandler(&logs, nil))
			recorder := &secretEventRecorder{}
			h.m.pub = recorder
			token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": r.Form.Get(field), "error_description": r.Form.Get("client_secret")})
			}))
			defer token.Close()
			st, err := h.m.Add(context.Background(), AddParams{Name: "exchange", URL: token.URL, AuthKind: AuthOAuth})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.m.SetEnabled(context.Background(), st.ID, false); err != nil {
				t.Fatal(err)
			}
			cfg, err := h.m.server(context.Background(), st.ID)
			if err != nil {
				t.Fatal(err)
			}
			ln, err := listenLoopback()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			flow := &authFlow{cancel: cancel, done: make(chan struct{})}
			redirect := "http://" + ln.Addr().String() + "/callback"
			secret, code, verifier := "opaque-client-material-42", "opaque-auth-code-43", "opaque-verifier-44"
			disc := &Discovery{Resource: token.URL, AS: &ASMetadata{TokenEndpoint: token.URL}}
			go h.m.runFlow(ctx, flow, ln, cfg, disc, clientReg{clientID: "registered", secret: secret, style: "params"}, redirect, "expected-state", verifier, nil)
			hit(t, redirect+"?state=expected-state&code="+url.QueryEscape(code))
			select {
			case <-flow.done:
			case <-time.After(5 * time.Second):
				t.Fatal("exchange did not finish")
			}
			var audit string
			if err := h.db.QueryRow(`SELECT detail FROM audit_events WHERE action='mcp.auth.finish' AND target=?`, "mcp/"+st.ID).Scan(&audit); err != nil {
				t.Fatal(err)
			}
			state, err := h.m.Get(context.Background(), st.ID)
			if err != nil {
				t.Fatal(err)
			}
			for sink, text := range map[string]string{"logs": logs.String(), "audit": audit, "events": recorder.text(), "status": state.LastError} {
				if !strings.Contains(text, "HTTP 400 unknown_error") {
					t.Errorf("%s missing safe diagnosis: %s", sink, text)
				}
				for _, value := range []string{secret, code, verifier} {
					if strings.Contains(text, value) {
						t.Errorf("credential reflected into %s", sink)
					}
				}
			}
		})
	}
}

func TestOAuthRefreshErrorIdentifiersAreWhitelisted(t *testing.T) {
	for _, code := range []string{"invalid_grant", "invalid_client", "unauthorized_client", "temporarily_unavailable", "opaque-refresh-material-45"} {
		t.Run(code, func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": r.Form.Get("refresh_token")})
			}))
			defer endpoint.Close()
			store := credentials.NewMemory()
			blob := &credBlob{Kind: "oauth", AccessToken: "opaque-access-material-46", RefreshToken: "opaque-refresh-material-45", ClientSecret: "opaque-client-material-47", TokenEndpoint: endpoint.URL, Expiry: time.Now().Add(-time.Minute)}
			raw, _ := json.Marshal(blob)
			if err := store.Set(credKey("test"), raw); err != nil {
				t.Fatal(err)
			}
			a := newAuthSession("test", store, nil2(), func(context.Context) (string, error) { return endpoint.URL, nil })
			_, err := a.AccessToken(context.Background())
			if err == nil || strings.Contains(err.Error(), blob.RefreshToken) || strings.Contains(err.Error(), blob.ClientSecret) {
				t.Fatalf("unsafe refresh error: %v", err)
			}
			reauth := code == "invalid_grant" || code == "invalid_client" || code == "unauthorized_client"
			if errors.Is(err, ErrReauthRequired) != reauth {
				t.Fatalf("refresh classification changed: %v", err)
			}
			has, _, _, _ := a.Snapshot()
			if has == reauth {
				t.Fatal("refresh rejection did not preserve credential-clearing semantics")
			}
			if code == blob.RefreshToken && err.Error() != "token endpoint: HTTP 400 unknown_error" {
				t.Fatalf("unexpected safe unknown error: %v", err)
			}
		})
	}
}

func TestMCPResultScrubsRefreshedAndRetiredOAuthCredentials(t *testing.T) {
	h := newHarness(t)
	var id string
	var original credBlob
	srv := mcptest.New(t, mcptest.Options{DCR: true, Setup: func(s *sdk.Server) {
		mcptest.AddJSONTool(s, "diagnostic", "Authentication diagnostic", mcptest.ReadOnly(), nil, nil, func(map[string]any) (any, error) {
			raw, err := h.creds.Get(credKey(id))
			if err != nil {
				return nil, err
			}
			var current credBlob
			if err := json.Unmarshal(raw, &current); err != nil {
				return nil, err
			}
			return map[string]any{"message": original.AccessToken + " " + original.RefreshToken + " " + current.AccessToken + " " + current.RefreshToken}, nil
		})
	}})
	st := h.add(t, srv, AddParams{})
	id = st.ID
	h.authorize(t, srv, id)
	raw, err := h.creds.Get(credKey(id))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	// The real SDK handles the 401 and refreshes before retrying the tool.
	srv.InvalidateAccessTokens()
	result, err := h.m.CallTool(context.Background(), id, "diagnostic", nil, CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = h.creds.Get(credKey(id))
	if err != nil {
		t.Fatal(err)
	}
	var current credBlob
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	if current.AccessToken == original.AccessToken {
		t.Fatal("production request did not rotate credentials")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{original.AccessToken, original.RefreshToken, current.AccessToken, current.RefreshToken} {
		if value == "" || bytes.Contains(encoded, []byte(value)) {
			t.Fatal("OAuth credential escaped the MCP output boundary")
		}
	}
	if !bytes.Contains(encoded, []byte("[REDACTED]")) {
		t.Fatal("diagnostic content was not preserved with redaction")
	}
}

func TestMCPProtocolErrorScrubsOpaqueBearer(t *testing.T) {
	h := newHarness(t)
	secret := "opaque-transport-credential-48"
	srv := mcptest.New(t, mcptest.Options{Setup: func(s *sdk.Server) {
		s.AddTool(&sdk.Tool{Name: "fail", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return nil, errors.New("diagnostic " + secret)
		})
	}})
	srv.AddStaticToken(secret)
	st := h.add(t, srv, AddParams{AuthKind: AuthBearer, BearerToken: secret})
	h.wait(t, st.ID, StatusConnected)
	_, err := h.m.CallTool(context.Background(), st.ID, "fail", nil, CallOptions{})
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("unsafe protocol error: %v", err)
	}
}

func TestMCPRedactsBeforeResultTruncation(t *testing.T) {
	h := newHarness(t)
	secret := "opaque-boundary-credential-49"
	srv := mcptest.New(t, mcptest.Options{Setup: func(s *sdk.Server) {
		s.AddTool(&sdk.Tool{Name: "boundary", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("x", maxResultText-8) + secret}}}, nil
		})
	}})
	srv.AddStaticToken(secret)
	st := h.add(t, srv, AddParams{AuthKind: AuthBearer, BearerToken: secret})
	h.wait(t, st.ID, StatusConnected)
	out, err := h.m.CallTool(context.Background(), st.ID, "boundary", nil, CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Content) != 1 || !out.Content[0].Truncated || strings.Contains(out.Content[0].Text, secret[:8]) {
		t.Fatal("truncation retained a credential prefix")
	}
}

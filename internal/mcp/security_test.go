package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func TestCancelCallWhileApprovalPending(t *testing.T) {
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	s := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, s.ID, StatusConnected)
	done := make(chan error, 1)
	go func() {
		_, err := h.m.InvokeChecked(context.Background(), s.ID, "delete_note", nil, CallOptions{CallID: "pending-call", Actor: "test"})
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for len(h.pol.Pending()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(h.pol.Pending()) != 1 {
		t.Fatal("approval not queued")
	}
	if !h.m.CancelCall("pending-call") {
		t.Fatal("could not cancel pending approval")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not unblock approval")
	}
	if len(h.pol.Pending()) != 0 {
		t.Fatal("approval leaked")
	}
}

func TestConservativeAnnotationDefaults(t *testing.T) {
	if classify(false, nil) != policy.EffectDestructive {
		t.Fatal("missing destructive hint must default true")
	}
	if classify(false, new(false)) != policy.EffectWrite {
		t.Fatal("explicit additive hint")
	}
	if classify(true, nil) != policy.EffectRead {
		t.Fatal("readonly hint")
	}
}

func TestRedirectsCannotForwardTokenPOST(t *testing.T) {
	parse := func(s string) *url.URL { u, _ := url.Parse(s); return u }
	first := &http.Request{Method: http.MethodPost, URL: parse("https://issuer.example/token")}
	target := &http.Request{Method: http.MethodPost, URL: parse("https://attacker.example/capture")}
	if oauthRedirect(target, []*http.Request{first}) == nil {
		t.Fatal("credential POST redirect accepted")
	}
	first.Method = http.MethodGet
	target.URL = parse("http://127.0.0.1/callback")
	if oauthRedirect(target, []*http.Request{first}) == nil {
		t.Fatal("metadata redirect into loopback accepted")
	}
	target.URL = parse("https://issuer.example/metadata")
	if err := oauthRedirect(target, []*http.Request{first}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorityRevokedDuringApprovalPreventsRemoteEffect(t *testing.T) {
	h := newHarness(t)
	note := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(note, []byte("keep until authorized"), 0600); err != nil {
		t.Fatal(err)
	}
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true, Setup: func(s *sdk.Server) {
		mcptest.AddJSONTool(s, "delete_note", "Delete the test note.", mcptest.Destructive(), nil, nil,
			func(map[string]any) (any, error) { return nil, os.Remove(note) })
	}})
	server := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, server.ID, StatusConnected)
	invoke := func(opts CallOptions) error {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := h.m.InvokeChecked(ctx, server.ID, "delete_note", nil, opts)
			done <- err
		}()
		for {
			pending := h.pol.Pending()
			if len(pending) == 1 {
				if err := h.pol.Resolve(ctx, pending[0].ID, policy.Resolution{Allow: true}); err != nil {
					t.Fatal(err)
				}
				break
			}
			select {
			case err := <-done:
				t.Fatalf("invocation ended before approval: %v", err)
			case <-ctx.Done():
				t.Fatal("approval not queued")
			case <-time.After(time.Millisecond):
			}
		}
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			t.Fatal("approved invocation did not finish")
			return ctx.Err()
		}
	}
	revoked := errors.New("plugin authority revoked")
	if err := invoke(CallOptions{Actor: "plugin:test", BeforeCall: func(context.Context) error { return revoked }}); !errors.Is(err, revoked) {
		t.Fatalf("revoked authority: %v", err)
	}
	if data, err := os.ReadFile(note); err != nil || string(data) != "keep until authorized" {
		t.Fatalf("revoked invocation affected the remote file: %q %v", data, err)
	}
	if err := invoke(CallOptions{Actor: "plugin:test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(note); !os.IsNotExist(err) {
		t.Fatalf("authorized invocation did not remove the remote note: %v", err)
	}
}

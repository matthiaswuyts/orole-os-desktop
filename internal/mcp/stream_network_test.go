package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
)

// Negotiate a session-bearing protocol that actually opens the standalone GET.
// Newer MCP versions remove that stream, so a latest-only fixture misses this bug.
func mcpStreamTestHandler(t *testing.T, redirect string, gets *atomic.Int32) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.Header.Get("Mcp-Session-Id") != "stream-test" || r.Header.Get("Accept") != "text/event-stream" {
				t.Error("session GET missing MCP stream headers")
			}
			gets.Add(1)
			if redirect != "" {
				http.Redirect(w, r, redirect, http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPost:
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(request.ID) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
			switch request.Method {
			case "server/discover":
				response["error"] = map[string]any{"code": -32601, "message": "method not found"}
			case "initialize":
				w.Header().Set("Mcp-Session-Id", "stream-test")
				response["result"] = map[string]any{
					"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
					"serverInfo": map[string]any{"name": "stream-test", "version": "1"},
				}
			default:
				response["result"] = map[string]any{}
			}
			oauthNetworkTestJSON(w, response)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func TestMCPPublicSessionRedirectsRejectPrivateTargetsBeforeDial(t *testing.T) {
	for _, target := range []string{
		"https://127.0.0.1/private", "https://10.0.0.9/private",
		"https://169.254.169.254/private", "https://[::1]/private",
		"https://private.example/private",
	} {
		t.Run(target, func(t *testing.T) {
			var gets atomic.Int32
			base, allowed, forbidden := oauthNetworkTestClient(t, "8.8.8.8:443", mcpStreamTestHandler(t, target, &gets))
			network, err := newOAuthNetwork("https://8.8.8.8/mcp")
			if err != nil {
				t.Fatal(err)
			}
			var lookups atomic.Int32
			network.resolver = oauthTestResolver(func(_ context.Context, host string) ([]net.IPAddr, error) {
				if host != "private.example" {
					return nil, fmt.Errorf("unexpected DNS query %q", host)
				}
				lookups.Add(1)
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
			})
			hc, err := network.streamClient(base)
			if err != nil {
				t.Fatal(err)
			}
			defer hc.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: "https://8.8.8.8/mcp", HTTPClient: hc, MaxRetries: -1}, &sdk.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
			if session != nil {
				stop := context.AfterFunc(ctx, func() { _ = session.Close() })
				err = session.Wait()
				stop()
				_ = session.Close()
			}
			// The SDK wraps the standalone-stream error with %v, not %w.
			if err == nil || !strings.Contains(err.Error(), ErrUnsafeURL.Error()) || gets.Load() == 0 || allowed.Load() == 0 || forbidden.Load() != 0 {
				t.Fatalf("err=%v stream GETs=%d allowed dials=%d forbidden dials=%d", err, gets.Load(), allowed.Load(), forbidden.Load())
			}
			if strings.Contains(target, "private.example") && lookups.Load() == 0 {
				t.Fatal("private redirect DNS was not resolved by the guard")
			}
		})
	}
}

func TestMCPStreamDNSRestrictionsCoverEveryRequest(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		for _, addresses := range [][]string{{"127.0.0.1"}, {"169.254.169.254"}, {"8.8.8.8", "10.0.0.1"}} {
			t.Run(method+"/"+strings.Join(addresses, ","), func(t *testing.T) {
				var dials atomic.Int32
				base := &http.Client{Timeout: time.Second, Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
					dials.Add(1)
					return nil, errors.New("unexpected dial")
				}}}
				network, err := newOAuthNetwork("https://resource.example/mcp")
				if err != nil {
					t.Fatal(err)
				}
				network.resolver = oauthTestResolver(func(context.Context, string) ([]net.IPAddr, error) {
					var ips []net.IPAddr
					for _, address := range addresses {
						ips = append(ips, net.IPAddr{IP: net.ParseIP(address)})
					}
					return ips, nil
				})
				hc, err := network.streamClient(base)
				if err != nil {
					t.Fatal(err)
				}
				defer hc.CloseIdleConnections()
				if hc.Timeout != 0 || base.Timeout != time.Second {
					t.Fatal("stream timeout must be removed without mutating the OAuth client")
				}
				request, err := http.NewRequest(method, "https://resource.example/mcp", nil)
				if err != nil {
					t.Fatal(err)
				}
				_, err = hc.Do(request)
				if !errors.Is(err, ErrUnsafeURL) || dials.Load() != 0 || network.local {
					t.Fatalf("err=%v dials=%d local=%t", err, dials.Load(), network.local)
				}
			})
		}
	}
}

func TestMCPManagerUsesPerServerStreamTrust(t *testing.T) {
	var gets atomic.Int32
	base, _, forbidden := oauthNetworkTestClient(t, "8.8.8.8:443", mcpStreamTestHandler(t, "https://10.0.0.1/private", &gets))
	manager := New(Options{HTTP: base, Creds: credentials.NewMemory(), ConnectTimeout: 5 * time.Second})
	remote := &conn{m: manager, cfg: ServerConfig{ID: "public", URL: "https://8.8.8.8/mcp"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, hc, err := remote.dial(ctx)
	if session != nil {
		stop := context.AfterFunc(ctx, func() { _ = session.Close() })
		err = session.Wait()
		stop()
		_ = session.Close()
		hc.CloseIdleConnections()
	}
	if err == nil || !strings.Contains(err.Error(), ErrUnsafeURL.Error()) || gets.Load() == 0 || forbidden.Load() != 0 {
		t.Fatalf("manager bypassed stream guard: err=%v GETs=%d forbidden dials=%d", err, gets.Load(), forbidden.Load())
	}

	var localGets atomic.Int32
	localServer := httptest.NewServer(mcpStreamTestHandler(t, "", &localGets))
	defer localServer.Close()
	// The same manager must derive local trust anew from this configured URL.
	manager.hc = &http.Client{Timeout: time.Nanosecond}
	local := &conn{m: manager, cfg: ServerConfig{ID: "local", URL: localServer.URL}}
	session, hc, err = local.dial(ctx)
	if err != nil {
		t.Fatalf("explicit local server failed: %v", err)
	}
	defer hc.CloseIdleConnections()
	defer session.Close()
	if err := session.Ping(ctx, nil); err != nil {
		t.Fatalf("local session POST failed: %v", err)
	}
	if localGets.Load() == 0 || hc.Timeout != 0 {
		t.Fatalf("local stream GETs=%d timeout=%s", localGets.Load(), hc.Timeout)
	}
}

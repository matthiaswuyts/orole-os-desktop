package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
)

type oauthTestResolver func(context.Context, string) ([]net.IPAddr, error)

func (f oauthTestResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

// Only the expected numeric address is mapped to this in-process TLS server.
// Every other dial is counted and stopped, so these tests cannot use live DNS or
// accidentally contact a private/public network when the guard regresses.
func oauthNetworkTestClient(t *testing.T, address string, handler http.Handler) (*http.Client, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "127.0.0.1"
	allowed, forbidden := &atomic.Int32{}, &atomic.Int32{}
	transport.DialContext = func(ctx context.Context, network, target string) (net.Conn, error) {
		if target != address {
			forbidden.Add(1)
			return nil, errors.New("unexpected OAuth test destination")
		}
		allowed.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	client.Transport = transport
	t.Cleanup(client.CloseIdleConnections)
	return client, allowed, forbidden
}

func oauthNetworkTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func TestOAuthPublicDiscoveryRejectsLocalLinksBeforeDial(t *testing.T) {
	const origin = "https://8.8.8.8"
	targets := []string{
		"http://127.0.0.1:45678/private",
		"http://localhost:45678/private",
		"https://10.0.0.9/private",
		"https://172.16.0.9/private",
		"https://192.168.1.9/private",
		"https://169.254.169.254/private",
		"https://100.64.0.1/private",
		"https://[::1]/private",
		"https://[::ffff:127.0.0.1]/private",
		"https://[fd00::9]/private",
		"https://private.example/private",
	}
	stages := []string{"challenge", "issuer", "authorization_endpoint", "token_endpoint", "registration_endpoint", "revocation_endpoint"}
	for _, stage := range stages {
		for _, target := range targets {
			t.Run(stage+"/"+target, func(t *testing.T) {
				client, _, forbidden := oauthNetworkTestClient(t, "8.8.8.8:443", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/mcp":
						metadata := origin + "/prm"
						if stage == "challenge" {
							metadata = target
						}
						w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q`, metadata))
						w.WriteHeader(http.StatusUnauthorized)
					case "/prm":
						issuer := origin
						if stage == "issuer" {
							issuer = target
						}
						oauthNetworkTestJSON(w, map[string]any{"resource": origin + "/mcp", "authorization_servers": []string{issuer}})
					case "/.well-known/oauth-authorization-server":
						metadata := map[string]any{
							"issuer": origin, "authorization_endpoint": origin + "/authorize", "token_endpoint": origin + "/token",
							"registration_endpoint": origin + "/register", "code_challenge_methods_supported": []string{"S256"},
						}
						metadata[stage] = target
						oauthNetworkTestJSON(w, metadata)
					default:
						http.NotFound(w, r)
					}
				}))
				network, err := newOAuthNetwork(origin + "/mcp")
				if err != nil {
					t.Fatal(err)
				}
				network.resolver = oauthTestResolver(func(_ context.Context, host string) ([]net.IPAddr, error) {
					if host != "private.example" {
						return nil, fmt.Errorf("unexpected DNS query %q", host)
					}
					return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
				})
				discovery, err := network.discover(context.Background(), client, origin+"/mcp")
				if !errors.Is(err, ErrUnsafeURL) {
					t.Fatalf("unsafe link not rejected: %v; discovery %+v", err, discovery)
				}
				if forbidden.Load() != 0 {
					t.Fatalf("unsafe target reached dialer %d times", forbidden.Load())
				}
				if len(discovery.Attempts) == 0 || discovery.Attempts[len(discovery.Attempts)-1].Result == "ok" {
					t.Fatalf("rejected discovery hop missing from diagnostics: %+v", discovery.Attempts)
				}
			})
		}
	}
}

func TestOAuthPublicMetadataRedirectsRejectLocalTargetsBeforeDial(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1:45678/metadata", "https://192.168.1.2/metadata", "https://private.example/metadata"} {
		t.Run(target, func(t *testing.T) {
			client, _, forbidden := oauthNetworkTestClient(t, "8.8.8.8:443", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/mcp" {
					w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://8.8.8.8/metadata"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, target, http.StatusFound)
			}))
			network, err := newOAuthNetwork("https://8.8.8.8/mcp")
			if err != nil {
				t.Fatal(err)
			}
			network.resolver = oauthTestResolver(func(context.Context, string) ([]net.IPAddr, error) {
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
			})
			_, err = network.discover(context.Background(), client, "https://8.8.8.8/mcp")
			if !errors.Is(err, ErrUnsafeURL) || forbidden.Load() != 0 {
				t.Fatalf("redirect error = %v, forbidden dials = %d", err, forbidden.Load())
			}
		})
	}
}

func TestOAuthDNSAnswersAreValidatedBeforeDial(t *testing.T) {
	for _, addresses := range [][]string{
		{"127.0.0.1"}, {"10.0.0.1"}, {"::1"}, {"fd00::1"}, {"fe80::1"},
		{"8.8.8.8", "192.168.1.1"}, {"2001:4860:4860::8888", "::ffff:127.0.0.1"},
	} {
		t.Run(strings.Join(addresses, ","), func(t *testing.T) {
			var dials atomic.Int32
			base := &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				return nil, errors.New("unexpected dial")
			}}}
			network, err := newOAuthNetwork("https://resource.example/mcp")
			if err != nil {
				t.Fatal(err)
			}
			network.resolver = oauthTestResolver(func(context.Context, string) ([]net.IPAddr, error) {
				ips := make([]net.IPAddr, 0, len(addresses))
				for _, address := range addresses {
					ips = append(ips, net.IPAddr{IP: net.ParseIP(address)})
				}
				return ips, nil
			})
			_, err = network.discover(context.Background(), base, "https://resource.example/mcp")
			if !errors.Is(err, ErrUnsafeURL) || dials.Load() != 0 || network.local {
				t.Fatalf("DNS granted local trust: err=%v dials=%d local=%t", err, dials.Load(), network.local)
			}
		})
	}
}

func TestOAuthRedirectDNSRebindingCannotChangePinnedDestination(t *testing.T) {
	var targetRequests atomic.Int32
	client, allowed, forbidden := oauthNetworkTestClient(t, "8.8.8.8:443", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metadata" {
			// Force another TCP dial for the redirect to exercise rebinding, rather
			// than safely reusing a connection already pinned to the public address.
			w.Header().Set("Connection", "close")
			http.Redirect(w, r, "https://resource.example/target", http.StatusFound)
			return
		}
		targetRequests.Add(1)
		oauthNetworkTestJSON(w, map[string]any{})
	}))
	network, err := newOAuthNetwork("https://resource.example/mcp")
	if err != nil {
		t.Fatal(err)
	}
	var lookups atomic.Int32
	network.resolver = oauthTestResolver(func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "resource.example" {
			return nil, errors.New("unexpected DNS query")
		}
		if lookups.Add(1) == 1 {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})
	guarded, err := network.client(client)
	if err != nil {
		t.Fatal(err)
	}
	defer guarded.CloseIdleConnections()
	var document map[string]any
	err = getJSON(context.Background(), guarded, "https://resource.example/metadata", &document)
	if !errors.Is(err, ErrUnsafeURL) || allowed.Load() != 1 || forbidden.Load() != 0 || lookups.Load() != 2 || targetRequests.Load() != 0 {
		t.Fatalf("rebind err=%v allowed=%d forbidden=%d lookups=%d target requests=%d", err, allowed.Load(), forbidden.Load(), lookups.Load(), targetRequests.Load())
	}
}

func TestOAuthRegistrationExchangeAndRefreshKeepConfiguredTrust(t *testing.T) {
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		oauthNetworkTestJSON(w, map[string]any{"client_id": "client", "access_token": "access"})
	}))
	defer target.Close()
	for _, operation := range []string{"registration", "exchange", "refresh"} {
		t.Run(operation, func(t *testing.T) {
			store := credentials.NewMemory()
			var dials atomic.Int32
			client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				// This is always a local fixture, never an external network.
				return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
			}}}
			ctx := context.Background()
			var err error
			switch operation {
			case "registration":
				manager := New(Options{HTTP: client, Creds: store})
				_, err = manager.registerClient(ctx, &ASMetadata{RegistrationEndpoint: target.URL + "/register"}, "http://127.0.0.1/callback", ServerConfig{URL: "https://public.example/mcp"})
			case "exchange":
				_, err = oauthPostToken(ctx, client, "https://public.example/mcp", target.URL+"/token", "client", "secret-not-for-logs", "params", url.Values{"code": {"code-not-for-logs"}})
			case "refresh":
				session := newAuthSession("test", store, client, func(context.Context) (string, error) { return "https://public.example/mcp", nil })
				// A stored resource that claims local trust must not override the
				// actual configured source, including credentials saved before this fix.
				err = session.Set(&credBlob{Kind: "oauth", AccessToken: "old", RefreshToken: "refresh-not-for-logs", Expiry: time.Now().Add(-time.Hour), TokenEndpoint: target.URL + "/token", Resource: target.URL + "/mcp"})
				if err == nil {
					_, err = session.AccessToken(ctx)
				}
			}
			if !errors.Is(err, ErrUnsafeURL) || dials.Load() != 0 || requests.Load() != 0 {
				t.Fatalf("%s: err=%v dials=%d target requests=%d", operation, err, dials.Load(), requests.Load())
			}
			for _, secret := range []string{"secret-not-for-logs", "code-not-for-logs", "refresh-not-for-logs"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("network rejection exposed OAuth material")
				}
			}
		})
	}
}

func TestOAuthExplicitLocalDiscoveryAuthorizationAndRefresh(t *testing.T) {
	for _, preregistered := range []bool{false, true} {
		t.Run(fmt.Sprintf("preregistered=%t", preregistered), func(t *testing.T) {
			h := newHarness(t)
			options := mcptest.Options{DCR: !preregistered}
			params := AddParams{}
			if preregistered {
				options.ClientID = "real-service-style-client"
				options.ClientSecret = "local-client-secret"
				params.OAuthClientID = options.ClientID
				params.ClientSecret = options.ClientSecret
			}
			server := mcptest.New(t, options)
			state := h.add(t, server, params)
			start := h.authorize(t, server, state.ID)
			wantKind := "dcr"
			if preregistered {
				wantKind = "preregistered"
			}
			if start.Kind != wantKind {
				t.Fatalf("registration kind = %q, want %q", start.Kind, wantKind)
			}
			session := h.m.authSession(state.ID)
			session.mu.Lock()
			blob, err := session.loadLocked()
			if err == nil && blob != nil {
				blob.Expiry = time.Now().Add(-time.Hour)
			}
			session.mu.Unlock()
			if err != nil || blob == nil {
				t.Fatalf("load authorized credentials: %v", err)
			}
			if token, err := session.AccessToken(context.Background()); err != nil || token == "" {
				t.Fatalf("local refresh failed: %v", err)
			}
		})
	}
}

func TestOAuthExplicitPrivateHTTPSIntegration(t *testing.T) {
	const origin = "https://10.0.0.8"
	client, allowed, forbidden := oauthNetworkTestClient(t, "10.0.0.8:443", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+origin+`/prm"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/prm":
			oauthNetworkTestJSON(w, map[string]any{"resource": origin + "/mcp", "authorization_servers": []string{origin}})
		case "/.well-known/oauth-authorization-server":
			oauthNetworkTestJSON(w, map[string]any{
				"issuer": origin, "authorization_endpoint": origin + "/authorize", "token_endpoint": origin + "/token",
				"registration_endpoint": origin + "/register", "code_challenge_methods_supported": []string{"S256"},
			})
		case "/register":
			oauthNetworkTestJSON(w, map[string]any{"client_id": "local-client"})
		case "/token":
			oauthNetworkTestJSON(w, map[string]any{"access_token": "local-access", "token_type": "Bearer"})
		default:
			http.NotFound(w, r)
		}
	}))
	ctx := context.Background()
	discovery, err := Discover(ctx, client, origin+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	manager := New(Options{HTTP: client})
	reg, err := manager.registerClient(ctx, discovery.AS, "http://127.0.0.1/callback", ServerConfig{URL: origin + "/mcp"})
	if err != nil || reg.clientID != "local-client" {
		t.Fatalf("local registration: %v", err)
	}
	if _, err := oauthPostToken(ctx, client, origin+"/mcp", discovery.AS.TokenEndpoint, reg.clientID, "", "params", url.Values{"grant_type": {"authorization_code"}}); err != nil {
		t.Fatal(err)
	}
	if allowed.Load() == 0 || forbidden.Load() != 0 {
		t.Fatalf("private integration allowed=%d forbidden=%d", allowed.Load(), forbidden.Load())
	}
}

func TestOAuthLocalTrustSurvivesPublicMetadataRedirect(t *testing.T) {
	var targetURL string
	var targetRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metadata" {
			http.Redirect(w, r, targetURL, http.StatusFound)
			return
		}
		targetRequests.Add(1)
		oauthNetworkTestJSON(w, map[string]any{"local": true})
	}))
	defer server.Close()
	targetURL = server.URL + "/target"
	base := server.Client()
	transport := base.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "127.0.0.1"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" && address != server.Listener.Addr().String() {
			return nil, errors.New("unexpected test destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	base.Transport = transport
	base.CheckRedirect = oauthRedirect
	network, err := newOAuthNetwork("http://127.0.0.1:45678/mcp")
	if err != nil {
		t.Fatal(err)
	}
	client, err := network.client(base)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	var metadata map[string]any
	if err := getJSON(context.Background(), client, "https://8.8.8.8/metadata", &metadata); err != nil {
		t.Fatal(err)
	}
	if targetRequests.Load() != 1 || metadata["local"] != true {
		t.Fatalf("local metadata redirect failed: requests=%d metadata=%v", targetRequests.Load(), metadata)
	}
}

func TestOAuthPublicTransportCannotBypassGuardThroughProxyOrTLSDial(t *testing.T) {
	base, allowed, forbidden := oauthNetworkTestClient(t, "8.8.8.8:443", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oauthNetworkTestJSON(w, map[string]any{"public": true})
	}))
	transport := base.Transport.(*http.Transport)
	var bypasses atomic.Int32
	transport.Proxy = func(*http.Request) (*url.URL, error) {
		bypasses.Add(1)
		return url.Parse("http://127.0.0.1:45678")
	}
	transport.DialTLSContext = func(context.Context, string, string) (net.Conn, error) {
		bypasses.Add(1)
		return nil, errors.New("unguarded TLS dial")
	}
	network, err := newOAuthNetwork("https://8.8.8.8/mcp")
	if err != nil {
		t.Fatal(err)
	}
	client, err := network.client(base)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	var metadata map[string]any
	if err := getJSON(context.Background(), client, "https://8.8.8.8/metadata", &metadata); err != nil {
		t.Fatal(err)
	}
	if allowed.Load() != 1 || forbidden.Load() != 0 || bypasses.Load() != 0 || metadata["public"] != true {
		t.Fatalf("public transport: allowed=%d forbidden=%d bypasses=%d metadata=%v", allowed.Load(), forbidden.Load(), bypasses.Load(), metadata)
	}
}

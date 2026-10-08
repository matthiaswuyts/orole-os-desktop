package automations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type httpTestResolver func(context.Context, string) ([]net.IPAddr, error)

func (f httpTestResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

func httpTestIPs(addresses ...string) []net.IPAddr {
	ips := make([]net.IPAddr, len(addresses))
	for i, address := range addresses {
		ips[i] = net.IPAddr{IP: net.ParseIP(address)}
	}
	return ips
}

func httpTestClient(t *testing.T, handler http.HandlerFunc) (*SafeHTTP, *atomic.Int32) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	calls := new(atomic.Int32)
	return &SafeHTTP{
		Resolver: httpTestResolver(func(context.Context, string) ([]net.IPAddr, error) {
			return httpTestIPs("8.8.8.8"), nil
		}),
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			calls.Add(1)
			if address != "8.8.8.8:80" {
				return nil, fmt.Errorf("dial did not receive pinned public IP: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}, calls
}

func TestSafeHTTPPublicRequest(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	s, calls := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"method": r.Method, "body": string(body), "host": r.Host,
			"header": r.Header.Get("X-Request-ID"), "query": r.URL.RawQuery,
		})
	})
	result, err := s.Do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}, AllowHTTP: true}, HTTPRequest{
		Method: "POST", URL: "http://public.example/path?q=1", Body: "payload",
		Headers: map[string]string{"X-Request-ID": "request-id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != http.StatusCreated || result.Truncated || calls.Load() != 1 {
		t.Fatalf("unexpected response: %+v, dials=%d", result, calls.Load())
	}
	parsed, ok := result.JSON.(map[string]any)
	if !ok || parsed["method"] != "POST" || parsed["body"] != "payload" || parsed["host"] != "public.example" || parsed["header"] != "request-id" || parsed["query"] != "q=1" {
		t.Fatalf("request was not preserved: %#v", result.JSON)
	}
}

func TestSafeHTTPURLPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		url     string
		hosts   []string
		http    bool
		allowed bool
	}{
		{"https default", "https://api.example.com/", []string{"api.example.com"}, false, true},
		{"http default denied", "http://api.example.com/", []string{"api.example.com"}, false, false},
		{"http explicit", "http://api.example.com/", []string{"api.example.com"}, true, true},
		{"no hosts", "https://api.example.com/", nil, false, false},
		{"wildcard subdomain", "https://api.example.com/", []string{"*.example.com"}, false, true},
		{"wildcard nested", "https://a.api.example.com/", []string{"*.example.com"}, false, true},
		{"wildcard not apex", "https://example.com/", []string{"*.example.com"}, false, false},
		{"wildcard boundary", "https://badexample.com/", []string{"*.example.com"}, false, false},
		{"wildcard not suffix", "https://example.com.attacker.test/", []string{"*.example.com"}, false, false},
		{"bare wildcard", "https://example.com/", []string{"*"}, false, false},
		{"host canonicalization", "https://API.EXAMPLE.COM./", []string{"api.example.com"}, false, true},
		{"credentials", "https://user:pass@api.example.com/", []string{"api.example.com"}, false, false},
		{"empty credentials", "https://@api.example.com/", []string{"api.example.com"}, false, false},
		{"wrong scheme", "ftp://api.example.com/", []string{"api.example.com"}, true, false},
		{"missing host", "https:///path", []string{""}, false, false},
		{"zone", "https://[fe80::1%25eth0]/", []string{"fe80::1%eth0"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			err = validateHTTPURL(u, HTTPPolicy{AllowedHosts: tc.hosts, AllowHTTP: tc.http})
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v, error=%v", tc.allowed, err)
			}
		})
	}
}

func TestSafeHTTPRejectsCredentialHeaders(t *testing.T) {
	for _, header := range []string{
		"Authorization", "authorization", "Proxy-Authorization", "Cookie", "Cookie2",
		"Set-Cookie", "X-API-Key", "X_Api_Key", "X-Auth-Token", "X-Access-Token", "X-Secret", "Host",
	} {
		t.Run(header, func(t *testing.T) {
			var resolved atomic.Bool
			s := SafeHTTP{Resolver: httpTestResolver(func(context.Context, string) ([]net.IPAddr, error) {
				resolved.Store(true)
				return nil, errors.New("unexpected DNS lookup")
			})}
			_, err := s.Do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}}, HTTPRequest{
				URL: "https://public.example/", Headers: map[string]string{header: "credential"},
			})
			if err == nil || resolved.Load() {
				t.Fatalf("credential request reached network: error=%v, resolved=%v", err, resolved.Load())
			}
		})
	}
}

func TestSafeHTTPBlocksNonPublicAddresses(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0", "0.1.2.3", "10.1.2.3", "100.64.0.1", "127.0.0.1", "169.254.169.254",
		"172.16.1.2", "192.0.0.8", "192.0.2.1", "192.88.99.1", "192.168.1.1",
		"198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "fc00::1", "fe80::1", "ff02::1",
		"64:ff9b::a00:1", "2001::a00:1", "2001:db8::1", "2002:7f00:1::", "3fff::1", "4000::1",
	} {
		t.Run(address, func(t *testing.T) {
			var dialed atomic.Bool
			s := SafeHTTP{
				Resolver: httpTestResolver(func(context.Context, string) ([]net.IPAddr, error) {
					return httpTestIPs(address), nil
				}),
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					dialed.Store(true)
					return nil, errors.New("unexpected dial")
				},
			}
			for _, host := range []string{"public.example", address} {
				_, err := s.Do(context.Background(), HTTPPolicy{AllowedHosts: []string{host}}, HTTPRequest{
					URL: "https://" + net.JoinHostPort(host, "443") + "/",
				})
				if err == nil || dialed.Load() {
					t.Fatalf("non-public %s reached dialer: %v", host, err)
				}
			}
		})
	}
}

func TestSafeHTTPRejectsUnsafeDNSAnswers(t *testing.T) {
	for _, tc := range []struct {
		name string
		ips  []net.IPAddr
		err  error
	}{
		{"empty", nil, nil},
		{"failed", nil, errors.New("DNS failed")},
		{"malformed", []net.IPAddr{{IP: net.IP{1, 2}}}, nil},
		{"mixed", httpTestIPs("8.8.8.8", "10.0.0.1"), nil},
		{"zoned", []net.IPAddr{{IP: net.ParseIP("2606:4700:4700::1111"), Zone: "eth0"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dialed atomic.Bool
			s := SafeHTTP{
				Resolver: httpTestResolver(func(context.Context, string) ([]net.IPAddr, error) { return tc.ips, tc.err }),
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					dialed.Store(true)
					return nil, errors.New("unexpected dial")
				},
			}
			_, err := s.Do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}}, HTTPRequest{URL: "https://public.example/"})
			if err == nil || dialed.Load() {
				t.Fatalf("unsafe DNS answer reached dialer: %v", err)
			}
		})
	}
}

func TestSafeHTTPRedirectRevalidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		location string
	}{
		{"private DNS", "http://private.example/"},
		{"private literal", "http://127.0.0.1/"},
		{"not allowed", "http://attacker.example/"},
		{"credentials", "http://user:pass@public.example/"},
		{"scheme", "ftp://public.example/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, calls := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, tc.location, http.StatusFound)
			})
			s.Resolver = httpTestResolver(func(_ context.Context, host string) ([]net.IPAddr, error) {
				if host == "private.example" {
					return httpTestIPs("10.0.0.1"), nil
				}
				return httpTestIPs("8.8.8.8"), nil
			})
			_, err := s.Do(context.Background(), HTTPPolicy{
				AllowedHosts: []string{"public.example", "private.example", "127.0.0.1"}, AllowHTTP: true,
			}, HTTPRequest{URL: "http://public.example/"})
			if err == nil || calls.Load() != 1 {
				t.Fatalf("unsafe redirect was followed: error=%v, dials=%d", err, calls.Load())
			}
		})
	}
}

func TestSafeHTTPDNSRebinding(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(strconv.FormatBool(redirect), func(t *testing.T) {
			s, dials := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if redirect {
					http.Redirect(w, r, "/next", http.StatusFound)
					return
				}
				_, _ = io.WriteString(w, "ok")
			})
			var lookups atomic.Int32
			s.Resolver = httpTestResolver(func(context.Context, string) ([]net.IPAddr, error) {
				if lookups.Add(1) == 1 {
					return httpTestIPs("8.8.8.8"), nil
				}
				return httpTestIPs("127.0.0.1"), nil
			})
			result, err := s.Do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}, AllowHTTP: true}, HTTPRequest{URL: "http://public.example/"})
			if redirect {
				if err == nil || lookups.Load() != 2 {
					t.Fatalf("redirect did not revalidate changed DNS: lookups=%d, error=%v", lookups.Load(), err)
				}
			} else if err != nil || result.Body != "ok" || lookups.Load() != 1 {
				t.Fatalf("request did a second DNS lookup: lookups=%d, result=%+v, error=%v", lookups.Load(), result, err)
			}
			if dials.Load() != 1 {
				t.Fatalf("private rebound address was dialed: %d", dials.Load())
			}
		})
	}
}

func TestSafeHTTPRedirectLimitAndHeaders(t *testing.T) {
	for _, redirects := range []int{3, 4} {
		t.Run(strconv.Itoa(redirects), func(t *testing.T) {
			var forwarded atomic.Bool
			s, calls := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
				if n > 0 && (r.Header.Get("X-Vendor-Credential") != "" || r.Header.Get("Referer") != "" || r.Header.Get("Cookie") != "") {
					forwarded.Store(true)
				}
				if n < redirects {
					w.Header().Set("Set-Cookie", "session=secret")
					http.Redirect(w, r, "/"+strconv.Itoa(n+1), http.StatusFound)
					return
				}
				_, _ = io.WriteString(w, "done")
			})
			result, err := s.Do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}, AllowHTTP: true}, HTTPRequest{
				URL: "http://public.example/0", Headers: map[string]string{"X-Vendor-Credential": "private"},
			})
			if redirects == 3 && (err != nil || result.Body != "done") {
				t.Fatalf("three redirects should succeed: %+v, %v", result, err)
			}
			if redirects == 4 && err == nil {
				t.Fatal("four redirects should fail")
			}
			if calls.Load() != 4 || forwarded.Load() {
				t.Fatalf("redirect safety failed: dials=%d, credentials forwarded=%v", calls.Load(), forwarded.Load())
			}
		})
	}
}

func TestSafeHTTPResponseBounds(t *testing.T) {
	for _, tc := range []struct {
		name      string
		limit     int64
		body      string
		wantBytes int
		truncated bool
	}{
		{"default", 0, strings.Repeat("x", (64<<10)+1), 64 << 10, true},
		{"negative default", -1, strings.Repeat("x", (64<<10)+1), 64 << 10, true},
		{"cap", 1 << 30, strings.Repeat("x", (1<<20)+1), 1 << 20, true},
		{"exact", 4, "test", 4, false},
		{"small", 4, "tests", 4, true},
		{"truncated JSON", 2, "{} ", 2, true},
		{"invalid JSON", 128, "{\"a\":1,", 7, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, tc.body)
			})
			result, err := s.Do(context.Background(), HTTPPolicy{AllowedHosts: []string{"public.example"}, AllowHTTP: true, MaxBytes: tc.limit}, HTTPRequest{URL: "http://public.example/"})
			if err != nil || result.Status != http.StatusBadRequest || len(result.Body) != tc.wantBytes || result.Truncated != tc.truncated || result.JSON != nil {
				t.Fatalf("unexpected bounded result: status=%d, bytes=%d, truncated=%v, json=%#v, error=%v", result.Status, len(result.Body), result.Truncated, result.JSON, err)
			}
		})
	}
}

func TestSafeHTTPContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, _ := httpTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
	})
	_, err := s.Do(ctx, HTTPPolicy{AllowedHosts: []string{"public.example"}, AllowHTTP: true}, HTTPRequest{URL: "http://public.example/"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("response read did not honor cancellation: %v", err)
	}
}

func TestSafeHTTPContextDeadlineDuringDNS(t *testing.T) {
	s := SafeHTTP{Resolver: httpTestResolver(func(ctx context.Context, host string) ([]net.IPAddr, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := s.Do(ctx, HTTPPolicy{AllowedHosts: []string{"public.example"}}, HTTPRequest{URL: "https://public.example/"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DNS lookup did not honor deadline: %v", err)
	}
}

func TestSafeHTTPPinsPublicAddresses(t *testing.T) {
	for _, address := range []string{"8.8.8.8", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		for _, literal := range []bool{false, true} {
			t.Run(address+"/"+strconv.FormatBool(literal), func(t *testing.T) {
				want := net.JoinHostPort(net.ParseIP(address).String(), "443")
				reached := errors.New("validated destination reached")
				var lookups atomic.Int32
				s := SafeHTTP{
					Resolver: httpTestResolver(func(context.Context, string) ([]net.IPAddr, error) {
						lookups.Add(1)
						return httpTestIPs(address), nil
					}),
					DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
						if network != "tcp" || target != want {
							return nil, fmt.Errorf("wrong dial target: %s %s, want tcp %s", network, target, want)
						}
						if _, ok := ctx.Deadline(); !ok {
							return nil, errors.New("dial context has no deadline")
						}
						return nil, reached
					},
				}
				host := "public.example"
				if literal {
					host = address
				}
				_, err := s.Do(context.Background(), HTTPPolicy{AllowedHosts: []string{host}}, HTTPRequest{
					URL: "https://" + net.JoinHostPort(host, "443") + "/",
				})
				wantLookups := int32(1)
				if literal {
					wantLookups = 0
				}
				if !errors.Is(err, reached) || lookups.Load() != wantLookups {
					t.Fatalf("public address was not pinned: lookups=%d, error=%v", lookups.Load(), err)
				}
			})
		}
	}
}

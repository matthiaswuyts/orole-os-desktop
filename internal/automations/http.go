package automations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// HTTPRequest deliberately carries no credential reference: HTTP steps are public
// requests, and may not forward authentication, cookies, or proxy credentials.
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

type HTTPResult struct {
	Status    int    `json:"status"`
	Body      string `json:"body"`
	JSON      any    `json:"json,omitempty"`
	Truncated bool   `json:"truncated"`
}

// HTTPPolicy requires an explicit host allowlist. A wildcard matches subdomains,
// never the apex. MaxBytes defaults to 64 KiB and is capped at 1 MiB.
type HTTPPolicy struct {
	AllowedHosts []string
	AllowHTTP    bool
	MaxBytes     int64
}

// SafeHTTP's zero value uses the system resolver and a bounded TCP dialer.
// Injected dialers must honor the validated numeric address and context; neither
// dependency may be selected by an automation definition.
type SafeHTTP struct {
	Resolver interface {
		LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
	}
	DialContext func(context.Context, string, string) (net.Conn, error)
}

// Do resolves and validates every destination before connecting to its numeric
// address. Each redirect gets a new connection, so DNS validation cannot be
// bypassed by pooling, and no transport or environment proxy does another lookup.
func (s *SafeHTTP) Do(ctx context.Context, p HTTPPolicy, r HTTPRequest) (HTTPResult, error) {
	return s.do(ctx, p, r, nil)
}

func (s *SafeHTTP) do(ctx context.Context, p HTTPPolicy, r HTTPRequest, beforeDispatch func(context.Context, httpRequestSnapshot) error) (HTTPResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, strings.NewReader(r.Body))
	if err != nil {
		return HTTPResult{}, errors.New("invalid HTTP request")
	}
	if err := validateHTTPURL(req.URL, p); err != nil {
		return HTTPResult{}, err
	}
	for name, value := range r.Headers {
		if credentialHTTPHeader(name) {
			return HTTPResult{}, fmt.Errorf("HTTP credential header %q is not allowed", name)
		}
		if strings.EqualFold(name, "Host") {
			return HTTPResult{}, errors.New("HTTP Host header override is not allowed")
		}
		req.Header.Set(name, value)
	}

	resolver := s.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dial := s.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	transport := &http.Transport{
		// Proxy is intentionally nil, not http.ProxyFromEnvironment.
		DisableKeepAlives:      true,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  15 * time.Second,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			// Transport may detach a dial from the request's cancellation. Keep
			// DNS and TCP bounded and reconnect cancellation to the whole operation.
			dialCtx, cancelDial := context.WithTimeout(dialCtx, 10*time.Second)
			defer cancelDial()
			stop := context.AfterFunc(ctx, cancelDial)
			defer stop()
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("invalid HTTP destination")
			}
			var ips []net.IPAddr
			if ip, err := netip.ParseAddr(host); err == nil {
				ips = []net.IPAddr{{IP: net.IP(ip.AsSlice()), Zone: ip.Zone()}}
			} else {
				ips, err = resolver.LookupIPAddr(dialCtx, host)
				if err != nil {
					return nil, fmt.Errorf("resolve HTTP destination: %w", err)
				}
			}
			if len(ips) == 0 {
				return nil, errors.New("HTTP destination has no addresses")
			}
			var target string
			// Reject mixed public/private answers as well as entirely private ones.
			// Freeze the chosen address while validating; never dial the hostname.
			for _, resolved := range ips {
				ip, ok := netip.AddrFromSlice(resolved.IP)
				if !ok || resolved.Zone != "" || !publicHTTPAddress(ip) {
					return nil, errors.New("HTTP destination resolves to a non-public address")
				}
				if target == "" {
					target = net.JoinHostPort(ip.Unmap().String(), port)
				}
			}
			return dial(dialCtx, network, target)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: authorizedTransport{transport: transport, body: r.Body, beforeDispatch: beforeDispatch},
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if req.Header.Get("Idempotency-Key") != "" {
				return errors.New("idempotency-key requests require a direct endpoint; redirects are not followed")
			}
			if len(via) > 3 {
				return errors.New("HTTP redirect limit exceeded")
			}
			if err := validateHTTPURL(next.URL, p); err != nil {
				return err
			}
			// Do not rely on net/http's same-domain credential forwarding rules.
			// Drop all supplied headers, including arbitrary vendor credential names.
			next.Header = make(http.Header)
			return nil
		},
	}
	response, err := client.Do(req)
	if err != nil {
		var denied *authorizationError
		if errors.As(err, &denied) {
			// net/http wraps errors with the full URL. An authorization failure
			// may specifically reject a secret-bearing URL; never retain it.
			return HTTPResult{}, denied
		}
		return HTTPResult{}, err
	}
	defer response.Body.Close()
	limit := p.MaxBytes
	if limit <= 0 {
		limit = 64 << 10
	}
	if limit > 1<<20 {
		limit = 1 << 20
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	result := HTTPResult{Status: response.StatusCode}
	if int64(len(body)) > limit {
		body = body[:limit]
		result.Truncated = true
	}
	result.Body = string(body)
	if err != nil {
		return result, fmt.Errorf("read HTTP response: %w", err)
	}
	if !result.Truncated {
		// Non-JSON responses are valid; the body remains available verbatim.
		var value any
		if json.Unmarshal(body, &value) == nil {
			result.JSON = value
		}
	}
	return result, nil
}

// httpRequestSnapshot is detached from the dispatched request: strings retain
// the exact immutable body/target, and headers are copied before authorization.
// A callback cannot rewrite the live request while the user considers approval.
type httpRequestSnapshot struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers http.Header `json:"headers"`
	Body    string      `json:"body"`
}

// Authorization runs for the initial concrete request and every redirect, before
// DNS or connection establishment. Low-level SafeHTTP callers only apply SSRF
// restrictions; the automation engine always supplies the shared-policy hook.
type authorizedTransport struct {
	transport      http.RoundTripper
	body           string
	beforeDispatch func(context.Context, httpRequestSnapshot) error
}

func (t authorizedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if t.beforeDispatch != nil {
		body := ""
		if req.Body != nil && req.Body != http.NoBody {
			// SafeHTTP creates bodies from one immutable string. net/http either
			// replays it for 307/308 or removes it when rewriting to GET.
			body = t.body
		}
		snapshot := httpRequestSnapshot{
			Method: req.Method, URL: req.URL.String(),
			Headers: req.Header.Clone(), Body: body,
		}
		if err := t.beforeDispatch(req.Context(), snapshot); err != nil {
			return nil, err
		}
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return t.transport.RoundTrip(req)
}

func validateHTTPURL(u *url.URL, p HTTPPolicy) error {
	if u.Scheme != "https" && !(p.AllowHTTP && u.Scheme == "http") {
		return errors.New("HTTP request requires HTTPS unless HTTP is explicitly allowed")
	}
	if u.User != nil {
		return errors.New("HTTP URL credentials are not allowed")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, "%*\\") || u.Opaque != "" {
		return errors.New("invalid HTTP destination host")
	}
	for _, allowed := range p.AllowedHosts {
		allowed = strings.ToLower(strings.TrimSuffix(allowed, "."))
		if allowed == host {
			return nil
		}
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:]
			if len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
				return nil
			}
		}
	}
	return errors.New("HTTP destination host is not allowed")
}

func credentialHTTPHeader(name string) bool {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", "")
	return strings.Contains(name, "auth") || strings.Contains(name, "cookie") ||
		strings.Contains(name, "token") || strings.Contains(name, "secret") ||
		strings.Contains(name, "apikey") || strings.HasPrefix(name, "proxy")
}

// net.IP.IsGlobalUnicast also accepts private and special-purpose addresses.
// Explicitly exclude special-use IPv4 blocks and IPv6 transition/documentation
// blocks. IPv6 outside the allocated global-unicast space is denied by default.
var blockedHTTPPrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var globalHTTPIPv6 = netip.MustParsePrefix("2000::/3")

func publicHTTPAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !globalHTTPIPv6.Contains(ip)) {
		return false
	}
	for _, prefix := range blockedHTTPPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

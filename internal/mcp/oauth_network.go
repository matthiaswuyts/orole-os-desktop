package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// oauthNetwork carries the configured resource's trust, never trust inferred from
// metadata or a DNS answer. Local integrations must use localhost or an explicit
// loopback/private/link-local IP; a public hostname resolving locally is not opt-in.
// HTTP remains limited to loopback by checkEndpointURL, even for local integrations.
type oauthNetwork struct {
	local    bool
	resolver interface {
		LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
	}
}

func newOAuthNetwork(source string) (*oauthNetwork, error) {
	if err := checkEndpointURL(source); err != nil {
		return nil, err
	}
	u, _ := url.Parse(source)
	local := isLoopbackHost(u.Host)
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		ip = ip.Unmap()
		local = ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return &oauthNetwork{local: local, resolver: net.DefaultResolver}, nil
}

func (n *oauthNetwork) checkURL(raw string) error {
	if err := checkEndpointURL(raw); err != nil {
		return err
	}
	if n.local {
		return nil
	}
	u, _ := url.Parse(raw)
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: public OAuth resource cannot use a local endpoint", ErrUnsafeURL)
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicOAuthAddress(ip) {
		return fmt.Errorf("%w: public OAuth resource cannot use a non-public address", ErrUnsafeURL)
	}
	return nil
}

// addresses validates the entire answer before any dial, including mixed answers.
// Dialers receive only these numeric addresses, preventing a second DNS lookup
// from changing the destination between validation and connection establishment.
func (n *oauthNetwork) addresses(ctx context.Context, host string) ([]net.IPAddr, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var ips []net.IPAddr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []net.IPAddr{{IP: net.IP(ip.AsSlice()), Zone: ip.Zone()}}
	} else {
		var err error
		ips, err = n.resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve OAuth destination: %w", err)
		}
	}
	if len(ips) == 0 {
		return nil, errors.New("OAuth destination has no addresses")
	}
	for _, resolved := range ips {
		ip, ok := netip.AddrFromSlice(resolved.IP)
		if !ok || (!n.local && (resolved.Zone != "" || !publicOAuthAddress(ip))) {
			return nil, fmt.Errorf("%w: OAuth destination resolves to a non-public address", ErrUnsafeURL)
		}
	}
	return ips, nil
}

// checkEndpoint also checks DNS for advertised endpoints that the daemon does not
// immediately dial (notably the authorization URL handed to the system browser).
// Daemon requests recheck DNS at dial time; this preflight is not a DNS pin.
func (n *oauthNetwork) checkEndpoint(ctx context.Context, raw string) error {
	if err := n.checkURL(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	_, err := n.addresses(ctx, u.Hostname())
	return err
}

type oauthTransport struct {
	network *oauthNetwork
	base    *http.Transport
}

func (t *oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.network.checkURL(req.URL.String()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func (t *oauthTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }

// client preserves TLS settings and bounded caller timeouts, but not proxy or
// custom TLS dial hooks that can bypass destination validation. An injected TCP
// dialer must honor the validated numeric address, as in automations.SafeHTTP.
func (n *oauthNetwork) client(base *http.Client) (*http.Client, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	if base != nil {
		*client = *base
		if client.Timeout == 0 {
			client.Timeout = 30 * time.Second
		}
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	template, ok := transport.(*http.Transport)
	if !ok {
		return nil, errors.New("OAuth requires an HTTP transport with a guarded TCP dialer")
	}
	t := template.Clone()
	dial := t.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	t.Proxy = nil // An environment proxy would resolve and dial outside this guard.
	t.DialTLS, t.DialTLSContext = nil, nil
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid OAuth destination")
		}
		ips, err := n.addresses(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			var conn net.Conn
			conn, err = dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, err
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := n.checkURL(req.URL.String()); err != nil {
			return err
		}
		// OAuth redirect policy belongs to the configured resource, not to the
		// current metadata hop or an injected client's redirect callback.
		return oauthRedirectWithTrust(req, via, n.local)
	}
	client.Transport = &oauthTransport{network: n, base: t}
	return client, nil
}

// streamClient uses the same destination policy for every live MCP request,
// including initialization, tool POSTs and the long-lived session GET. Only the
// overall request timeout is removed; DNS and connection establishment stay bounded.
func (n *oauthNetwork) streamClient(base *http.Client) (*http.Client, error) {
	client, err := n.client(base)
	if err != nil {
		return nil, err
	}
	client.Timeout = 0
	return client, nil
}

func oauthPostToken(ctx context.Context, base *http.Client, source, endpoint, clientID, clientSecret, style string, form url.Values) (*tokenResponse, error) {
	network, err := newOAuthNetwork(source)
	if err != nil {
		return nil, err
	}
	hc, err := network.client(base)
	if err != nil {
		return nil, err
	}
	defer hc.CloseIdleConnections()
	return postToken(ctx, hc, endpoint, clientID, clientSecret, style, form)
}

// Match the special-use exclusions in automations.SafeHTTP. Global-unicast alone
// includes private, documentation, transition and other non-public destinations.
var blockedOAuthPrefixes = [...]netip.Prefix{
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

var globalOAuthIPv6 = netip.MustParsePrefix("2000::/3")

func publicOAuthAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !globalOAuthIPv6.Contains(ip)) {
		return false
	}
	for _, prefix := range blockedOAuthPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

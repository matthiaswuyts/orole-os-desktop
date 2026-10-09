package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/manifest"
)

var errCrossHost = errors.New("the provider redirected to a different host; refused")

// origin is scheme://host[:port] in lowercase: the unit a reviewed manifest is pinned to.
func origin(scheme, host string) string {
	return strings.ToLower(scheme) + "://" + strings.ToLower(host)
}

// pinned refuses every request outside the reviewed origin before it reaches any client,
// so a key can never leave for another host whatever HTTPDoer is injected.
type pinned struct {
	doer   llm.HTTPDoer
	origin string
}

func (p pinned) Do(req *http.Request) (*http.Response, error) {
	if req.URL == nil || origin(req.URL.Scheme, req.URL.Host) != p.origin {
		return nil, errCrossHost
	}
	return p.doer.Do(req)
}

// newClient returns a client that only talks to m's origin: no proxy (a proxy would resolve
// the destination itself and bypass the dial checks), redirects only within the same origin,
// and dial-time address checks. A loopback manifest may only reach loopback addresses; every
// other manifest may only reach public addresses (the private/special ranges of
// internal/automations/http.go), so a remote name can never be steered at the LAN.
func newClient(m manifest.ProviderManifest) *http.Client {
	want := origin(schemeOf(m), m.Host())
	loopback := m.Loopback()
	hostname := hostnameOf(m)
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil || !strings.EqualFold(host, hostname) {
					return nil, errCrossHost
				}
				return dialChecked(ctx, dialer, network, host, port, loopback)
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute,
			ExpectContinueTimeout: time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          4,
			MaxIdleConnsPerHost:   4,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if origin(next.URL.Scheme, next.URL.Host) != want {
				return errCrossHost
			}
			if len(via) > 3 {
				return errors.New("the provider redirected too many times")
			}
			return nil
		},
	}
}

func schemeOf(m manifest.ProviderManifest) string {
	o := m.Origin()
	scheme, _, _ := strings.Cut(o, "://")
	return scheme
}

func hostnameOf(m manifest.ProviderManifest) string {
	h := m.Host()
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return strings.Trim(h, "[]")
}

// dialChecked resolves host, requires every answer to be allowed, then dials the numeric
// addresses in order so the checked address is the one connected to (no second lookup).
func dialChecked(ctx context.Context, d *net.Dialer, network, host, port string, loopback bool) (net.Conn, error) {
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve provider host: %w", err)
		}
		for _, r := range ips {
			ip, ok := netip.AddrFromSlice(r.IP)
			if !ok || r.Zone != "" {
				return nil, errors.New("the provider host resolves to an unusable address")
			}
			addrs = append(addrs, ip)
		}
	}
	if len(addrs) == 0 {
		return nil, errors.New("the provider host has no addresses")
	}
	for _, ip := range addrs {
		ip = ip.Unmap()
		if loopback && !ip.IsLoopback() {
			return nil, errors.New("a loopback provider host resolves to a non-loopback address")
		}
		if !loopback && !publicAddress(ip) {
			return nil, errors.New("the provider host resolves to a non-public address")
		}
	}
	var last error
	for _, ip := range addrs {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

// blockedPrefixes mirrors internal/automations/http.go: special-use IPv4 blocks and IPv6
// transition/documentation blocks. IPv6 outside allocated global unicast space is denied.
var blockedPrefixes = [...]netip.Prefix{
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

var globalIPv6 = netip.MustParsePrefix("2000::/3")

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !globalIPv6.Contains(ip)) {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

// fakeResolver answers from a table; unknown names do not resolve.
type fakeResolver map[string][]string

func (r fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ips, ok := r[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	out := make([]net.IPAddr, len(ips))
	for i, s := range ips {
		out[i] = net.IPAddr{IP: net.ParseIP(s)}
	}
	return out, nil
}

// serverFetcher routes every validated dial to srv, recording the addresses it was asked to dial.
func serverFetcher(t *testing.T, srv *httptest.Server, res fakeResolver) (*Fetcher, *[]string) {
	t.Helper()
	var dialed []string
	addr := srv.Listener.Addr().String()
	return &Fetcher{
		Resolver: res,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}, &dialed
}

func callFetch(f *Fetcher, rawURL string) (chat.ToolResult, error) {
	args, _ := json.Marshal(map[string]string{"url": rawURL})
	return f.Call(context.Background(), chat.RunContext{BotID: "b1", Actor: "bot:b1"}, args)
}

func TestPublicAddress(t *testing.T) {
	for addr, want := range map[string]bool{
		"93.184.216.34": true, "8.8.8.8": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "224.0.0.1": false,
		"::1": false, "fe80::1": false, "fc00::1": false, "::ffff:127.0.0.1": false, "::ffff:10.0.0.1": false,
		"64:ff9b::7f00:1": false, "2002:7f00:1::1": false,
	} {
		if got := PublicAddress(netip.MustParseAddr(addr)); got != want {
			t.Errorf("PublicAddress(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestFetchHappyPathHTML(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Hello &amp; welcome</title><style>p{}</style></head><body>
<nav>Menu</nav><h1>Big</h1><p>Some <b>bold</b> text<script>alert(1)</script> and <a href="/next">a link</a>.</p>
<ul><li>one</li><li>two</li></ul><footer>Footer</footer></body></html>`))
	}))
	defer srv.Close()
	f, dialed := serverFetcher(t, srv, fakeResolver{"docs.example.com": {"93.184.216.34"}})

	res, err := callFetch(f, "http://docs.example.com/page?token=secret")
	if err != nil || res.IsError {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if !res.Untrusted {
		t.Fatal("result must be untrusted")
	}
	for _, want := range []string{"Title: Hello & welcome", "# Big", "Some bold text and [a link](http://docs.example.com/next).", "- one", "- two"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("%q missing from:\n%s", want, res.Text)
		}
	}
	for _, bad := range []string{"alert", "Menu", "Footer", "p{}"} {
		if strings.Contains(res.Text, bad) {
			t.Errorf("%q leaked into:\n%s", bad, res.Text)
		}
	}
	if len(*dialed) != 1 || (*dialed)[0] != net.JoinHostPort("93.184.216.34", "80") {
		t.Fatalf("dialed %v: connections must go to the validated address", *dialed)
	}
	if gotHeaders.Get("Cookie") != "" || gotHeaders.Get("Authorization") != "" {
		t.Fatalf("credentials sent: %v", gotHeaders)
	}
	if len(res.Parts) != 1 || res.Parts[0].Type != chat.PartCitation || res.Parts[0].URL != "http://docs.example.com/page?token=secret" {
		t.Fatalf("parts = %+v", res.Parts)
	}
}

func TestFetchBlocksNonPublicDestinations(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte("secret"))
	}))
	defer srv.Close()
	f, dialed := serverFetcher(t, srv, fakeResolver{
		"intranet.example.com": {"10.0.0.5"},
		"mixed.example.com":    {"93.184.216.34", "127.0.0.1"},
		"meta.example.com":     {"169.254.169.254"},
		"v6.example.com":       {"::1"},
	})
	for _, target := range []string{
		"http://127.0.0.1/", "http://localhost:8080/", "http://10.0.0.1/", "http://[::1]/", "http://169.254.169.254/latest/meta-data/",
		"http://intranet.example.com/", "http://mixed.example.com/", "http://meta.example.com/", "http://v6.example.com/",
		"http://2130706433/", "http://0x7f.0.0.1/", "http://unresolvable.example.com/",
	} {
		res, err := callFetch(f, target)
		if err != nil || !res.IsError {
			t.Errorf("%s: res = %+v err = %v, want an error result", target, res, err)
		}
	}
	if len(*dialed) != 0 || hits.Load() != 0 {
		t.Fatalf("a blocked destination was contacted: dialed %v, hits %d", *dialed, hits.Load())
	}
}

func TestFetchDNSRebindingIsCheckedAtDialTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("fine"))
	}))
	defer srv.Close()
	// First lookup is public, later lookups are private: every connection resolves again.
	var lookups atomic.Int32
	res := rebinder{n: &lookups}
	f, _ := serverFetcher(t, srv, nil)
	f.Resolver = res
	first, _ := callFetch(f, "http://rebind.example.com/")
	if first.IsError {
		t.Fatalf("first fetch: %+v", first)
	}
	second, _ := callFetch(f, "http://rebind.example.com/")
	if !second.IsError || !strings.Contains(second.Text, "not a public internet address") {
		t.Fatalf("second fetch must be blocked, got %+v", second)
	}
}

type rebinder struct{ n *atomic.Int32 }

func (r rebinder) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	if r.n.Add(1) == 1 {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	return []net.IPAddr{{IP: net.ParseIP("192.168.0.9")}}, nil
}

func TestFetchRedirects(t *testing.T) {
	var privateHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/to-private", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://internal.example.com/secret", http.StatusFound)
	})
	mux.HandleFunc("/to-ip", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
	})
	mux.HandleFunc("/to-file", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	})
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	})
	mux.HandleFunc("/ok-target", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("landed"))
	})
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ok-target", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/secret", func(w http.ResponseWriter, r *http.Request) { privateHits.Add(1) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	f, _ := serverFetcher(t, srv, fakeResolver{
		"pub.example.com": {"93.184.216.34"}, "internal.example.com": {"10.9.9.9"},
	})
	for _, path := range []string{"to-private", "to-ip", "to-file", "loop"} {
		res, err := callFetch(f, "http://pub.example.com/"+path)
		if err != nil || !res.IsError {
			t.Errorf("%s: res = %+v err = %v, want an error result", path, res, err)
		}
		if strings.Contains(res.Text, "pub.example.com") || strings.Contains(res.Text, "169.254") {
			t.Errorf("%s: error leaks URL: %q", path, res.Text)
		}
	}
	if privateHits.Load() != 0 {
		t.Fatal("redirect reached a private host")
	}
	res, _ := callFetch(f, "http://pub.example.com/ok")
	if res.IsError || !strings.Contains(res.Text, "landed") || !strings.Contains(res.Text, "/ok-target") {
		t.Fatalf("followed redirect: %+v", res)
	}
}

func TestFetchResponseLimitsAndTypes(t *testing.T) {
	big := strings.Repeat("x", 3<<20)
	mux := http.NewServeMux()
	mux.HandleFunc("/declared-big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(big)))
		w.Write([]byte(big))
	})
	mux.HandleFunc("/stream-big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.(http.Flusher).Flush() // unknown length: chunked
		for range 3 {
			w.Write([]byte(big[:1<<20]))
		}
	})
	mux.HandleFunc("/pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.7 ..."))
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte{0, 1, 2, 3})
	})
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"a":1}`))
	})
	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><script>x()</script></html>"))
	})
	mux.HandleFunc("/latin1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
		w.Write([]byte{'c', 'a', 'f', 0xe9})
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	f, _ := serverFetcher(t, srv, fakeResolver{"pub.example.com": {"93.184.216.34"}})
	f.Timeout = 300 * time.Millisecond
	get := func(p string) chat.ToolResult {
		t.Helper()
		res, err := callFetch(f, "http://pub.example.com/"+p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		return res
	}
	if res := get("declared-big"); !res.IsError || !strings.Contains(res.Text, "larger than 2 MiB") {
		t.Errorf("declared oversize: %+v", res.Text[:min(len(res.Text), 120)])
	}
	if res := get("stream-big"); res.IsError || !strings.Contains(res.Text, "[content truncated]") || len(res.Text) > maxFetchOutput+1024 {
		t.Errorf("streamed oversize must be truncated, got error=%v len=%d", res.IsError, len(res.Text))
	}
	if res := get("pdf"); !res.IsError || !strings.Contains(res.Text, "PDF") {
		t.Errorf("pdf: %+v", res)
	}
	if res := get("bin"); !res.IsError || !strings.Contains(res.Text, "not supported") {
		t.Errorf("binary: %+v", res)
	}
	if res := get("json"); res.IsError || !strings.Contains(res.Text, `{"a":1}`) {
		t.Errorf("json: %+v", res)
	}
	if res := get("404"); !res.IsError || !strings.Contains(res.Text, "HTTP 404") {
		t.Errorf("404: %+v", res)
	}
	if res := get("empty"); !res.IsError || !strings.Contains(res.Text, "no readable text") {
		t.Errorf("empty: %+v", res)
	}
	if res := get("latin1"); res.IsError || !strings.Contains(res.Text, "café") {
		t.Errorf("latin1: %+v", res)
	}
	if res := get("slow"); !res.IsError || !strings.Contains(res.Text, "timed out") {
		t.Errorf("slow: %+v", res)
	}
}

func TestFetchArgumentValidation(t *testing.T) {
	f := &Fetcher{Resolver: fakeResolver{}}
	for _, args := range []string{
		`{}`, `{"url":""}`, `not json`, `{"url":"ftp://example.com/x"}`, `{"url":"file:///etc/passwd"}`,
		`{"url":"https://user:pw@example.com/"}`, `{"url":"https://other.org/"}`, `{"url":"javascript:alert(1)"}`,
		`{"url":"https://example.com/` + strings.Repeat("a", 3000) + `"}`,
	} {
		res, err := f.Call(context.Background(), chat.RunContext{BotID: "b"}, json.RawMessage(args))
		if err != nil || !res.IsError {
			t.Errorf("%.60s: res = %+v err = %v, want an error result", args, res, err)
		}
	}
}

type recordingPolicy struct {
	reqs []policy.Request
	err  error
}

func (p *recordingPolicy) Require(_ context.Context, r policy.Request) error {
	p.reqs = append(p.reqs, r)
	return p.err
}

func TestFetchPolicy(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f, _ := serverFetcher(t, srv, fakeResolver{"pub.example.com": {"93.184.216.34"}})
	pol := &recordingPolicy{}
	f.Policy = pol
	res, _ := callFetch(f, "http://user@pub.example.com/a?key=SECRET#frag")
	if !res.IsError { // credentials in URL are refused before the policy is consulted
		t.Fatalf("res = %+v", res)
	}
	res, _ = callFetch(f, "http://pub.example.com/a?key=SECRET#frag")
	if res.IsError || len(pol.reqs) != 1 {
		t.Fatalf("res = %+v reqs = %+v", res, pol.reqs)
	}
	r := pol.reqs[0]
	if r.Actor != "bot:b1" || r.Action != "chat.tool.call" || r.Resource != "chat/b1/fetch_url" || r.Effect != policy.EffectRead || r.FromHint {
		t.Fatalf("request = %+v", r)
	}
	if strings.Contains(r.Detail, "SECRET") || !strings.Contains(r.Detail, "pub.example.com/a") {
		t.Fatalf("detail must show the URL without its query: %q", r.Detail)
	}

	pol.err = policy.ErrDenied
	before := hits.Load()
	res, err := callFetch(f, "http://pub.example.com/b")
	if err != nil || !res.IsError || hits.Load() != before {
		t.Fatalf("denied fetch must not run: res = %+v err = %v hits = %d->%d", res, err, before, hits.Load())
	}
	if strings.Contains(res.Text, "did not answer") {
		t.Fatalf("a denial must not claim a timeout: %q", res.Text)
	}
	pol.err = fmt.Errorf("wrapped: %w", policy.ErrApprovalTimeout)
	res, err = callFetch(f, "http://pub.example.com/b")
	if err != nil || !res.IsError || hits.Load() != before || !strings.Contains(res.Text, "did not answer the approval request") || !strings.Contains(res.Text, "5 minutes") {
		t.Fatalf("timed-out fetch: res = %+v err = %v", res, err)
	}

	// A canceled run surfaces the cancellation instead of a tool error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pol.err = context.Canceled
	args, _ := json.Marshal(map[string]string{"url": "http://pub.example.com/c"})
	if _, err := f.Call(ctx, chat.RunContext{BotID: "b1"}, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestWebFetchFallbackReachesAnyPublicDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f, _ := serverFetcher(t, srv, fakeResolver{"a.example.com": {"93.184.216.34"}, "b.example.org": {"93.184.216.35"}})
	tool := WebFetch{Fetcher: f}
	// Domain lists saved by older versions are ignored: every public domain is reachable.
	bot := chat.Bot{ID: "b1", Config: chat.BotConfig{Tools: []chat.BotTool{{ID: chat.ToolWebFetch, Params: map[string]any{"allowed_domains": []any{"example.com"}}}}}}
	rc := chat.RunContext{BotID: "b1", Bot: bot}
	for _, target := range []string{"http://a.example.com/", "http://b.example.org/"} {
		res, _ := tool.Call(context.Background(), rc, FetchURLName, json.RawMessage(`{"url":"`+target+`"}`))
		if res.IsError {
			t.Fatalf("%s: %+v", target, res)
		}
	}
}

func TestExtractHTMLEdgeCases(t *testing.T) {
	base, _ := url.Parse("https://x.test/dir/page")
	title, text := extractHTML(`<!doctype html><!-- c --><TITLE> A  <b>t</b> </TITLE><body><P>One<br>two</p><p>3 &lt; 4 < 5</p>
<a href="mailto:x@y.z">mail</a><a href=sub/a.html>rel</a><a href="#top">top</a><h2>Head<h3>Sub</h3>
<table><tr><td>a</td><td>b</td></tr></table><svg><g>vector</g></svg><textarea>typed</textarea><div`, base)
	if title != "A <b>t</b>" {
		t.Errorf("title = %q", title)
	}
	for _, want := range []string{"One\ntwo", "3 < 4 < 5", "mail", "[rel](https://x.test/dir/sub/a.html)", "## Head", "### Sub", "a b"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing from %q", want, text)
		}
	}
	for _, bad := range []string{"mailto", "#top", "vector", "typed"} {
		if strings.Contains(text, bad) {
			t.Errorf("%q leaked into %q", bad, text)
		}
	}
}

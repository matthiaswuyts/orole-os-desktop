//go:build mobbinprobe

package mcp

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func rawPost(t *testing.T, tok, sid, body string) string {
	req, _ := http.NewRequest("POST", "https://api.mobbin.com/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	c := &http.Client{Timeout: 15 * time.Second}
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		t.Logf("POST err after %v: %v", time.Since(start), err)
		return ""
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2000))
	t.Logf("POST %s -> %d %s sid=%q proto=%s after %v body=%q err=%v", body[:40], resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Mcp-Session-Id"), resp.Proto, time.Since(start), b, err)
	return resp.Header.Get("Mcp-Session-Id")
}

func TestTmpMobbin(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	s, err := h.m.Add(ctx, AddParams{Name: "Mobbin", URL: "https://api.mobbin.com/mcp", AuthKind: AuthOAuth})
	if err != nil {
		t.Fatal("add", err)
	}
	st, err := h.m.StartAuth(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", st.URL).Start()
	var tok string
	for range 200 {
		time.Sleep(time.Second)
		if has, _, _, _ := h.m.authSession(s.ID).Snapshot(); has {
			tok, _ = h.m.authSession(s.ID).AccessToken(ctx)
			break
		}
	}
	if tok == "" {
		t.Fatal("no token")
	}
	sid := rawPost(t, tok, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	rawPost(t, tok, sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	rawPost(t, tok, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	rawPost(t, tok, "", `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	rawPost(t, tok, "", `{"jsonrpc":"2.0","method":"notifications/initialized","params":{"_meta":{}}}`)
	c := &http.Client{}
	mk := func(body string) *http.Request {
		r, _ := http.NewRequest("POST", "https://api.mobbin.com/mcp", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+tok)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		return r
	}
	resp, err := c.Do(mk(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
	t.Logf("init err=%v", err)
	if err == nil {
		buf := make([]byte, 16)
		_, _ = resp.Body.Read(buf) // body deliberately left unread
		ctx2, c2 := context.WithTimeout(ctx, 10*time.Second)
		defer c2()
		start := time.Now()
		r2, err := c.Do(mk(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`).WithContext(ctx2))
		if err == nil {
			t.Logf("notif while init body open: %d after %v", r2.StatusCode, time.Since(start))
		} else {
			t.Logf("notif while init body open: err=%v after %v", err, time.Since(start))
		}
	}
	sdkProbe(t, h, s.ID, tok)
}

type logRT struct {
	t *testing.T
}

func (l logRT) RoundTrip(r *http.Request) (*http.Response, error) {
	b, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(b)))
	h := r.Header.Clone()
	h.Del("Authorization")
	l.t.Logf("-> %s %s hdr=%v body=%.200s", r.Method, r.URL, h, b)
	start := time.Now()
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		l.t.Logf("<- ERR %v after %v", err, time.Since(start))
		return nil, err
	}
	l.t.Logf("<- %d %v after %v", resp.StatusCode, resp.Header, time.Since(start))
	return resp, nil
}

func sdkProbe(t *testing.T, h *harness, id, tokdummy string) {
	cl := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "1"}, &sdk.ClientOptions{Capabilities: &sdk.ClientCapabilities{}})
	tr := &sdk.StreamableClientTransport{Endpoint: "https://api.mobbin.com/mcp", HTTPClient: &http.Client{Transport: logRT{t}}, OAuthHandler: sdkHandler{a: h.m.authSession(id)}, MaxRetries: 2}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	_, err := cl.Connect(ctx, tr, &sdk.ClientSessionOptions{ProtocolVersion: "2025-06-18"})
	t.Logf("SDK connect err=%v", err)
}

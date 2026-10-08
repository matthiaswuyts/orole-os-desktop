package daemon_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

type memCreds struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memCreds) Set(k string, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[k] = v
	return nil
}
func (s *memCreds) Get(k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m[k]; ok {
		return v, nil
	}
	return nil, credentials.ErrNotFound
}
func (s *memCreds) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}

func TestGitHubServiceEndToEndAgainstFake(t *testing.T) {
	const pat = "github_pat_11ABCDEFG0123456789_secretsecretsecret"
	var issueGets, posts int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+pat {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
			return
		}
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"fake","id":1}`)
		case r.URL.Path == "/repos/o/r/issues" && r.Method == "GET":
			atomic.AddInt32(&issueGets, 1)
			fmt.Fprint(w, `[{"number":1,"title":"<img src=x onerror=alert(1)>","state":"open","repository_url":"https://api.github.com/repos/o/r"}]`)
		case r.URL.Path == "/repos/o/r/issues" && r.Method == "POST":
			atomic.AddInt32(&posts, 1)
			w.WriteHeader(201)
			fmt.Fprint(w, `{"number":2,"title":"made"}`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer fake.Close()
	store := &memCreds{}
	defer daemon.SetGitHubTestSeams(fake.URL, store)()

	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	var logs bytes.Buffer
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() { d.Shutdown(); <-done }()
	ctx := context.Background()
	c := control.NewClient(dir, "test")
	for i := 0; ; i++ {
		if err := c.Connect(ctx); err == nil {
			break
		} else if i > 100 {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer c.Close()

	var raw map[string]any
	if err := c.Call(ctx, "github.issues", map[string]any{"repo": "o/r"}, &raw); err == nil || !strings.Contains(err.Error(), "not_connected") {
		t.Fatalf("expected not_connected, got %v", err)
	}

	// Bad token: rejected, nothing stored.
	if err := c.Call(ctx, "github.login", map[string]any{"source": "pat", "token": strings.Repeat("x", 30)}, &raw); err == nil {
		t.Fatal("bad token accepted")
	}
	if len(store.m) != 0 {
		t.Fatal("rejected token was stored")
	}

	var st daemon.GitHubStatus
	if err := c.Call(ctx, "github.login", map[string]any{"source": "pat", "token": pat}, &st); err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.User == nil || st.User.Login != "fake" || st.Source != "pat" || !st.PATStored {
		t.Fatalf("status after login: %+v", st)
	}
	if string(store.m["github/token"]) != pat {
		t.Fatal("PAT not saved to credential store")
	}

	var issues []map[string]any
	for range 3 {
		if err := c.Call(ctx, "github.issues", map[string]any{"repo": "o/r"}, &issues); err != nil || len(issues) != 1 || issues[0]["repo"] != "o/r" {
			t.Fatalf("issues: %v %v", issues, err)
		}
	}
	if issueGets != 1 {
		t.Fatalf("60s cache: server saw %d GETs, want 1", issueGets)
	}

	create := map[string]any{"repo": "o/r", "title": "hello"}
	if err := c.Call(ctx, "github.issue.create", create, &raw); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("unconfirmed create must be refused, got %v", err)
	}
	if posts != 0 {
		t.Fatal("write reached GitHub without confirmation")
	}
	create["confirmed"] = true
	if err := c.Call(ctx, "github.issue.create", create, &raw); err != nil || posts != 1 {
		t.Fatalf("confirmed create: %v posts=%d", err, posts)
	}
	// Write invalidated the cache.
	if err := c.Call(ctx, "github.issues", map[string]any{"repo": "o/r"}, &issues); err != nil || issueGets != 2 {
		t.Fatalf("cache not invalidated by write: gets=%d %v", issueGets, err)
	}

	var n int
	row := d.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action='github.issue.create'`)
	if err := row.Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit rows=%d err=%v (want denied+allowed)", n, err)
	}

	if _, err := d.Policy.AddRule(ctx, policy.Rule{Actor: "user", Action: "github.issue.create", Resource: "*", MaxEffect: policy.EffectWrite, Decision: policy.Deny}); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "github.issue.create", create, &raw); err == nil || posts != 1 {
		t.Fatalf("explicit confirmation bypassed central deny: %v posts=%d", err, posts)
	}

	if err := c.Call(ctx, "github.logout", nil, &st); err != nil || st.Connected || len(store.m) != 0 {
		t.Fatalf("logout: %+v %v creds=%d", st, err, len(store.m))
	}
	if strings.Contains(logs.String(), pat) {
		t.Fatal("token leaked into daemon log")
	}
	var auditDump string
	rows, _ := d.DB.Query(`SELECT action||target||outcome||detail FROM audit_events`)
	for rows.Next() {
		var s string
		rows.Scan(&s)
		auditDump += s
	}
	if strings.Contains(auditDump, pat) {
		t.Fatal("token leaked into audit log")
	}
}

// TestGitHubLiveThroughDaemon drives the real daemon IPC against api.github.com
// using the machine's gh login. Read-only. OROLE_GITHUB_LIVE=1 to run.
func TestGitHubLiveThroughDaemon(t *testing.T) {
	if os.Getenv("OROLE_GITHUB_LIVE") == "" {
		t.Skip("set OROLE_GITHUB_LIVE=1")
	}
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() { d.Shutdown(); <-done }()
	ctx := context.Background()
	c := control.NewClient(dir, "test")
	for i := 0; ; i++ {
		if err := c.Connect(ctx); err == nil {
			break
		} else if i > 100 {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer c.Close()

	var st daemon.GitHubStatus
	if err := c.Call(ctx, "github.status", nil, &st); err != nil {
		t.Fatal(err)
	}
	t.Logf("before login: connected=%v ghDetected=%v ghLogin=%s", st.Connected, st.GHDetected, st.GHLogin)
	if err := c.Call(ctx, "github.login", map[string]any{"source": "gh"}, &st); err != nil {
		t.Fatal(err)
	}
	t.Logf("after login: connected=%v source=%s user=%s scopes=%q", st.Connected, st.Source, st.User.Login, st.Scopes)
	var repos []struct {
		FullName string `json:"full_name"`
	}
	if err := c.Call(ctx, "github.repos", map[string]any{"limit": 5}, &repos); err != nil {
		t.Fatal(err)
	}
	t.Logf("repos: %v", repos)
	var issues []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	if err := c.Call(ctx, "github.issues", map[string]any{"repo": "rayorole/orole-os-desktop", "state": "all", "limit": 100}, &issues); err != nil {
		t.Fatal(err)
	}
	t.Logf("issues: %d, first #%d %q", len(issues), issues[0].Number, issues[0].Title)
	var detail struct {
		Issue struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
		} `json:"issue"`
	}
	if err := c.Call(ctx, "github.issue", map[string]any{"repo": "rayorole/orole-os-desktop", "number": issues[0].Number}, &detail); err != nil {
		t.Fatal(err)
	}
	t.Logf("issue: %+v", detail.Issue)
	err = c.Call(ctx, "github.issue.create", map[string]any{"repo": "rayorole/orole-os-desktop", "title": "must be refused"}, nil)
	t.Logf("unconfirmed create (must be refused, nothing sent): %v", err)
	if err == nil {
		t.Fatal("write was not refused")
	}
}

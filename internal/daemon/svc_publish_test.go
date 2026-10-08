package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func publishGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestAgentsPublishThroughDaemon drives agents.publish over real IPC with a real repository, a
// local bare remote behind a github.com origin URL, and a fake GitHub API.
func TestAgentsPublishThroughDaemon(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	const pat = "github_pat_11ABCDEFG0123456789_secretsecretsecret"
	var mu sync.Mutex
	var posts []map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+pat {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"fake","id":1}`)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			out := []map[string]any{}
			for _, p := range posts {
				if "owner:"+p["head"].(string) == r.URL.Query().Get("head") {
					out = append(out, prJSON(p))
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls/3":
			_ = json.NewEncoder(w).Encode(prJSON(posts[0]))
		case r.Method == "POST" && r.URL.Path == "/repos/owner/repo/pulls":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			posts = append(posts, body)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(prJSON(body))
		default:
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
		}
	}))
	defer api.Close()
	defer daemon.SetGitHubTestSeams(api.URL, &memCreds{})()

	repo := t.TempDir()
	publishGit(t, repo, "init", "-b", "main")
	publishGit(t, repo, "config", "user.name", "Daemon Test")
	publishGit(t, repo, "config", "user.email", "daemon@example.test")
	publishGit(t, repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	publishGit(t, repo, "add", ".")
	publishGit(t, repo, "commit", "-m", "base")
	bare := filepath.Join(t.TempDir(), "remote.git")
	publishGit(t, repo, "init", "--bare", "-b", "main", bare)
	publishGit(t, repo, "remote", "add", "origin", "https://github.com/owner/repo.git")
	publishGit(t, repo, "config", "url."+bare+".insteadOf", "https://github.com/owner/repo.git")
	publishGit(t, repo, "push", "origin", "main")
	publishGit(t, repo, "remote", "set-head", "origin", "main")
	publishGit(t, repo, "checkout", "-b", "orole/daemon-publish")
	if err := os.WriteFile(filepath.Join(repo, "work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	var logs bytes.Buffer
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sess := agents.Session{ID: "publish-ipc", Kind: "claude", Machine: "local", Project: repo, Dir: repo, Branch: "orole/daemon-publish",
		BaseRef: publishGit(t, repo, "rev-parse", "main"), Title: "Daemon publish", Task: "write work.txt", Mode: agents.ModeStructured,
		Status: agents.StatusStopped, Result: "wrote it", StartedAt: now, UpdatedAt: now}
	if err := d.Agents.Store().SaveSession(sess, ""); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() { d.Shutdown(); <-done }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := control.NewClient(dir, "publish-test")
	for i := 0; ; i++ {
		if err := c.Connect(ctx); err == nil {
			break
		} else if i > 200 {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer c.Close()
	updated := make(chan agents.Session, 16)
	c.OnEvent(func(name string, data json.RawMessage) {
		if name != "agents.updated" {
			return
		}
		var s agents.Session
		if json.Unmarshal(data, &s) == nil && s.ID == sess.ID {
			select {
			case updated <- s:
			default:
			}
		}
	})
	if err := c.Subscribe(ctx, "agents.*"); err != nil {
		t.Fatal(err)
	}

	publish := func(spec map[string]any) (agents.Session, error) {
		var out agents.Session
		err := c.Call(ctx, "agents.publish", map[string]any{"id": sess.ID, "spec": spec}, &out)
		return out, err
	}
	pullSpec := map[string]any{"push": true, "pr": true, "draft": true}

	// Without a GitHub sign-in the pull request step cannot run, and nothing is committed.
	if _, err := publish(map[string]any{"push": true, "pr": true, "confirmed": true}); err == nil || !strings.Contains(err.Error(), "not_connected") {
		t.Fatalf("publish while signed out: %v", err)
	}
	if publishGit(t, repo, "status", "--porcelain") == "" {
		t.Fatal("a refused publish committed")
	}
	var raw map[string]any
	if err := c.Call(ctx, "github.login", map[string]any{"source": "pat", "token": pat}, &raw); err != nil {
		t.Fatal(err)
	}

	if _, err := publish(pullSpec); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("unconfirmed publish must be refused, got %v", err)
	}
	if err := c.Call(ctx, "agents.publish", map[string]any{"id": "missing", "spec": map[string]any{}}, nil); err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("unknown session: %v", err)
	}

	// Central policy wins over the user's confirmation, and denies before anything changes.
	rule, err := d.Policy.AddRule(ctx, policy.Rule{Actor: "user", Action: "github.pull.create", Resource: "*", MaxEffect: policy.EffectWrite, Decision: policy.Deny})
	if err != nil {
		t.Fatal(err)
	}
	pullSpec["confirmed"] = true
	if _, err := publish(pullSpec); err == nil || !strings.Contains(err.Error(), "prepare") {
		t.Fatalf("policy denial: %v", err)
	}
	if publishGit(t, repo, "status", "--porcelain") == "" || publishGit(t, bare, "branch", "--list", "orole/*") != "" {
		t.Fatal("a policy-denied publish changed the repository or the remote")
	}
	if err := d.Policy.DeleteRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}

	got, err := publish(pullSpec)
	if err != nil {
		t.Fatal(err)
	}
	head := publishGit(t, repo, "rev-parse", "HEAD")
	if got.PRNumber != 3 || got.PRURL != "https://github.com/owner/repo/pull/3" || got.Branch != "orole/daemon-publish" {
		t.Fatalf("published session: %+v", got)
	}
	if publishGit(t, bare, "rev-parse", "refs/heads/orole/daemon-publish") != head || publishGit(t, repo, "status", "--porcelain") != "" {
		t.Fatal("work was not committed and pushed")
	}
	select {
	case s := <-updated:
		if s.ID != sess.ID {
			t.Fatalf("unexpected update %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no agents.updated event")
	}
	mu.Lock()
	if len(posts) != 1 || posts[0]["base"] != "main" || posts[0]["draft"] != true || posts[0]["title"] != "Daemon publish" ||
		!strings.Contains(posts[0]["body"].(string), "write work.txt") || !strings.Contains(posts[0]["body"].(string), "wrote it") {
		t.Fatalf("pull request request: %v", posts)
	}
	mu.Unlock()

	// Idempotent: the second run returns the same pull request without another commit or POST.
	again, err := publish(pullSpec)
	if err != nil || again.PRNumber != 3 || publishGit(t, repo, "rev-parse", "HEAD") != head {
		t.Fatalf("second publish: %+v %v", again, err)
	}
	mu.Lock()
	n := len(posts)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("second publish opened another pull request (%d)", n)
	}

	// Both GitHub gates were audited as user actions.
	for _, action := range []string{"github.branch.push", "github.pull.create"} {
		var count int
		if err := d.DB.QueryRow(`SELECT count(*) FROM audit_events WHERE action=? AND outcome='allowed'`, action).Scan(&count); err != nil || count == 0 {
			t.Fatalf("no allowed audit row for %s: %d %v", action, count, err)
		}
	}
	if strings.Contains(logs.String(), pat) {
		t.Fatal("token leaked into the daemon log")
	}
}

func prJSON(body map[string]any) map[string]any {
	repo := map[string]any{"full_name": "owner/repo"}
	return map[string]any{
		"number": 3, "html_url": "https://github.com/owner/repo/pull/3", "state": "open", "draft": body["draft"], "title": body["title"],
		"head": map[string]any{"ref": body["head"], "sha": "abc", "repo": repo},
		"base": map[string]any{"ref": body["base"], "sha": "def", "repo": repo},
	}
}

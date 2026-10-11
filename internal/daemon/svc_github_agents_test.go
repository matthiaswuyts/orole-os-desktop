package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/git"
	"github.com/rayorole/orole-os-desktop/internal/github"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/projects"
	"github.com/rayorole/orole-os-desktop/internal/testrepo"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func issueTestService(t *testing.T) *ghService {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "issue.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	d := &Daemon{DB: db}
	d.Projects = projects.New(db)
	d.Policy = policy.New(db.DB, nil, nil)
	d.Agents = agents.NewManager(agents.Config{Store: agents.NewStore(db), ProjectDirectory: d.projectTaskDirectory, Authorize: func(context.Context, string, string, string) error { return policy.ErrDenied }})
	t.Cleanup(d.Agents.Shutdown)
	return &ghService{d: d}
}

func issueTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

// issueRepoTemplate is built once per test binary; issueTestProject copies it (see internal/testrepo).
var issueRepoTemplate = testrepo.New(func(dir string) error {
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Issue Test"},
		{"config", "user.email", "issue@example.test"},
	} {
		if err := testrepo.Git(dir, args...); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("base\n"), 0600); err != nil {
		return err
	}
	if err := testrepo.Git(dir, "add", "file.txt"); err != nil {
		return err
	}
	return testrepo.Git(dir, "commit", "-m", "base")
})

func issueTestProject(t *testing.T, g *ghService, remote string) projects.Project {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	issueRepoTemplate.Copy(t, dir)
	issueTestGit(t, dir, "remote", "add", "origin", remote)
	p, err := g.d.Projects.Add(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func issueTestRecord(t *testing.T, g *ghService, r IssueAgentRun) {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.d.DB.Exec(`INSERT INTO github_issue_runs(id,request_key,repo,issue_number,data,created_at) VALUES(?,?,?,?,?,?)`, r.ID, r.Key, r.Repo, r.Number, string(b), time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
}

func TestIssueAgentProjectMatchUsesLiveRemote(t *testing.T) {
	t.Parallel()
	g := issueTestService(t)
	p := issueTestProject(t, g, "git@github.com:Owner/Repo.git")
	ctx := context.Background()
	ps, err := g.issueProjects(ctx, "owner/repo")
	if err != nil || len(ps) != 1 || ps[0].ID != p.ID {
		t.Fatalf("matching projects: %+v %v", ps, err)
	}
	if _, err := g.issueProject(ctx, p.ID, "owner/unrelated"); err == nil {
		t.Fatal("unrelated repository accepted")
	}
	issueTestGit(t, p.Path, "remote", "set-url", "origin", "https://github.com/owner/different.git")
	if _, err := g.issueProject(ctx, p.ID, "owner/repo"); err == nil {
		t.Fatal("stale registered remote accepted")
	}
	for _, remote := range []string{"https://github.com.evil/owner/repo", "https://github.com/owner/repo?rewrite=x", "file:///owner/repo", "ssh://git@evil/owner/repo", "https://github.com/owner/repo/extra"} {
		if githubRemoteRepo(remote) != "" {
			t.Errorf("unsafe remote accepted: %s", remote)
		}
	}
}

func TestIssueRunRecoversDurableSessionLinkAndRetry(t *testing.T) {
	t.Parallel()
	g := issueTestService(t)
	r := IssueAgentRun{ID: "run", Key: "intent-key", Repo: "owner/repo", Number: 54, ProjectID: "project", Kind: "claude", Task: "Fix the selected bug", IssueURL: "https://github.com/owner/repo/issues/54", IssueTitle: "Bug", IssueBody: "Untrusted source", IssueUpdatedAt: "2026-10-08T00:00:00Z", State: "launch"}
	issueTestRecord(t, g, r)
	now := time.Now().UTC()
	s := agents.Session{ID: "session", Kind: "claude", Project: "project-path", Dir: "owned-worktree", Branch: "orole/issue-54", BaseRef: "commit", WorktreeOwned: true, Status: agents.StatusStopped, StartedAt: now, UpdatedAt: now}
	if err := g.d.Agents.Store().SaveSession(s, "github.issue:"+r.ID); err != nil {
		t.Fatal(err)
	}
	// A new service instance sees provenance even if the old one died before saving the link.
	g = &ghService{d: g.d}
	got, err := g.startIssueAgent(context.Background(), IssueAgentStart{Repo: r.Repo, Number: r.Number, ProjectID: r.ProjectID, Kind: r.Kind, Task: r.Task, IssueUpdatedAt: r.IssueUpdatedAt, Key: r.Key, Confirmed: true})
	if err != nil || got.SessionID != s.ID || got.Worktree != s.Dir || got.Branch != s.Branch || got.IssueBody != r.IssueBody || got.IssueURL != r.IssueURL {
		t.Fatalf("recovered run: %+v %v", got, err)
	}
	list, err := g.d.Agents.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("retry created extra sessions: %v %v", list, err)
	}
	_, err = g.startIssueAgent(context.Background(), IssueAgentStart{Repo: r.Repo, Number: r.Number, ProjectID: r.ProjectID, Kind: r.Kind, Task: "Different task", IssueUpdatedAt: r.IssueUpdatedAt, Key: r.Key, Confirmed: true})
	if err == nil {
		t.Fatal("idempotency key reused for a different task")
	}
}

func TestIssueWorkflowCancellationAndConfirmation(t *testing.T) {
	t.Parallel()
	g := issueTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.startIssueAgent(ctx, IssueAgentStart{Confirmed: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled launch: %v", err)
	}
	if _, err := g.startIssueAgent(context.Background(), IssueAgentStart{}); err == nil {
		t.Fatal("unreviewed source launched")
	}
	if _, err := g.publishIssuePR(ctx, issuePublishArg{Confirmed: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publish: %v", err)
	}
	if _, err := g.publishIssuePR(context.Background(), issuePublishArg{}); err == nil {
		t.Fatal("unconfirmed publish accepted")
	}
	var n int
	if err := g.d.DB.QueryRow(`SELECT count(*) FROM github_issue_runs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("cancelled/denied intent persisted: %d %v", n, err)
	}
}

func TestIssuePublicationPrerequisitesAndPolicyDenial(t *testing.T) {
	t.Parallel()
	g := issueTestService(t)
	ctx := context.Background()
	p := issueTestProject(t, g, "https://github.com/owner/repo.git")
	wt, err := g.d.Projects.Worktrees.Create(ctx, p.ID, "issue-54-test", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "file.txt"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	r := IssueAgentRun{ID: "publication", Key: "publication-key", Repo: "owner/repo", Number: 54, ProjectID: p.ID, SessionID: "session", Worktree: wt.Path, Branch: wt.Branch, State: "active", IssueURL: "https://github.com/owner/repo/issues/54"}
	issueTestRecord(t, g, r)
	s := agents.Session{ID: r.SessionID, Kind: "claude", Project: p.Path, Dir: wt.Path, Branch: wt.Branch, WorktreeOwned: true, Status: agents.StatusStopped, StartedAt: now, UpdatedAt: now}
	if err := g.d.Agents.Store().SaveSession(s, "github.issue:"+r.ID); err != nil {
		t.Fatal(err)
	}
	arg := issuePrepareArg{ID: r.ID, Title: "Fix issue", Body: "Reviewed changes", Base: "main"}
	if _, err := g.prepareIssuePR(ctx, arg); err == nil {
		t.Fatal("dirty worktree prepared for publication")
	}
	issueTestGit(t, wt.Path, "add", "file.txt")
	issueTestGit(t, wt.Path, "commit", "-m", "reviewed change")
	baseHead := issueTestGit(t, wt.Path, "rev-parse", "main")
	var baseMoved atomic.Bool
	branchAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		head := baseHead
		if baseMoved.Load() {
			head = strings.Repeat("a", 40)
		}
		_, _ = w.Write([]byte(`{"ref":"refs/heads/main","object":{"type":"commit","sha":"` + head + `"}}`))
	}))
	defer branchAPI.Close()
	g.client = github.NewClient(issueTestToken{})
	g.client.BaseURL = branchAPI.URL
	baseMoved.Store(true)
	if _, err := g.prepareIssuePR(ctx, arg); err == nil || !strings.Contains(err.Error(), "local base differs") {
		t.Fatalf("stale local base: %v", err)
	}
	baseMoved.Store(false)
	prep, err := g.prepareIssuePR(ctx, arg)
	if err != nil {
		t.Fatal(err)
	}
	if prep.Token == "" || !strings.Contains(prep.Diff, "+changed") || prep.Head == "" {
		t.Fatalf("missing real preparation: %+v", prep)
	}
	g.client.Authorize = github.RequireConfirmation(nil)
	baseMoved.Store(true)
	if _, err := g.publishIssuePR(ctx, issuePublishArg{ID: r.ID, Token: prep.Token, Confirmed: true}); err == nil || !strings.Contains(err.Error(), "GitHub base changed") {
		t.Fatalf("moved remote base: %v", err)
	}
	baseMoved.Store(false)
	if _, err := g.publishIssuePR(ctx, issuePublishArg{ID: r.ID, Token: "wrong", Confirmed: true}); err == nil {
		t.Fatal("wrong review token accepted")
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "file.txt"), []byte("changed again\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := g.publishIssuePR(ctx, issuePublishArg{ID: r.ID, Token: prep.Token, Confirmed: true}); err == nil {
		t.Fatal("changed prepared worktree published")
	}
	issueTestGit(t, wt.Path, "restore", "file.txt")
	var remoteCalls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer api.Close()
	g.client = github.NewClient(nil)
	g.client.BaseURL = api.URL
	g.client.Authorize = func(ctx context.Context, action, detail string) error {
		ev, err := g.d.Policy.Decide(ctx, policy.Request{Actor: "user", Action: action, Resource: "github/" + detail, Effect: policy.EffectWrite})
		if err != nil {
			return err
		}
		if ev.Decision == policy.Deny {
			return policy.ErrDenied
		}
		return github.RequireConfirmation(nil)(ctx, action, detail)
	}
	if _, err := g.d.Policy.AddRule(ctx, policy.Rule{Actor: "user", Action: "github.pull.create", Resource: "*", MaxEffect: policy.EffectWrite, Decision: policy.Deny}); err != nil {
		t.Fatal(err)
	}
	_, err = g.publishIssuePR(ctx, issuePublishArg{ID: r.ID, Token: prep.Token, Confirmed: true})
	if !errors.Is(err, policy.ErrDenied) {
		t.Fatalf("policy denial: %v", err)
	}
	if remoteCalls != 0 {
		t.Fatal("publication contacted remote despite PR policy denial")
	}
	current, err := git.ResolveCommit(ctx, wt.Path, "HEAD")
	if err != nil || current != prep.Head {
		t.Fatalf("denial changed branch: %s %v", current, err)
	}
}

type issueTestToken struct{}

func (issueTestToken) Name() string                          { return "test" }
func (issueTestToken) Token(context.Context) (string, error) { return "test-only-token", nil }
func (issueTestToken) Invalidate()                           {}

type issueInstalledAdapter struct{ agents.Claude }

func (issueInstalledAdapter) Detect(context.Context) agents.Detection {
	return agents.Detection{Kind: "claude", Installed: true}
}

func TestIssueLaunchUsesManagerDenialAndPreservesReviewedSource(t *testing.T) {
	t.Parallel()
	g := issueTestService(t)
	p := issueTestProject(t, g, "https://github.com/owner/repo.git")
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":54,"title":"Issue title","body":"External issue body","updated_at":"2026-10-08T00:00:00Z"}`))
	}))
	defer api.Close()
	g.client = github.NewClient(issueTestToken{})
	g.client.BaseURL = api.URL
	denials := 0
	g.d.Agents = agents.NewManager(agents.Config{
		Store: agents.NewStore(g.d.DB), Adapters: []agents.Adapter{issueInstalledAdapter{}},
		ProjectDirectory: g.d.projectTaskDirectory,
		Authorize:        func(context.Context, string, string, string) error { denials++; return policy.ErrDenied },
	})
	t.Cleanup(g.d.Agents.Shutdown)
	spec := IssueAgentStart{Repo: "owner/repo", Number: 54, ProjectID: p.ID, Kind: "claude", Task: "Investigate this issue only", IssueUpdatedAt: "2026-10-08T00:00:00Z", Key: "denied-launch-key", Confirmed: true}
	for range 2 {
		if _, err := g.startIssueAgent(context.Background(), spec); !errors.Is(err, policy.ErrDenied) {
			t.Fatalf("manager denial: %v", err)
		}
	}
	runs, err := g.issueRuns(context.Background(), spec.Repo, spec.Number)
	if err != nil || len(runs) != 1 {
		t.Fatalf("denied retry provenance: %+v %v", runs, err)
	}
	r := runs[0]
	if r.State != "failed" || r.SessionID != "" || r.Task != spec.Task || r.IssueBody != "External issue body" || r.IssueTitle != "Issue title" || denials != 2 {
		t.Fatalf("denied provenance lost or authority bypassed: %+v denials=%d", r, denials)
	}
	sessions, err := g.d.Agents.List()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("denied launch created sessions: %+v %v", sessions, err)
	}
	worktrees, err := g.d.Projects.Worktrees.List(context.Background(), p.ID)
	if err != nil || len(worktrees) != 1 {
		t.Fatalf("denied launch created worktrees: %+v %v", worktrees, err)
	}
}

package agents_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/github"
	"github.com/rayorole/orole-os-desktop/internal/terminals"
	"github.com/rayorole/orole-os-desktop/internal/testrepo"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// ---- fake GitHub API ----

type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server

	mu         sync.Mutex
	posts      []map[string]any
	lists      int
	repoGets   int
	prs        []map[string]any
	failCreate bool
	deny       map[string]bool
	actions    []string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, deny: map[string]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/pulls":
		f.lists++
		out := []map[string]any{}
		for _, pr := range f.prs {
			if "owner:"+pr["head"].(map[string]any)["ref"].(string) == r.URL.Query().Get("head") {
				out = append(out, pr)
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/pulls":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posts = append(f.posts, body)
		if f.failCreate {
			w.WriteHeader(422)
			_, _ = io.WriteString(w, `{"message":"Validation Failed"}`)
			return
		}
		repo := map[string]any{"full_name": "owner/repo"}
		pr := map[string]any{
			"number": 7, "html_url": "https://github.com/owner/repo/pull/7", "state": "open", "draft": body["draft"], "title": body["title"],
			"head": map[string]any{"ref": body["head"], "sha": "abc", "repo": repo},
			"base": map[string]any{"ref": body["base"], "sha": "def", "repo": repo},
		}
		f.prs = append(f.prs, pr)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(pr)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/pulls/7" && len(f.prs) == 1:
		_ = json.NewEncoder(w).Encode(f.prs[0])
	case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo":
		f.repoGets++
		_, _ = io.WriteString(w, `{"full_name":"owner/repo","default_branch":"trunk"}`)
	default:
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}
}

// client mirrors the daemon's client: every write passes an Authorize hook that needs the user's
// confirmation (and can be made to deny).
func (f *fakeGitHub) client() (*github.Client, error) {
	c := github.NewClient(github.StaticSource{T: "test-token"})
	c.BaseURL = f.srv.URL
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	confirm := github.RequireConfirmation(nil)
	c.Authorize = func(ctx context.Context, action, detail string) error {
		f.mu.Lock()
		f.actions = append(f.actions, action)
		denied := f.deny[action]
		f.mu.Unlock()
		if denied {
			return errors.New("denied by policy: " + action)
		}
		return confirm(ctx, action, detail)
	}
	return c, nil
}

func (f *fakeGitHub) postCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts)
}

func (f *fakeGitHub) post(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.posts[i]
}

func (f *fakeGitHub) repoGetCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.repoGets
}

func (f *fakeGitHub) has(action string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.actions {
		if a == action {
			return true
		}
	}
	return false
}

// ---- harness ----

type pubEnv struct {
	m    *agents.Manager
	st   *agents.Store
	db   *database.DB
	gh   *fakeGitHub
	pubs *pubLog
}

func newPubEnv(t *testing.T) *pubEnv {
	t.Helper()
	data, root := t.TempDir(), t.TempDir()
	db, err := database.Open(filepath.Join(data, "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	gh := newFakeGitHub(t)
	e := &pubEnv{st: agents.NewStore(db), db: db, gh: gh, pubs: &pubLog{}}
	term := terminals.NewManager(terminals.NewBackend(), nil)
	e.m = agents.NewManager(agents.Config{
		Store: e.st, Host: &agents.Host{Terminals: term, DataDir: data},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Worktrees: agents.GitWorktrees{Root: root},
		Publish: e.pubs.publish, Audit: (&recorder{}).audit, GitHub: gh.client,
		Authorize: func(context.Context, string, string, string) error { return nil },
		Adapters:  []agents.Adapter{agents.Claude{Bin: fakeBin}, agents.Custom{}},
	})
	t.Cleanup(func() { e.m.Shutdown(); term.Shutdown() })
	return e
}

// publishTemplate holds <work> and <remote.git>: work is the base repository on main, with origin
// https://github.com/Owner/Repo.git already pushed to the bare remote (url.<bare>.insteadOf makes that
// push real without a network) and origin/HEAD set. The template's own path is not recorded in
// either repository; publishRepo adds the insteadOf rewrite for the copy.
var publishTemplate = testrepo.New(func(dir string) error {
	work, bare := filepath.Join(dir, "work"), filepath.Join(dir, "remote.git")
	if err := os.Mkdir(work, 0o755); err != nil {
		return err
	}
	if err := buildBaseRepo(work); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"branch", "-M", "main"},
		{"config", "commit.gpgsign", "false"},
		{"init", "--bare", "-b", "main", bare},
		{"remote", "add", "origin", "https://github.com/Owner/Repo.git"},
		{"-c", "url." + bare + ".insteadOf=https://github.com/Owner/Repo.git", "push", "origin", "main"},
		{"remote", "set-head", "origin", "main"},
	} {
		if err := testrepo.Git(work, args...); err != nil {
			return err
		}
	}
	return nil
})

// publishRepo is a repository whose origin is https://github.com/Owner/Repo.git but is rewritten
// (url.<bare>.insteadOf) to a local bare repository, so pushes are real and need no network.
func publishRepo(t *testing.T) (dir, bare string) {
	t.Helper()
	root := t.TempDir()
	publishTemplate.Copy(t, root)
	dir, bare = filepath.Join(root, "work"), filepath.Join(root, "remote.git")
	gitTest(t, dir, "config", "url."+bare+".insteadOf", "https://github.com/Owner/Repo.git")
	return dir, bare
}

// startEdited runs the fake agent in a new worktree of repo; it leaves one untracked file behind.
func (e *pubEnv) startEdited(t *testing.T, repo, title string) agents.Session {
	t.Helper()
	s, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: repo, Prompt: "EDIT the files", Title: title, NewWorktree: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err = e.m.WaitFor(ctx, s.ID, status(agents.StatusDone))
	if err != nil {
		t.Fatalf("waiting for the fake agent: %v (status %s, error %q)", err, s.Status, s.Error)
	}
	return s
}

// saveStopped stores a session that is not running, working in dir.
func (e *pubEnv) saveStopped(t *testing.T, dir, branch, baseRef string) agents.Session {
	t.Helper()
	now := time.Now().UTC()
	s := agents.Session{ID: "stopped-" + filepath.Base(dir), Kind: "claude", Machine: "local", Project: dir, Dir: dir, Branch: branch, BaseRef: baseRef,
		Title: "Stopped task", Task: "do the thing", Status: agents.StatusStopped, StartedAt: now, UpdatedAt: now}
	if err := e.st.SaveSession(s, ""); err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *pubEnv) systemEvents(t *testing.T, id string) []string {
	t.Helper()
	evs, err := e.m.Events(id, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ev := range evs {
		if ev.Kind == agents.EvSystem {
			out = append(out, ev.Text)
		}
	}
	return out
}

func mustContain(t *testing.T, lines []string, want string) {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, want) {
			return
		}
	}
	t.Fatalf("no system event contains %q in %q", want, lines)
}

func stepOf(t *testing.T, err error) string {
	t.Helper()
	var pe *agents.PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("not a PublishError: %v", err)
	}
	return pe.Step
}

func confirmed(spec agents.PublishSpec) agents.PublishSpec {
	spec.Confirmed = true
	return spec
}

// ---- tests ----

func TestWorktreeBranchNamingAndBaseRef(t *testing.T) {
	repo, _ := publishRepo(t)
	e := newPubEnv(t)
	base := gitTest(t, repo, "rev-parse", "HEAD")

	a := e.startEdited(t, repo, "Fix the login bug!")
	if a.Branch != "orole/fix-the-login-bug" || !a.WorktreeOwned || a.BaseRef != base {
		t.Fatalf("first session: branch=%q owned=%v baseRef=%q (want %q)", a.Branch, a.WorktreeOwned, a.BaseRef, base)
	}
	if got := gitTest(t, a.Dir, "rev-parse", "--abbrev-ref", "HEAD"); got != a.Branch {
		t.Fatalf("worktree is on %q, session says %q", got, a.Branch)
	}
	b := e.startEdited(t, repo, "Fix the login bug!")
	if b.Branch == a.Branch || !strings.HasPrefix(b.Branch, "orole/fix-the-login-bug-") || len(b.Branch) != len("orole/fix-the-login-bug-")+6 {
		t.Fatalf("conflicting name must get a short unique suffix, got %q", b.Branch)
	}
	// The worktree record survives a reload.
	if got, err := e.st.GetSession(b.ID); err != nil || got.Branch != b.Branch || got.Dir != b.Dir || got.BaseRef != base {
		t.Fatalf("persisted: %+v %v", got, err)
	}
	// Without a title the prompt names the branch.
	c, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: repo, Prompt: "EDIT: Add dark mode\nmore detail", NewWorktree: true})
	if err != nil || c.Branch != "orole/edit-add-dark-mode" {
		t.Fatalf("prompt-named branch: %q %v", c.Branch, err)
	}
	// An explicit branch is honoured as given.
	d, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: repo, Prompt: "EDIT", Branch: "try-1", NewWorktree: true})
	if err != nil || d.Branch != "orole/try-1" {
		t.Fatalf("explicit branch: %q %v", d.Branch, err)
	}
}

func TestPublishCommitPushPullRequestAndIdempotentRerun(t *testing.T) {
	repo, bare := publishRepo(t)
	e := newPubEnv(t)
	s := e.startEdited(t, repo, "Add edited file")
	if s.Branch != "orole/add-edited-file" {
		t.Fatalf("branch %q", s.Branch)
	}
	ctx := ctx10(t)
	spec := agents.PublishSpec{Push: true, PR: true, Draft: true}

	// Remote effects are refused without confirmation, and nothing is touched.
	if _, err := e.m.Publish(ctx, s.ID, spec); !errors.Is(err, agents.ErrPublishNeedsConfirmation) {
		t.Fatalf("unconfirmed publish: %v", err)
	}
	if head := gitTest(t, s.Dir, "rev-parse", "HEAD"); head != s.BaseRef || gitTest(t, s.Dir, "status", "--porcelain") == "" {
		t.Fatal("unconfirmed publish changed the work tree")
	}

	got, err := e.m.Publish(ctx, s.ID, confirmed(spec))
	if err != nil {
		t.Fatal(err)
	}
	head := gitTest(t, s.Dir, "rev-parse", "HEAD")
	if head == s.BaseRef || gitTest(t, s.Dir, "status", "--porcelain") != "" {
		t.Fatalf("not committed: head=%s base=%s", head, s.BaseRef)
	}
	if subject := gitTest(t, s.Dir, "log", "-1", "--format=%s"); subject != "Add edited file" {
		t.Fatalf("default commit message = %q, want the task title", subject)
	}
	if tip := gitTest(t, bare, "rev-parse", "refs/heads/"+s.Branch); tip != head {
		t.Fatalf("remote tip %s != local %s", tip, head)
	}
	if up := gitTest(t, s.Dir, "rev-parse", "--abbrev-ref", "@{upstream}"); up != "origin/"+s.Branch {
		t.Fatalf("upstream = %q", up)
	}
	if got.PRNumber != 7 || got.PRURL != "https://github.com/owner/repo/pull/7" {
		t.Fatalf("session PR = %d %q", got.PRNumber, got.PRURL)
	}
	if stored, err := e.st.GetSession(s.ID); err != nil || stored.PRNumber != 7 || stored.PRURL != got.PRURL {
		t.Fatalf("PR not persisted: %+v %v", stored, err)
	}
	var col string
	if err := e.db.QueryRow(`SELECT pr_url FROM agent_sessions WHERE id=?`, s.ID).Scan(&col); err != nil || col != got.PRURL {
		t.Fatalf("pr_url column = %q %v", col, err)
	}
	if e.gh.postCount() != 1 {
		t.Fatalf("posts = %d", e.gh.postCount())
	}
	post := e.gh.post(0)
	if post["title"] != "Add edited file" || post["head"] != s.Branch || post["base"] != "main" || post["draft"] != true {
		t.Fatalf("PR request = %v", post)
	}
	if body, _ := post["body"].(string); !strings.Contains(body, "EDIT the files") || !strings.Contains(body, "## Result") {
		t.Fatalf("default PR body should carry the task and the result: %q", body)
	}
	if !e.gh.has("github.branch.push") || !e.gh.has("github.pull.create") {
		t.Fatal("GitHub gates not consulted")
	}
	events := e.systemEvents(t, s.ID)
	mustContain(t, events, "Committed ")
	mustContain(t, events, "Pushed orole/add-edited-file to origin")
	mustContain(t, events, "Opened draft pull request #7 against main")
	if e.pubs.count("agents.updated") == 0 {
		t.Fatal("no agents.updated event")
	}

	// Re-running with a clean tree is a no-op that returns the same pull request.
	again, err := e.m.Publish(ctx, s.ID, confirmed(spec))
	if err != nil || again.PRNumber != 7 || again.PRURL != got.PRURL {
		t.Fatalf("second publish: %+v %v", again, err)
	}
	if gitTest(t, s.Dir, "rev-parse", "HEAD") != head || e.gh.postCount() != 1 {
		t.Fatal("second publish created another commit or pull request")
	}
	mustContain(t, e.systemEvents(t, s.ID), "Nothing to commit")
	mustContain(t, e.systemEvents(t, s.ID), "Pull request #7 already exists")

	// New work is committed and pushed onto the existing pull request.
	if err := os.WriteFile(filepath.Join(s.Dir, "second.txt"), []byte("more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := e.m.Publish(ctx, s.ID, confirmed(agents.PublishSpec{Message: "Second change", Push: true, PR: true}))
	if err != nil || third.PRNumber != 7 {
		t.Fatalf("third publish: %+v %v", third, err)
	}
	newHead := gitTest(t, s.Dir, "rev-parse", "HEAD")
	if newHead == head || gitTest(t, bare, "rev-parse", "refs/heads/"+s.Branch) != newHead || gitTest(t, s.Dir, "log", "-1", "--format=%s") != "Second change" {
		t.Fatal("follow-up commit was not pushed")
	}
	if e.gh.postCount() != 1 {
		t.Fatalf("a second pull request was opened (%d posts)", e.gh.postCount())
	}
}

func TestPublishRecoversPullRequestWhenCreationFailed(t *testing.T) {
	repo, bare := publishRepo(t)
	e := newPubEnv(t)
	s := e.startEdited(t, repo, "Flaky PR")
	spec := confirmed(agents.PublishSpec{Push: true, PR: true})

	e.gh.failCreate = true
	_, err := e.m.Publish(ctx10(t), s.ID, spec)
	if err == nil || stepOf(t, err) != agents.StepPullRequest || !strings.Contains(err.Error(), "pushed") {
		t.Fatalf("want a pull request step failure, got %v", err)
	}
	head := gitTest(t, s.Dir, "rev-parse", "HEAD")
	if gitTest(t, bare, "rev-parse", "refs/heads/"+s.Branch) != head {
		t.Fatal("the branch must stay pushed when only the PR step failed")
	}
	if cur, _ := e.st.GetSession(s.ID); cur.PRURL != "" || cur.PRNumber != 0 {
		t.Fatalf("a failed PR step recorded a pull request: %+v", cur)
	}
	mustContain(t, e.systemEvents(t, s.ID), "Publish stopped at pull request")

	e.gh.failCreate = false
	got, err := e.m.Publish(ctx10(t), s.ID, spec)
	if err != nil || got.PRNumber != 7 {
		t.Fatalf("retry: %+v %v", got, err)
	}
	if gitTest(t, s.Dir, "rev-parse", "HEAD") != head {
		t.Fatal("retry made another commit")
	}
}

func TestPublishBaseSelection(t *testing.T) {
	repo, _ := publishRepo(t)
	e := newPubEnv(t)
	publish := func(name string, spec agents.PublishSpec, baseRef string, prepare func(dir string)) map[string]any {
		t.Helper()
		// Each case gets its own checkout of the same repository.
		wt := filepath.Join(t.TempDir(), name)
		gitTest(t, repo, "worktree", "add", "-b", "feature/"+name, wt, "main")
		if prepare != nil {
			prepare(wt)
		}
		if err := os.WriteFile(filepath.Join(wt, name+".txt"), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		s := e.saveStopped(t, wt, "", baseRef)
		before := e.gh.postCount()
		got, err := e.m.Publish(ctx10(t), s.ID, confirmed(spec))
		if err != nil || got.PRNumber != 7 || got.Branch != "feature/"+name {
			t.Fatalf("%s: %+v %v", name, got, err)
		}
		if e.gh.postCount() != before+1 {
			t.Fatalf("%s: no pull request was posted", name)
		}
		e.gh.mu.Lock()
		defer e.gh.mu.Unlock()
		e.gh.prs = nil // each case starts without a pull request on GitHub
		return e.gh.posts[len(e.gh.posts)-1]
	}
	mainSHA := gitTest(t, repo, "rev-parse", "main")

	if p := publish("explicit", agents.PublishSpec{Push: true, PR: true, Base: "release-9"}, mainSHA, nil); p["base"] != "release-9" {
		t.Fatalf("explicit base: %v", p["base"])
	}
	if p := publish("from-baseref", agents.PublishSpec{Push: true, PR: true}, mainSHA, nil); p["base"] != "main" {
		t.Fatalf("base from the session's base commit: %v", p["base"])
	}
	// No usable base commit: the repository's default branch from origin/HEAD.
	if p := publish("from-origin-head", agents.PublishSpec{Push: true, PR: true}, "", nil); p["base"] != "main" {
		t.Fatalf("base from origin/HEAD: %v", p["base"])
	}
	// Without origin/HEAD the GitHub API decides.
	gitTest(t, repo, "remote", "set-head", "origin", "--delete")
	if p := publish("from-api", agents.PublishSpec{Push: true, PR: true}, "", nil); p["base"] != "trunk" || e.gh.repoGetCount() == 0 {
		t.Fatalf("base from the API: %v", p["base"])
	}
}

func TestPublishRefusesAndNamesTheStep(t *testing.T) {
	t.Run("protected branch", func(t *testing.T) {
		repo, bare := publishRepo(t)
		e := newPubEnv(t)
		s := e.saveStopped(t, repo, "", "")
		if err := os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		head := gitTest(t, repo, "rev-parse", "HEAD")
		_, err := e.m.Publish(ctx10(t), s.ID, confirmed(agents.PublishSpec{Push: true}))
		if err == nil || stepOf(t, err) != agents.StepPrepare || !strings.Contains(err.Error(), "protected") {
			t.Fatalf("pushing main: %v", err)
		}
		if gitTest(t, repo, "rev-parse", "HEAD") != head || gitTest(t, repo, "status", "--porcelain") == "" {
			t.Fatal("a refused publish still committed")
		}
		if gitTest(t, bare, "rev-parse", "main") != head {
			t.Fatal("remote main moved")
		}
	})
	t.Run("default branch with an unusual name", func(t *testing.T) {
		repo, _ := publishRepo(t)
		gitTest(t, repo, "branch", "-m", "main", "mainline")
		gitTest(t, repo, "push", "origin", "mainline")
		gitTest(t, repo, "remote", "set-head", "origin", "mainline")
		e := newPubEnv(t)
		s := e.saveStopped(t, repo, "", "")
		if _, err := e.m.Publish(ctx10(t), s.ID, confirmed(agents.PublishSpec{Push: true})); err == nil || !strings.Contains(err.Error(), "default branch") {
			t.Fatalf("pushing the default branch: %v", err)
		}
	})
	t.Run("detached HEAD", func(t *testing.T) {
		repo, _ := publishRepo(t)
		gitTest(t, repo, "checkout", "--detach")
		e := newPubEnv(t)
		s := e.saveStopped(t, repo, "", "")
		_, err := e.m.Publish(ctx10(t), s.ID, agents.PublishSpec{})
		if err == nil || stepOf(t, err) != agents.StepPrepare || !strings.Contains(err.Error(), "detached") {
			t.Fatalf("detached: %v", err)
		}
	})
	t.Run("no remote", func(t *testing.T) {
		repo, _ := publishRepo(t)
		gitTest(t, repo, "checkout", "-b", "feature/x")
		gitTest(t, repo, "remote", "remove", "origin")
		if err := os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		e := newPubEnv(t)
		s := e.saveStopped(t, repo, "", "")
		_, err := e.m.Publish(ctx10(t), s.ID, confirmed(agents.PublishSpec{Push: true}))
		if err == nil || stepOf(t, err) != agents.StepPrepare || !strings.Contains(err.Error(), "no remote named") {
			t.Fatalf("no remote: %v", err)
		}
		if gitTest(t, repo, "status", "--porcelain") == "" {
			t.Fatal("a refused publish still committed")
		}
		// Committing alone does not need a remote.
		if got, err := e.m.Publish(ctx10(t), s.ID, agents.PublishSpec{Message: "local only"}); err != nil || got.Branch != "feature/x" {
			t.Fatalf("commit-only publish: %+v %v", got, err)
		}
		if gitTest(t, repo, "log", "-1", "--format=%s") != "local only" {
			t.Fatal("commit-only publish did not commit")
		}
	})
	t.Run("pull request needs a GitHub origin", func(t *testing.T) {
		repo, bare := publishRepo(t)
		gitTest(t, repo, "checkout", "-b", "feature/x")
		gitTest(t, repo, "remote", "set-url", "origin", bare)
		e := newPubEnv(t)
		s := e.saveStopped(t, repo, "", "")
		_, err := e.m.Publish(ctx10(t), s.ID, confirmed(agents.PublishSpec{Push: true, PR: true}))
		if err == nil || stepOf(t, err) != agents.StepPrepare || !strings.Contains(err.Error(), "not a GitHub repository") {
			t.Fatalf("local origin + PR: %v", err)
		}
		// A plain push to a non-GitHub remote is fine.
		if _, err := e.m.Publish(ctx10(t), s.ID, confirmed(agents.PublishSpec{Push: true})); err != nil {
			t.Fatalf("push to a plain remote: %v", err)
		}
		if gitTest(t, bare, "rev-parse", "refs/heads/feature/x") != gitTest(t, repo, "rev-parse", "HEAD") {
			t.Fatal("branch not pushed")
		}
	})
	t.Run("policy denial happens before any change", func(t *testing.T) {
		repo, bare := publishRepo(t)
		e := newPubEnv(t)
		s := e.startEdited(t, repo, "Denied PR")
		e.gh.deny["github.pull.create"] = true
		_, err := e.m.Publish(ctx10(t), s.ID, confirmed(agents.PublishSpec{Push: true, PR: true}))
		if err == nil || stepOf(t, err) != agents.StepPrepare || !strings.Contains(err.Error(), "denied by policy") {
			t.Fatalf("denied: %v", err)
		}
		if gitTest(t, s.Dir, "rev-parse", "HEAD") != s.BaseRef || gitTest(t, s.Dir, "status", "--porcelain") == "" {
			t.Fatal("a denied publish committed")
		}
		if out := gitTest(t, bare, "branch", "--list", "orole/*"); out != "" {
			t.Fatalf("a denied publish pushed: %s", out)
		}
		if e.gh.postCount() != 0 {
			t.Fatal("a denied publish reached GitHub")
		}
	})
	t.Run("commit fails without an identity", func(t *testing.T) {
		repo, _ := publishRepo(t)
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
		e := newPubEnv(t)
		s := e.startEdited(t, repo, "No identity")
		gitTest(t, repo, "config", "--unset", "user.name")
		gitTest(t, repo, "config", "--unset", "user.email")
		_, err := e.m.Publish(ctx10(t), s.ID, confirmed(agents.PublishSpec{Push: true}))
		if err == nil {
			t.Skip("a system-level git configuration provides an identity")
		}
		if stepOf(t, err) != agents.StepCommit || !strings.Contains(err.Error(), "user.name") {
			t.Fatalf("want a commit step failure naming the identity, got %v", err)
		}
		if gitTest(t, s.Dir, "rev-parse", "HEAD") != s.BaseRef {
			t.Fatal("committed without an identity")
		}
	})
	t.Run("push fails", func(t *testing.T) {
		repo, bare := publishRepo(t)
		e := newPubEnv(t)
		s := e.startEdited(t, repo, "Unreachable remote")
		if err := os.RemoveAll(bare); err != nil {
			t.Fatal(err)
		}
		_, err := e.m.Publish(ctx10(t), s.ID, confirmed(agents.PublishSpec{Push: true, PR: true}))
		if err == nil || stepOf(t, err) != agents.StepPush || !strings.Contains(err.Error(), "no force push") {
			t.Fatalf("push failure: %v", err)
		}
		if gitTest(t, s.Dir, "rev-parse", "HEAD") == s.BaseRef {
			t.Fatal("the commit step should have completed before the push failed")
		}
		if e.gh.postCount() != 0 {
			t.Fatal("a pull request was attempted after the push failed")
		}
	})
	t.Run("unknown session", func(t *testing.T) {
		e := newPubEnv(t)
		if _, err := e.m.Publish(ctx10(t), "missing", agents.PublishSpec{}); !errors.Is(err, agents.ErrNotFound) {
			t.Fatalf("unknown session: %v", err)
		}
	})
	t.Run("remote machine sessions are refused", func(t *testing.T) {
		e := newPubEnv(t)
		now := time.Now().UTC()
		s := agents.Session{ID: "remote", Kind: "claude", Machine: "box-1", Dir: "/srv/x", Status: agents.StatusStopped, StartedAt: now, UpdatedAt: now}
		if err := e.st.SaveSession(s, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := e.m.Publish(ctx10(t), s.ID, agents.PublishSpec{}); err == nil || stepOf(t, err) != agents.StepPrepare {
			t.Fatalf("remote session: %v", err)
		}
	})
}

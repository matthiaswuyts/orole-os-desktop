package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/github"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

const (
	ghCacheTTL    = 60 * time.Second
	ghCodeNoLogin = "not_connected"
	ghSettingKey  = "github.source"
)

// GitHubStatus is the connection state shown in setup screens, settings and widgets.
type GitHubStatus struct {
	Connected  bool               `json:"connected"`
	Source     string             `json:"source"` // "gh", "pat" or ""
	User       *github.User       `json:"user,omitempty"`
	Error      string             `json:"error,omitempty"`
	GHDetected bool               `json:"ghDetected"`
	GHLogin    string             `json:"ghLogin,omitempty"`
	PATStored  bool               `json:"patStored"`
	Scopes     string             `json:"scopes,omitempty"`
	Rate       []github.RateLimit `json:"rate"`
}

// GitHubCloneEvent is published as github.clone while a clone runs.
type GitHubCloneEvent struct {
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	State   string `json:"state"` // running, done, error
	Line    string `json:"line,omitempty"`
	Percent int    `json:"percent"`
	Target  string `json:"target,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Test seams.
var (
	ghAPIBase         string // overrides https://api.github.com
	ghCredentialStore = credentials.New
)

type ghCacheItem struct {
	at  time.Time
	val any
}

type ghService struct {
	d     *Daemon
	creds credentials.Store

	mu      sync.Mutex
	issueMu sync.Mutex // serializes durable issue launches and publication intents
	source  string
	client  *github.Client
	cache   map[string]ghCacheItem
	epoch   uint64
}

func (d *Daemon) registerGitHub() {
	g := &ghService{d: d, creds: ghCredentialStore(), cache: map[string]ghCacheItem{}}
	d.gh = g
	g.loadSource()
	g.registerHandlers()
	g.registerIssueAgents()
	if d.Automations != nil {
		d.Automations.SetGitHubQuery(g)
	}
}

func (g *ghService) loadSource() {
	var v string
	if err := g.d.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, ghSettingKey).Scan(&v); err != nil {
		return
	}
	var s string
	if json.Unmarshal([]byte(v), &s) == nil && (s == "gh" || s == "pat") {
		g.source = s
		g.client = g.newClient(g.tokenSource(s))
	}
}

func (g *ghService) tokenSource(kind string) github.TokenSource {
	if kind == "pat" {
		return github.StoreSource{Store: g.creds}
	}
	return &github.GHCLISource{}
}

func (g *ghService) newClient(ts github.TokenSource) *github.Client {
	c := github.NewClient(ts)
	if ghAPIBase != "" {
		c.BaseURL = ghAPIBase
	}
	c.Log = g.d.Log
	confirmation := github.RequireConfirmation(func(action, detail, outcome string) {
		g.d.audit(action, "github", outcome, detail)
	})
	c.Authorize = func(ctx context.Context, action, detail string) error {
		if g.d.Policy != nil {
			ev, err := g.d.Policy.Decide(ctx, policy.Request{
				Actor: "user", Action: action, Resource: "github/" + detail,
				Detail: detail, Effect: policy.EffectWrite,
			})
			if err != nil {
				return err
			}
			if ev.Decision == policy.Deny {
				g.d.audit(action, "github", "denied", ev.Reason)
				return policy.ErrDenied
			}
		}
		// The feature's dialog resolves Ask; stored Allow does not waive
		// confirmed:true, and a policy Deny always wins.
		return confirmation(ctx, action, detail)
	}
	return c
}

func (g *ghService) persistSource(kind string) error {
	b, _ := json.Marshal(kind)
	_, err := g.d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		ghSettingKey, string(b), time.Now().Unix())
	return err
}

func (g *ghService) invalidate(ctx context.Context, reason string) {
	g.mu.Lock()
	g.cache = map[string]ghCacheItem{}
	g.epoch++
	g.mu.Unlock()
	g.d.IPC.Publish("github.changed", map[string]string{"reason": reason})
	if g.d.Events != nil {
		g.d.Events.Publish(events.Event{Topic: events.TopicGitHubChanged, Data: map[string]any{"reason": reason}, Chain: events.ChainFrom(ctx)})
	}
}

func (g *ghService) conn() (*github.Client, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.client == nil {
		return nil, &ipc.Error{Code: ghCodeNoLogin, Message: "GitHub is not connected"}
	}
	return g.client, nil
}

func ghCtx(ctx context.Context, confirmed bool) context.Context {
	if confirmed {
		return github.WithConfirmation(ctx)
	}
	return ctx
}

func ghOrEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ghRead registers a cached (60s) read method.
func ghRead[P any, R any](g *ghService, name string, fn func(ctx context.Context, c *github.Client, p P) (R, error)) {
	handle(g.d, name, func(ctx context.Context, p P) (R, error) {
		var zero R
		c, err := g.conn()
		if err != nil {
			return zero, err
		}
		kb, _ := json.Marshal(p)
		key := name + "|" + string(kb)
		g.mu.Lock()
		epoch := g.epoch
		if it, ok := g.cache[key]; ok && time.Since(it.at) < ghCacheTTL {
			g.mu.Unlock()
			return it.val.(R), nil
		}
		g.mu.Unlock()
		v, err := fn(ctx, c, p)
		if err != nil {
			return zero, err
		}
		g.mu.Lock()
		if c == g.client && epoch == g.epoch {
			if len(g.cache) >= 256 {
				g.cache = map[string]ghCacheItem{}
			}
			g.cache[key] = ghCacheItem{at: time.Now(), val: v}
		}
		g.mu.Unlock()
		return v, nil
	})
}

type ghRepoParam struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

func (g *ghService) status(ctx context.Context) GitHubStatus {
	st := GitHubStatus{Rate: []github.RateLimit{}}
	g.mu.Lock()
	st.Source = g.source
	c := g.client
	g.mu.Unlock()

	if tok, err := (github.StoreSource{Store: g.creds}).Token(ctx); err == nil && tok != "" {
		st.PATStored = true
	}
	if c != nil {
		u, err := c.Me(ctx)
		if err != nil {
			st.Error = err.Error()
		} else {
			st.Connected, st.User = true, &u
			st.Scopes = c.Scopes()
		}
		st.Rate = ghOrEmpty(c.RateLimits())
	}
	if !st.Connected || st.Source != "gh" {
		// Detect an existing gh login so setup can offer it (token stays in memory).
		probe := g.newClient(&github.GHCLISource{})
		if u, err := probe.Me(ctx); err == nil {
			st.GHDetected, st.GHLogin = true, u.Login
		}
	} else {
		st.GHDetected = true
		if st.User != nil {
			st.GHLogin = st.User.Login
		}
	}
	return st
}

func (g *ghService) registerHandlers() {
	d := g.d

	handle(d, "github.status", func(ctx context.Context, _ none) (GitHubStatus, error) {
		g.mu.Lock()
		it, ok := g.cache["status"]
		epoch := g.epoch
		g.mu.Unlock()
		if ok && time.Since(it.at) < ghCacheTTL {
			return it.val.(GitHubStatus), nil
		}
		st := g.status(ctx)
		g.mu.Lock()
		if epoch == g.epoch {
			g.cache["status"] = ghCacheItem{at: time.Now(), val: st}
		}
		g.mu.Unlock()
		return st, nil
	})

	handle(d, "github.login", func(ctx context.Context, p struct {
		Source string `json:"source"` // "gh" | "pat"
		Token  string `json:"token"`  // pat only; never echoed or logged
	}) (GitHubStatus, error) {
		var ts github.TokenSource
		switch p.Source {
		case "gh":
			ts = &github.GHCLISource{}
		case "pat":
			t := strings.TrimSpace(p.Token)
			if len(t) < 20 || strings.ContainsAny(t, " \r\n\t") {
				return GitHubStatus{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: "that does not look like a GitHub token"}
			}
			ts = github.StaticSource{T: t}
		default:
			return GitHubStatus{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: "source must be gh or pat"}
		}
		// Validate before committing anything.
		u, err := g.newClient(ts).Me(ctx)
		if err != nil {
			g.d.audit("github.login", "github", "denied", p.Source)
			if p.Source == "pat" {
				return GitHubStatus{}, errors.New(github.Redact(err.Error(), p.Token))
			}
			return GitHubStatus{}, err
		}
		if p.Source == "pat" {
			if err := (github.StoreSource{Store: g.creds}).Save(p.Token); err != nil {
				return GitHubStatus{}, errors.New("could not store the token in Windows Credential Manager")
			}
		}
		if err := g.persistSource(p.Source); err != nil {
			return GitHubStatus{}, err
		}
		g.mu.Lock()
		g.source = p.Source
		g.client = g.newClient(g.tokenSource(p.Source))
		g.mu.Unlock()
		g.d.audit("github.login", "github", "allowed", p.Source+" as "+u.Login)
		g.invalidate(ctx, "login")
		return g.status(ctx), nil
	})

	handle(d, "github.logout", func(ctx context.Context, _ none) (GitHubStatus, error) {
		if err := (github.StoreSource{Store: g.creds}).Clear(); err != nil {
			return GitHubStatus{}, errors.New("could not remove the token from Windows Credential Manager")
		}
		if _, err := d.DB.Exec(`DELETE FROM settings WHERE key=?`, ghSettingKey); err != nil {
			return GitHubStatus{}, err
		}
		g.mu.Lock()
		g.source, g.client = "", nil
		g.mu.Unlock()
		g.d.audit("github.logout", "github", "allowed", "")
		g.invalidate(ctx, "logout")
		return g.status(ctx), nil
	})

	ghRead(g, "github.repos", func(ctx context.Context, c *github.Client, p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}) ([]github.Repo, error) {
		var r []github.Repo
		var err error
		if strings.TrimSpace(p.Query) != "" {
			r, err = c.SearchRepos(ctx, p.Query, ghLimit(p.Limit, 30))
		} else {
			r, err = c.MyRepos(ctx, ghLimit(p.Limit, 50))
		}
		return ghOrEmpty(r), err
	})

	ghRead(g, "github.issues", func(ctx context.Context, c *github.Client, p struct {
		Repo      string   `json:"repo"`
		State     string   `json:"state"`
		Labels    []string `json:"labels"`
		Assignee  string   `json:"assignee"`
		Creator   string   `json:"creator"`
		Milestone string   `json:"milestone"`
		Sort      string   `json:"sort"`
		Limit     int      `json:"limit"`
	}) ([]github.Issue, error) {
		r, err := c.ListIssues(ctx, p.Repo, github.IssueFilter{State: p.State, Labels: p.Labels, Assignee: p.Assignee, Creator: p.Creator,
			Milestone: p.Milestone, Sort: p.Sort, Limit: ghLimit(p.Limit, 50)})
		return ghOrEmpty(r), err
	})
	ghRead(g, "github.issue", func(ctx context.Context, c *github.Client, p ghRepoParam) (github.IssueDetail, error) {
		return c.GetIssue(ctx, p.Repo, p.Number)
	})
	ghRead(g, "github.labels", func(ctx context.Context, c *github.Client, p ghRepoParam) ([]github.Label, error) {
		r, err := c.Labels(ctx, p.Repo)
		return ghOrEmpty(r), err
	})
	ghRead(g, "github.milestones", func(ctx context.Context, c *github.Client, p ghRepoParam) ([]github.Milestone, error) {
		r, err := c.Milestones(ctx, p.Repo)
		return ghOrEmpty(r), err
	})

	type issueWrite struct {
		Repo      string   `json:"repo"`
		Number    int      `json:"number"`
		Title     *string  `json:"title"`
		Body      *string  `json:"body"`
		State     *string  `json:"state"`
		Labels    []string `json:"labels"`
		SetLabels bool     `json:"setLabels"`
		Assignees []string `json:"assignees"`
		SetAssign bool     `json:"setAssignees"`
		Milestone *int     `json:"milestone"` // 0 clears
		Confirmed bool     `json:"confirmed"`
	}
	input := func(p issueWrite) github.IssueInput {
		in := github.IssueInput{Title: p.Title, Body: p.Body, State: p.State, Milestone: p.Milestone}
		if p.SetLabels {
			l := ghOrEmpty(p.Labels)
			in.Labels = &l
		}
		if p.SetAssign {
			a := ghOrEmpty(p.Assignees)
			in.Assignees = &a
		}
		return in
	}
	handle(d, "github.issue.create", func(ctx context.Context, p issueWrite) (github.Issue, error) {
		c, err := g.conn()
		if err != nil {
			return github.Issue{}, err
		}
		in := input(p)
		in.Labels, in.Assignees = nil, nil
		if in.Milestone != nil && *in.Milestone == 0 {
			in.Milestone = nil
		}
		if len(p.Labels) > 0 {
			in.Labels = &p.Labels
		}
		if len(p.Assignees) > 0 {
			in.Assignees = &p.Assignees
		}
		is, err := c.CreateIssue(ghCtx(ctx, p.Confirmed), p.Repo, in)
		if err == nil {
			g.invalidate(ctx, "issue.create")
		}
		return is, err
	})
	handle(d, "github.issue.update", func(ctx context.Context, p issueWrite) (github.Issue, error) {
		c, err := g.conn()
		if err != nil {
			return github.Issue{}, err
		}
		is, err := c.UpdateIssue(ghCtx(ctx, p.Confirmed), p.Repo, p.Number, input(p))
		if err == nil {
			g.invalidate(ctx, "issue.update")
		}
		return is, err
	})
	handle(d, "github.issue.comment", func(ctx context.Context, p struct {
		Repo      string `json:"repo"`
		Number    int    `json:"number"`
		Body      string `json:"body"`
		Confirmed bool   `json:"confirmed"`
	}) (github.Comment, error) {
		c, err := g.conn()
		if err != nil {
			return github.Comment{}, err
		}
		cm, err := c.AddComment(ghCtx(ctx, p.Confirmed), p.Repo, p.Number, p.Body)
		if err == nil {
			g.invalidate(ctx, "issue.comment")
		}
		return cm, err
	})

	ghRead(g, "github.pulls", func(ctx context.Context, c *github.Client, p struct {
		Repo  string `json:"repo"`
		State string `json:"state"`
		Limit int    `json:"limit"`
	}) ([]github.PullRequest, error) {
		r, err := c.ListPulls(ctx, p.Repo, p.State, ghLimit(p.Limit, 30))
		return ghOrEmpty(r), err
	})
	ghRead(g, "github.pull", func(ctx context.Context, c *github.Client, p ghRepoParam) (github.PullRequest, error) {
		return c.GetPull(ctx, p.Repo, p.Number)
	})
	ghRead(g, "github.pull.status", func(ctx context.Context, c *github.Client, p ghRepoParam) (github.PullStatus, error) {
		return c.PullStatus(ctx, p.Repo, p.Number)
	})
	ghRead(g, "github.pull.files", func(ctx context.Context, c *github.Client, p ghRepoParam) ([]github.PullFile, error) {
		r, err := c.PullFiles(ctx, p.Repo, p.Number)
		return ghOrEmpty(r), err
	})
	ghRead(g, "github.pull.comments", func(ctx context.Context, c *github.Client, p ghRepoParam) ([]github.ReviewComment, error) {
		r, err := c.PullReviewComments(ctx, p.Repo, p.Number)
		return ghOrEmpty(r), err
	})

	ghRead(g, "github.notifications", func(ctx context.Context, c *github.Client, p struct {
		All   bool `json:"all"`
		Limit int  `json:"limit"`
	}) ([]github.Notification, error) {
		r, err := c.Notifications(ctx, p.All, ghLimit(p.Limit, 50))
		return ghOrEmpty(r), err
	})

	ghRead(g, "github.runs", func(ctx context.Context, c *github.Client, p struct {
		Repo   string `json:"repo"`
		Status string `json:"status"`
		Branch string `json:"branch"`
		Limit  int    `json:"limit"`
	}) ([]github.WorkflowRun, error) {
		r, err := c.Runs(ctx, p.Repo, github.RunFilter{Status: p.Status, Branch: p.Branch, Limit: ghLimit(p.Limit, 30)})
		return ghOrEmpty(r), err
	})
	// Recent failed runs across the user's most recently pushed repositories.
	ghRead(g, "github.runs.failed", func(ctx context.Context, c *github.Client, p struct {
		Repos int `json:"repos"`
	}) ([]github.WorkflowRun, error) {
		repos, err := c.MyRepos(ctx, ghLimit(p.Repos, 6))
		if err != nil {
			return nil, err
		}
		var names []string
		for _, r := range repos {
			if !r.Archived {
				names = append(names, r.FullName)
			}
		}
		r, err := c.FailedRuns(ctx, names, 3)
		return ghOrEmpty(r), err
	})
	ghRead(g, "github.run.jobs", func(ctx context.Context, c *github.Client, p struct {
		Repo  string `json:"repo"`
		RunID int64  `json:"runId"`
	}) ([]github.Job, error) {
		r, err := c.RunJobs(ctx, p.Repo, p.RunID)
		return ghOrEmpty(r), err
	})
	// run.logs returns the log of one job (jobId) or every file of the run archive.
	ghRead(g, "github.run.logs", func(ctx context.Context, c *github.Client, p struct {
		Repo  string `json:"repo"`
		RunID int64  `json:"runId"`
		JobID int64  `json:"jobId"`
	}) ([]github.LogFile, error) {
		if p.JobID != 0 {
			f, err := c.JobLog(ctx, p.Repo, p.JobID)
			return []github.LogFile{f}, err
		}
		r, err := c.RunLogs(ctx, p.Repo, p.RunID)
		return ghOrEmpty(r), err
	})
	ghRead(g, "github.checks", func(ctx context.Context, c *github.Client, p struct {
		Repo string `json:"repo"`
		Ref  string `json:"ref"`
	}) ([]github.CheckRun, error) {
		r, err := c.CheckRuns(ctx, p.Repo, p.Ref)
		return ghOrEmpty(r), err
	})

	// Server-side issue/PR search (command palette, dashboard widgets).
	ghRead(g, "github.search", func(ctx context.Context, c *github.Client, p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}) (github.SearchResult, error) {
		r, err := c.SearchIssues(ctx, p.Query, ghLimit(p.Limit, 15))
		r.Items = ghOrEmpty(r.Items)
		return r, err
	})

	handle(d, "github.clone", func(ctx context.Context, p struct {
		Repo      string `json:"repo"`
		DestDir   string `json:"destDir"`
		Confirmed bool   `json:"confirmed"`
	}) (GitHubCloneEvent, error) {
		c, err := g.conn()
		if err != nil {
			return GitHubCloneEvent{}, err
		}
		target, err := github.CloneTarget(p.Repo, p.DestDir)
		if err != nil {
			return GitHubCloneEvent{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
		}
		if err := c.Authorize(ghCtx(ctx, p.Confirmed), "github.clone", p.Repo+" -> "+target); err != nil {
			return GitHubCloneEvent{}, err
		}
		idb := make([]byte, 8)
		_, _ = rand.Read(idb)
		id := hex.EncodeToString(idb)
		ev := GitHubCloneEvent{ID: id, Repo: p.Repo, State: "running", Percent: -1, Target: target}
		d.IPC.Publish("github.clone", ev)
		go func() {
			// Outlives this request, but not the daemon or the clone deadline.
			cloneCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			go func() {
				select {
				case <-d.stop:
					cancel()
				case <-cloneCtx.Done():
				}
			}()
			_, err := github.Clone(cloneCtx, "", p.Repo, p.DestDir, func(pr github.CloneProgress) {
				d.IPC.Publish("github.clone", GitHubCloneEvent{ID: id, Repo: p.Repo, State: "running", Line: pr.Line, Percent: pr.Percent, Target: target})
			})
			end := GitHubCloneEvent{ID: id, Repo: p.Repo, State: "done", Percent: 100, Target: target}
			if err != nil {
				end.State, end.Percent, end.Error = "error", -1, err.Error()
				d.audit("github.clone", "github", "error", p.Repo)
			} else {
				d.audit("github.clone.done", "github", "allowed", p.Repo+" -> "+target)
			}
			d.IPC.Publish("github.clone", end)
		}()
		return ev, nil
	})
}

func ghLimit(v, def int) int {
	if v <= 0 {
		return def
	}
	return min(v, 200)
}

// QueryGitHub is the read-only automation adapter; it never accepts a write or confirmation.
func (g *ghService) QueryGitHub(ctx context.Context, p automations.QueryRequest) (any, error) {
	c, err := g.conn()
	if err != nil {
		return nil, err
	}
	limit := min(ghLimit(p.Limit, 30), 100)
	switch p.Kind {
	case "issues":
		return c.ListIssues(ctx, p.Repo, github.IssueFilter{Limit: limit})
	case "pulls":
		return c.ListPulls(ctx, p.Repo, "open", limit)
	case "checks":
		checks, err := c.CheckRuns(ctx, p.Repo, p.Query)
		if err != nil {
			return nil, err
		}
		failed := 0
		for _, check := range checks {
			switch check.Conclusion {
			case "failure", "timed_out", "cancelled", "action_required":
				failed++
			}
		}
		return map[string]any{"checks": ghOrEmpty(checks), "failed": failed, "repo": p.Repo, "ref": p.Query}, nil
	case "search":
		return c.SearchIssues(ctx, p.Query, limit)
	default:
		return nil, errors.New("github: unknown read-only query kind")
	}
}

package github

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveReadOnly exercises the real GitHub API with the machine's `gh` login.
// Read-only. Run: OROLE_GITHUB_LIVE=1 OROLE_GITHUB_REPO=owner/name go test -run Live -v ./internal/github
func TestLiveReadOnly(t *testing.T) {
	if os.Getenv("OROLE_GITHUB_LIVE") == "" {
		t.Skip("set OROLE_GITHUB_LIVE=1 to run against api.github.com")
	}
	repo := os.Getenv("OROLE_GITHUB_REPO")
	if repo == "" {
		repo = "rayorole/orole-os-desktop"
	}
	c := NewClient(&GHCLISource{})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	me, err := c.Me(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("user: %s", me.Login)

	repos, err := c.MyRepos(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range repos {
		t.Logf("repo: %-40s private=%v pushed=%s", r.FullName, r.Private, r.PushedAt.Format("2006-01-02"))
	}

	issues, err := c.ListIssues(ctx, repo, IssueFilter{State: "all", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d issues listed (state=all, limit 100)", repo, len(issues))
	if len(issues) == 0 {
		t.Fatal("no issues")
	}
	for _, i := range issues[:min(5, len(issues))] {
		t.Logf("  #%d [%s] %s (labels=%d)", i.Number, i.State, i.Title, len(i.Labels))
	}
	d, err := c.GetIssue(ctx, repo, issues[0].Number)
	if err != nil {
		t.Fatal(err)
	}
	body := string(d.Issue.Body)
	t.Logf("issue #%d %q by %s, %d comments, body %d bytes", d.Issue.Number, d.Issue.Title, d.Issue.User.Login, len(d.Comments), len(body))

	// Second identical call must be served by ETag revalidation.
	if _, err := c.ListIssues(ctx, repo, IssueFilter{State: "all", Limit: 100}); err != nil {
		t.Fatal(err)
	}
	for _, l := range c.RateLimits() {
		t.Logf("rate %s: %d/%d resets %s", l.Resource, l.Remaining, l.Limit, l.ResetAt.Format(time.RFC3339))
	}
	if runs, err := c.Runs(ctx, repo, RunFilter{Limit: 3}); err == nil {
		t.Logf("%d recent workflow runs", len(runs))
	} else {
		t.Logf("runs: %v", err)
	}
}

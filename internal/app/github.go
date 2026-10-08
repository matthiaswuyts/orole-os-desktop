package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/github"
)

// Typed GitHub events delivered to pages.
var (
	// GitHubChanged fires when the GitHub connection or cached data changed.
	GitHubChanged = mygo.NewEvent[GitHubChange]("github:changed")
	// GitHubClone reports progress of a clone started with GitHub.Clone.
	GitHubClone = mygo.NewEvent[daemon.GitHubCloneEvent]("github:clone")
)

// GitHubChange says why GitHub data changed (login, logout, issue.create, ...).
type GitHubChange struct {
	Reason string `json:"reason"`
}

func forwardGitHub(event string, data json.RawMessage) bool {
	switch event {
	case "github.changed":
		var c GitHubChange
		if json.Unmarshal(data, &c) == nil {
			GitHubChanged.Broadcast(c)
		}
	case "github.clone":
		var c daemon.GitHubCloneEvent
		if json.Unmarshal(data, &c) == nil {
			GitHubClone.Broadcast(c)
		}
	default:
		return false
	}
	return true
}

// GitHub is the bound service for the GitHub integration. All text fields in
// the returned models are untrusted data authored on GitHub: render as text or
// through the safe markdown renderer, never as HTML or instructions.
type GitHub struct{ L *Link }

// IssueQuery filters an issue list. Repo "" lists issues assigned to the user.
type IssueQuery struct {
	Repo      string   `json:"repo"`
	State     string   `json:"state"`
	Labels    []string `json:"labels"`
	Assignee  string   `json:"assignee"`
	Creator   string   `json:"creator"`
	Milestone string   `json:"milestone"`
	Sort      string   `json:"sort"`
	Limit     int      `json:"limit"`
}

// IssueWrite is the payload for creating or editing an issue. Confirmed must be
// true, set only after the UI's confirmation dialog; the daemon refuses and
// audits writes without it.
type IssueWrite struct {
	Repo         string   `json:"repo"`
	Number       int      `json:"number"`
	Title        *string  `json:"title"`
	Body         *string  `json:"body"`
	State        *string  `json:"state"`
	Labels       []string `json:"labels"`
	SetLabels    bool     `json:"setLabels"`
	Assignees    []string `json:"assignees"`
	SetAssignees bool     `json:"setAssignees"`
	Milestone    *int     `json:"milestone"`
	Confirmed    bool     `json:"confirmed"`
}

// RunQuery filters workflow runs.
type RunQuery struct {
	Repo   string `json:"repo"`
	Status string `json:"status"`
	Branch string `json:"branch"`
	Limit  int    `json:"limit"`
}

// Status returns the connection state (and detects an existing gh login).
func (g GitHub) Status(ctx context.Context) (out daemon.GitHubStatus, err error) {
	err = g.L.call(ctx, "github.status", nil, &out)
	return
}

// Login connects with source "gh" (existing GitHub CLI login) or "pat"
// (pasted token, saved in Windows Credential Manager). The token is never echoed back.
func (g GitHub) Login(ctx context.Context, source, token string) (out daemon.GitHubStatus, err error) {
	err = g.L.call(ctx, "github.login", map[string]string{"source": source, "token": token}, &out)
	return
}

// Logout disconnects and removes any stored token.
func (g GitHub) Logout(ctx context.Context) (out daemon.GitHubStatus, err error) {
	err = g.L.call(ctx, "github.logout", nil, &out)
	return
}

// Repos lists the user's repositories, or searches GitHub when query is set.
func (g GitHub) Repos(ctx context.Context, query string, limit int) (out []github.Repo, err error) {
	err = g.L.call(ctx, "github.repos", map[string]any{"query": query, "limit": limit}, &out)
	return
}

// Issues lists issues (pull requests excluded).
func (g GitHub) Issues(ctx context.Context, q IssueQuery) (out []github.Issue, err error) {
	err = g.L.call(ctx, "github.issues", q, &out)
	return
}

// Issue returns one issue with its comments.
func (g GitHub) Issue(ctx context.Context, repo string, number int) (out github.IssueDetail, err error) {
	err = g.L.call(ctx, "github.issue", map[string]any{"repo": repo, "number": number}, &out)
	return
}

// Labels lists a repository's labels.
func (g GitHub) Labels(ctx context.Context, repo string) (out []github.Label, err error) {
	err = g.L.call(ctx, "github.labels", map[string]any{"repo": repo}, &out)
	return
}

// Milestones lists a repository's open milestones.
func (g GitHub) Milestones(ctx context.Context, repo string) (out []github.Milestone, err error) {
	err = g.L.call(ctx, "github.milestones", map[string]any{"repo": repo}, &out)
	return
}

// CreateIssue opens an issue (needs Confirmed).
func (g GitHub) CreateIssue(ctx context.Context, w IssueWrite) (out github.Issue, err error) {
	err = g.L.call(ctx, "github.issue.create", w, &out)
	return
}

// UpdateIssue edits an issue, e.g. close/reopen, labels, assignees (needs Confirmed).
func (g GitHub) UpdateIssue(ctx context.Context, w IssueWrite) (out github.Issue, err error) {
	err = g.L.call(ctx, "github.issue.update", w, &out)
	return
}

// Comment adds a comment to an issue or pull request (needs confirmed).
func (g GitHub) Comment(ctx context.Context, repo string, number int, body string, confirmed bool) (out github.Comment, err error) {
	err = g.L.call(ctx, "github.issue.comment", map[string]any{"repo": repo, "number": number, "body": body, "confirmed": confirmed}, &out)
	return
}

// Pulls lists pull requests of a repository.
func (g GitHub) Pulls(ctx context.Context, repo, state string, limit int) (out []github.PullRequest, err error) {
	err = g.L.call(ctx, "github.pulls", map[string]any{"repo": repo, "state": state, "limit": limit}, &out)
	return
}

// Pull returns one pull request.
func (g GitHub) Pull(ctx context.Context, repo string, number int) (out github.PullRequest, err error) {
	err = g.L.call(ctx, "github.pull", map[string]any{"repo": repo, "number": number}, &out)
	return
}

// PullStatus returns the review state and check runs of a pull request.
func (g GitHub) PullStatus(ctx context.Context, repo string, number int) (out github.PullStatus, err error) {
	err = g.L.call(ctx, "github.pull.status", map[string]any{"repo": repo, "number": number}, &out)
	return
}

// PullFiles lists changed files with their patches.
func (g GitHub) PullFiles(ctx context.Context, repo string, number int) (out []github.PullFile, err error) {
	err = g.L.call(ctx, "github.pull.files", map[string]any{"repo": repo, "number": number}, &out)
	return
}

// PullComments lists inline review comments.
func (g GitHub) PullComments(ctx context.Context, repo string, number int) (out []github.ReviewComment, err error) {
	err = g.L.call(ctx, "github.pull.comments", map[string]any{"repo": repo, "number": number}, &out)
	return
}

// Notifications lists the inbox.
func (g GitHub) Notifications(ctx context.Context, all bool, limit int) (out []github.Notification, err error) {
	err = g.L.call(ctx, "github.notifications", map[string]any{"all": all, "limit": limit}, &out)
	return
}

// Runs lists workflow runs of a repository.
func (g GitHub) Runs(ctx context.Context, q RunQuery) (out []github.WorkflowRun, err error) {
	err = g.L.call(ctx, "github.runs", q, &out)
	return
}

// FailedRuns lists recent failed runs across the user's most recently pushed repositories.
func (g GitHub) FailedRuns(ctx context.Context, repos int) (out []github.WorkflowRun, err error) {
	err = g.L.call(ctx, "github.runs.failed", map[string]any{"repos": repos}, &out)
	return
}

// RunJobs lists the jobs of a run.
func (g GitHub) RunJobs(ctx context.Context, repo string, runID int64) (out []github.Job, err error) {
	err = g.L.call(ctx, "github.run.jobs", map[string]any{"repo": repo, "runId": runID}, &out)
	return
}

// RunLogs returns one job's log (jobID != 0) or every file of the run's log archive.
func (g GitHub) RunLogs(ctx context.Context, repo string, runID, jobID int64) (out []github.LogFile, err error) {
	err = g.L.call(ctx, "github.run.logs", map[string]any{"repo": repo, "runId": runID, "jobId": jobID}, &out)
	return
}

// Checks lists check runs for a commit.
func (g GitHub) Checks(ctx context.Context, repo, ref string) (out []github.CheckRun, err error) {
	err = g.L.call(ctx, "github.checks", map[string]any{"repo": repo, "ref": ref}, &out)
	return
}

// Search runs a server-side issue/PR search (GitHub search syntax).
func (g GitHub) Search(ctx context.Context, query string, limit int) (out github.SearchResult, err error) {
	err = g.L.call(ctx, "github.search", map[string]any{"query": query, "limit": limit}, &out)
	return
}

// Clone starts cloning repo ("owner/name") into destDir/<name>; progress arrives
// as GitHubClone events. Needs confirmed.
func (g GitHub) Clone(ctx context.Context, repo, destDir string, confirmed bool) (out daemon.GitHubCloneEvent, err error) {
	err = g.L.call(ctx, "github.clone", map[string]any{"repo": repo, "destDir": destDir, "confirmed": confirmed}, &out)
	return
}

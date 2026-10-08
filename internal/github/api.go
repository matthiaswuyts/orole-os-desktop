package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

// ValidRepo reports whether s is an "owner/name" repository reference.
func ValidRepo(s string) bool {
	o, n, _ := strings.Cut(s, "/")
	return repoRe.MatchString(s) && o != "." && o != ".." && n != "." && n != ".."
}

func repoPath(repo string) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("github: invalid repository %q (want owner/name)", repo)
	}
	o, n, _ := strings.Cut(repo, "/")
	return "/repos/" + url.PathEscape(o) + "/" + url.PathEscape(n), nil
}

func repoFromURL(u string) string {
	if i := strings.Index(u, "/repos/"); i >= 0 {
		return u[i+len("/repos/"):]
	}
	return ""
}

// Me returns the authenticated user.
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	return u, c.getJSON(ctx, "/user", nil, &u)
}

// MyRepos lists repositories the user can access, most recently pushed first.
func (c *Client) MyRepos(ctx context.Context, limit int) ([]Repo, error) {
	q := url.Values{"sort": {"pushed"}, "affiliation": {"owner,collaborator,organization_member"}}
	return list[Repo](ctx, c, "/user/repos", q, limit)
}

// SearchRepos searches public and accessible repositories.
func (c *Client) SearchRepos(ctx context.Context, query string, limit int) ([]Repo, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("github: empty search query")
	}
	return getAll(ctx, c, "/search/repositories", url.Values{"q": {query}}, min(limit, 100), wrapped[Repo]("items"))
}

func fixIssues(in []Issue) []Issue {
	for i := range in {
		in[i].Repo = repoFromURL(in[i].RepositoryURL)
		if in[i].Labels == nil {
			in[i].Labels = []Label{}
		}
	}
	return in
}

// ListIssues lists issues of repo (or the user's assigned issues across repos
// when repo is ""). Pull requests are excluded.
func (c *Client) ListIssues(ctx context.Context, repo string, f IssueFilter) ([]Issue, error) {
	q := url.Values{}
	state := f.State
	if state == "" {
		state = "open"
	}
	q.Set("state", state)
	if len(f.Labels) > 0 {
		q.Set("labels", strings.Join(f.Labels, ","))
	}
	for k, v := range map[string]string{"assignee": f.Assignee, "creator": f.Creator, "mentioned": f.Mentioned, "milestone": f.Milestone, "sort": f.Sort, "direction": f.Direction} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if !f.Since.IsZero() {
		q.Set("since", f.Since.UTC().Format(time.RFC3339))
	}
	path := "/issues"
	if repo != "" {
		rp, err := repoPath(repo)
		if err != nil {
			return nil, err
		}
		path = rp + "/issues"
	} else {
		q.Set("filter", "assigned")
		q.Del("assignee")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	// Filter each page before counting toward the limit: a repository can have
	// many more pull requests than issues.
	return getAll(ctx, c, path, q, limit, func(b []byte) ([]Issue, error) {
		var page []Issue
		if err := json.Unmarshal(b, &page); err != nil {
			return nil, err
		}
		out := page[:0]
		for _, is := range fixIssues(page) {
			if is.PullRequest == nil {
				if is.Repo == "" {
					is.Repo = repo
				}
				out = append(out, is)
			}
		}
		return out, nil
	})
}

// GetIssue returns an issue with up to 100 comments.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (IssueDetail, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return IssueDetail{}, err
	}
	var d IssueDetail
	p := rp + "/issues/" + strconv.Itoa(number)
	if err := c.getJSON(ctx, p, nil, &d.Issue); err != nil {
		return d, err
	}
	d.Issue.Repo = repo
	if d.Issue.Labels == nil {
		d.Issue.Labels = []Label{}
	}
	d.Comments = []Comment{}
	if d.Issue.Comments > 0 {
		if d.Comments, err = list[Comment](ctx, c, p+"/comments", nil, 100); err != nil {
			return d, err
		}
	}
	return d, nil
}

// CreateIssue opens an issue. Requires the Authorize hook to allow it.
func (c *Client) CreateIssue(ctx context.Context, repo string, in IssueInput) (Issue, error) {
	var is Issue
	rp, err := repoPath(repo)
	if err != nil {
		return is, err
	}
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return is, fmt.Errorf("github: issue title is required")
	}
	in.State, in.StateReason = nil, nil
	err = c.write(ctx, "github.issue.create", repo+": "+*in.Title, "POST", rp+"/issues", in, &is)
	is.Repo = repo
	return is, err
}

// UpdateIssue edits an issue. Milestone 0 clears the milestone.
func (c *Client) UpdateIssue(ctx context.Context, repo string, number int, in IssueInput) (Issue, error) {
	var is Issue
	rp, err := repoPath(repo)
	if err != nil {
		return is, err
	}
	m := map[string]any{}
	if in.Title != nil {
		m["title"] = *in.Title
	}
	if in.Body != nil {
		m["body"] = *in.Body
	}
	if in.State != nil {
		if *in.State != "open" && *in.State != "closed" {
			return is, fmt.Errorf("github: state must be open or closed")
		}
		m["state"] = *in.State
	}
	if in.StateReason != nil {
		m["state_reason"] = *in.StateReason
	}
	if in.Labels != nil {
		m["labels"] = *in.Labels
	}
	if in.Assignees != nil {
		m["assignees"] = *in.Assignees
	}
	if in.Milestone != nil {
		if *in.Milestone == 0 {
			m["milestone"] = nil
		} else {
			m["milestone"] = *in.Milestone
		}
	}
	if len(m) == 0 {
		return is, fmt.Errorf("github: nothing to update")
	}
	detail := fmt.Sprintf("%s#%d", repo, number)
	err = c.write(ctx, "github.issue.update", detail, "PATCH", rp+"/issues/"+strconv.Itoa(number), m, &is)
	is.Repo = repo
	return is, err
}

// AddComment comments on an issue or pull request.
func (c *Client) AddComment(ctx context.Context, repo string, number int, body string) (Comment, error) {
	var cm Comment
	rp, err := repoPath(repo)
	if err != nil {
		return cm, err
	}
	if strings.TrimSpace(body) == "" {
		return cm, fmt.Errorf("github: comment body is required")
	}
	err = c.write(ctx, "github.comment.create", fmt.Sprintf("%s#%d", repo, number), "POST",
		rp+"/issues/"+strconv.Itoa(number)+"/comments", map[string]string{"body": body}, &cm)
	return cm, err
}

// Labels lists a repository's labels.
func (c *Client) Labels(ctx context.Context, repo string) ([]Label, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	return list[Label](ctx, c, rp+"/labels", nil, 200)
}

// Milestones lists a repository's open milestones.
func (c *Client) Milestones(ctx context.Context, repo string) ([]Milestone, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	return list[Milestone](ctx, c, rp+"/milestones", url.Values{"state": {"open"}}, 100)
}

// ListPulls lists pull requests of repo (state open|closed|all).
func (c *Client) ListPulls(ctx context.Context, repo, state string, limit int) ([]PullRequest, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if state == "" {
		state = "open"
	}
	return list[PullRequest](ctx, c, rp+"/pulls", url.Values{"state": {state}, "sort": {"updated"}, "direction": {"desc"}}, limit)
}

// GetPull returns one pull request.
func (c *Client) GetPull(ctx context.Context, repo string, number int) (PullRequest, error) {
	var p PullRequest
	rp, err := repoPath(repo)
	if err != nil {
		return p, err
	}
	return p, c.getJSON(ctx, rp+"/pulls/"+strconv.Itoa(number), nil, &p)
}

// PullFiles lists changed files with patches (up to 300, GitHub's API cap).
func (c *Client) PullFiles(ctx context.Context, repo string, number int) ([]PullFile, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	return list[PullFile](ctx, c, rp+"/pulls/"+strconv.Itoa(number)+"/files", nil, 300)
}

// PullReviewComments lists inline review comments.
func (c *Client) PullReviewComments(ctx context.Context, repo string, number int) ([]ReviewComment, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	return list[ReviewComment](ctx, c, rp+"/pulls/"+strconv.Itoa(number)+"/comments", nil, 200)
}

// CheckRuns lists check runs for a commit.
func (c *Client) CheckRuns(ctx context.Context, repo, ref string) ([]CheckRun, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	if ref == "" || strings.ContainsAny(ref, " ?#\\") {
		return nil, fmt.Errorf("github: invalid ref")
	}
	return getAll(ctx, c, rp+"/commits/"+url.PathEscape(ref)+"/check-runs", nil, 100, wrapped[CheckRun]("check_runs"))
}

// SummarizeChecks rolls runs up into counts.
func SummarizeChecks(runs []CheckRun) ChecksSummary {
	s := ChecksSummary{Total: len(runs)}
	for _, r := range runs {
		switch {
		case r.Status != "completed":
			s.Pending++
		case r.Conclusion == "success" || r.Conclusion == "neutral" || r.Conclusion == "skipped":
			s.Passed++
		default:
			s.Failed++
		}
	}
	return s
}

// ReviewState reduces reviews to approved / changes_requested / commented / none,
// using each reviewer's latest decisive review.
func ReviewState(reviews []Review) string {
	latest := map[string]string{}
	seenAny := false
	for _, r := range reviews { // API returns chronological order
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED":
			latest[r.User.Login] = r.State
			seenAny = true
		case "DISMISSED":
			delete(latest, r.User.Login)
		case "COMMENTED":
			seenAny = true
		}
	}
	approved := false
	for _, s := range latest {
		if s == "CHANGES_REQUESTED" {
			return "changes_requested"
		}
		approved = approved || s == "APPROVED"
	}
	switch {
	case approved:
		return "approved"
	case seenAny:
		return "commented"
	}
	return "none"
}

// PullStatus fetches reviews and check runs for a pull request.
func (c *Client) PullStatus(ctx context.Context, repo string, number int) (PullStatus, error) {
	st := PullStatus{ReviewState: "none", Reviews: []Review{}, CheckRuns: []CheckRun{}}
	rp, err := repoPath(repo)
	if err != nil {
		return st, err
	}
	pr, err := c.GetPull(ctx, repo, number)
	if err != nil {
		return st, err
	}
	if st.Reviews, err = list[Review](ctx, c, rp+"/pulls/"+strconv.Itoa(number)+"/reviews", nil, 100); err != nil {
		return st, err
	}
	st.ReviewState = ReviewState(st.Reviews)
	if pr.Head.SHA != "" {
		if st.CheckRuns, err = c.CheckRuns(ctx, repo, pr.Head.SHA); err != nil {
			return st, err
		}
	}
	st.Checks = SummarizeChecks(st.CheckRuns)
	return st, nil
}

// Notifications lists inbox threads.
func (c *Client) Notifications(ctx context.Context, all bool, limit int) ([]Notification, error) {
	q := url.Values{}
	if all {
		q.Set("all", "true")
	}
	return list[Notification](ctx, c, "/notifications", q, limit)
}

// RunFilter narrows workflow runs.
type RunFilter struct {
	Status string // e.g. failure, success, in_progress, completed
	Branch string
	Event  string
	Limit  int
}

// Runs lists workflow runs of repo.
func (c *Client) Runs(ctx context.Context, repo string, f RunFilter) ([]WorkflowRun, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for k, v := range map[string]string{"status": f.Status, "branch": f.Branch, "event": f.Event} {
		if v != "" {
			q.Set(k, v)
		}
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 30
	}
	runs, err := getAll(ctx, c, rp+"/actions/runs", q, limit, wrapped[WorkflowRun]("workflow_runs"))
	for i := range runs {
		runs[i].Repo = repo
	}
	return runs, err
}

// RunJobs lists the jobs of a run (failed jobs are the interesting ones).
func (c *Client) RunJobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	return getAll(ctx, c, rp+"/actions/runs/"+strconv.FormatInt(runID, 10)+"/jobs", url.Values{"filter": {"latest"}}, 100, wrapped[Job]("jobs"))
}

// FailedRuns returns recent failed workflow runs across repos.
func (c *Client) FailedRuns(ctx context.Context, repos []string, perRepo int) ([]WorkflowRun, error) {
	var out []WorkflowRun
	var firstErr error
	for _, r := range repos {
		runs, err := c.Runs(ctx, r, RunFilter{Status: "failure", Limit: perRepo})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, runs...)
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// SearchIssues runs an issue/PR search (query uses GitHub search syntax).
func (c *Client) SearchIssues(ctx context.Context, query string, limit int) (SearchResult, error) {
	var res SearchResult
	if strings.TrimSpace(query) == "" {
		return res, fmt.Errorf("github: empty search query")
	}
	if limit <= 0 {
		limit = 20
	}
	r, err := c.request(ctx, "GET", "/search/issues", url.Values{"q": {query}, "per_page": {strconv.Itoa(min(limit, 100))}, "sort": {"updated"}}, nil)
	if err != nil {
		return res, err
	}
	var raw struct {
		Total int     `json:"total_count"`
		Items []Issue `json:"items"`
	}
	if err := json.Unmarshal(r.body, &raw); err != nil {
		return res, err
	}
	res.Total, res.Items = raw.Total, fixIssues(raw.Items)
	return res, nil
}

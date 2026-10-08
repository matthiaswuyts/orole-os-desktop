package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

func publicationRefs(head, base string) error {
	if !git.ValidRef(head) || head == "HEAD" || !git.ValidRef(base) || base == "HEAD" || head == base {
		return fmt.Errorf("github: distinct head and base branch names are required")
	}
	return nil
}

// CreatePull creates a draft only, after the client's write authorizer approves.
// After an ambiguous failure callers must use FindPullByHead before retrying.
func (c *Client) CreatePull(ctx context.Context, repo, title, body, head, base string) (PullRequest, error) {
	var pr PullRequest
	rp, err := repoPath(repo)
	if err != nil {
		return pr, err
	}
	if err := publicationRefs(head, base); err != nil {
		return pr, err
	}
	if strings.TrimSpace(title) == "" {
		return pr, fmt.Errorf("github: pull request title is required")
	}
	in := struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  string `json:"head"`
		Base  string `json:"base"`
		Draft bool   `json:"draft"`
	}{title, body, head, base, true}
	err = c.write(ctx, "github.pull.create", repo, http.MethodPost, rp+"/pulls", in, &pr)
	return pr, err
}

// FindPullByHead finds a same-repository PR, including closed or merged PRs.
// It deliberately does not authorize a write. This lookup is the retry guard for
// an earlier publication whose response may have been lost.
func (c *Client) FindPullByHead(ctx context.Context, repo, head, base string) (PullRequest, bool, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return PullRequest{}, false, err
	}
	if err := publicationRefs(head, base); err != nil {
		return PullRequest{}, false, err
	}
	owner, _, _ := strings.Cut(repo, "/")
	q := url.Values{"state": {"all"}, "head": {owner + ":" + head}, "base": {base}, "per_page": {"100"}, "sort": {"created"}, "direction": {"asc"}}
	next := rp + "/pulls"
	for page := 0; next != ""; page++ {
		if page == 50 {
			return PullRequest{}, false, fmt.Errorf("github: pull retry lookup exceeded pagination limit")
		}
		r, err := c.request(ctx, http.MethodGet, next, q, nil)
		if err != nil {
			return PullRequest{}, false, err
		}
		var prs []PullRequest
		if err := json.Unmarshal(r.body, &prs); err != nil {
			return PullRequest{}, false, err
		}
		for _, pr := range prs {
			if string(pr.Head.Ref) == head && string(pr.Base.Ref) == base && pr.Head.Repo != nil && pr.Base.Repo != nil && strings.EqualFold(pr.Head.Repo.FullName, repo) && strings.EqualFold(pr.Base.Repo.FullName, repo) {
				return pr, true, nil
			}
		}
		next, q = nextLink(r.header), nil
	}
	return PullRequest{}, false, nil
}

// BranchHead resolves a GitHub branch to its actual commit object. Publication
// compares this with the approved local base so its diff matches the PR target.
func (c *Client) BranchHead(ctx context.Context, repo, branch string) (string, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	if !git.ValidRef(branch) || branch == "HEAD" {
		return "", fmt.Errorf("github: invalid branch name")
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if err := c.getJSON(ctx, rp+"/git/ref/heads/"+url.PathEscape(branch), nil, &ref); err != nil {
		return "", err
	}
	sha := ref.Object.SHA
	if ref.Ref != "refs/heads/"+branch || ref.Object.Type != "commit" || (len(sha) != 40 && len(sha) != 64) || strings.Trim(sha, "0123456789abcdef") != "" {
		return "", fmt.Errorf("github: branch did not resolve to an exact commit")
	}
	return sha, nil
}

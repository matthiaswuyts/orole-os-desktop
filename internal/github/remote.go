package github

import (
	"context"
	"net/url"
	"strings"
)

// RepoFromRemote returns the lower-cased "owner/name" of a github.com remote URL, or "" when
// remote is anything else (another host, a port, a query, a local path, a non-https/ssh scheme).
// Only https://github.com/o/r[.git], ssh://git@github.com/o/r[.git] and git@github.com:o/r[.git]
// are accepted.
func RepoFromRemote(remote string) string {
	if strings.HasPrefix(remote, "git@github.com:") {
		remote = "ssh://git@github.com/" + strings.TrimPrefix(remote, "git@github.com:")
	}
	u, err := url.Parse(remote)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "ssh") {
		return ""
	}
	r := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
	if !ValidRepo(r) {
		return ""
	}
	return strings.ToLower(r)
}

// GetRepo returns one repository, including its default branch.
func (c *Client) GetRepo(ctx context.Context, repo string) (Repo, error) {
	var r Repo
	rp, err := repoPath(repo)
	if err != nil {
		return r, err
	}
	return r, c.getJSON(ctx, rp, nil, &r)
}

package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

// Diff is a parsed unified diff (shared with the Git views).
type Diff = git.DiffResult

// DiffFile is one changed file of a Diff.
type DiffFile = git.DiffFile

// Worktrees is what the agent runtime needs from the projects/git layer. Orchestration
// ("try N ways") and the Review view depend only on this interface.
type Worktrees interface {
	// Create makes a new worktree of the repository containing repo on a new branch derived
	// from name, started from base ("" = HEAD), and returns its path and branch.
	Create(ctx context.Context, repo, name, base string) (path, branch string, err error)
	// Remove deletes a worktree the runtime created, discarding uncommitted changes.
	Remove(ctx context.Context, repo, path string) error
	// Head returns the commit dir is at ("" and an error when dir is not a git work tree).
	Head(ctx context.Context, dir string) (string, error)
	// Diff returns everything dir changed relative to base, including untracked files and
	// commits made on top of base.
	Diff(ctx context.Context, dir, base string) (Diff, error)
}

// GitWorktrees implements Worktrees on internal/git. Worktrees are created under
// Root/<repo-name>/<branch-slug>.
type GitWorktrees struct{ Root string }

var slugRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(s, "-"), "-.")
	if s == "" {
		return "wt"
	}
	return s
}

func (g GitWorktrees) Create(ctx context.Context, repo, name, base string) (string, string, error) {
	top, err := git.Toplevel(ctx, repo)
	if err != nil {
		return "", "", fmt.Errorf("%s is not a git repository: %w", repo, err)
	}
	branch := "agent/" + slug(name)
	path := filepath.Join(g.Root, slug(filepath.Base(top)), slug(name))
	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("worktree path %s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	if err := git.WorktreeAdd(ctx, top, path, branch, base); err != nil {
		return "", "", err
	}
	return path, branch, nil
}

func (g GitWorktrees) Remove(ctx context.Context, repo, path string) error {
	top, err := git.Toplevel(ctx, repo)
	if err != nil {
		return err
	}
	return git.WorktreeRemove(ctx, top, path, true)
}

func (g GitWorktrees) Head(ctx context.Context, dir string) (string, error) {
	return git.ResolveCommit(ctx, dir, "HEAD")
}

// Diff uses the shared isolated review runner; repository filters and hooks are
// never executed by automatic statistics or manual review.
func (g GitWorktrees) Diff(ctx context.Context, dir, base string) (Diff, error) {
	return git.DiffWorktree(ctx, dir, base)
}

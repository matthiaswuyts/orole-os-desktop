package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

// ErrWorktreeExists is returned by Worktrees.Create when the branch or directory derived from
// the requested name is taken. Start retries once with a short unique suffix.
var ErrWorktreeExists = errors.New("worktree or branch already exists")

// BranchPrefix is prepended to the slug of every branch the runtime creates for a session.
const BranchPrefix = "orole/"

// Diff is a parsed unified diff (shared with the Git views).
type Diff = git.DiffResult

// DiffFile is one changed file of a Diff.
type DiffFile = git.DiffFile

// Worktrees is what the agent runtime needs from the projects/git layer. Orchestration
// ("try N ways") and the Review view depend only on this interface.
type Worktrees interface {
	// Create makes a new worktree of the repository containing repo on a new branch
	// "orole/<slug of name>", started from base ("" = HEAD), and returns its path and branch.
	// It returns an error wrapping ErrWorktreeExists when that branch or directory is taken.
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
	branch := BranchPrefix + slug(name)
	path := filepath.Join(g.Root, slug(filepath.Base(top)), slug(name))
	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("%w: %s", ErrWorktreeExists, path)
	}
	if ok, err := git.BranchExists(ctx, top, branch); err != nil {
		return "", "", err
	} else if ok {
		return "", "", fmt.Errorf("%w: branch %s", ErrWorktreeExists, branch)
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

// branchSlug turns free text into a lower-case branch-name part of at most 40 characters
// ("" when no ASCII letter or digit remains).
func branchSlug(s string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}

// createWorktree makes the session's worktree. An explicit spec.Branch is used as given. Otherwise
// the branch is named after the task (its title, else the first line of the prompt); when that
// name is taken a short suffix from the session id makes it unique.
func (m *Manager) createWorktree(ctx context.Context, repo string, spec StartSpec, s Session) (path, branch string, err error) {
	if spec.Branch != "" {
		return m.cfg.Worktrees.Create(ctx, repo, spec.Branch, spec.BaseRef)
	}
	name := branchSlug(s.Title)
	if name == "" {
		name = branchSlug(firstLine(s.Task))
	}
	if name == "" {
		name = newBranchName(spec.Kind, s.ID)
	}
	path, branch, err = m.cfg.Worktrees.Create(ctx, repo, name, spec.BaseRef)
	if errors.Is(err, ErrWorktreeExists) {
		path, branch, err = m.cfg.Worktrees.Create(ctx, repo, name+"-"+s.ID[:6], spec.BaseRef)
	}
	return path, branch, err
}

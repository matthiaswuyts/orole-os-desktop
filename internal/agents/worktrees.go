package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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

const maxDiffBytes = 8 << 20

// Diff stages everything into a throwaway copy of the index (so the real index and work
// tree are untouched) and diffs that against base.
func (g GitWorktrees) Diff(ctx context.Context, dir, base string) (Diff, error) {
	if base == "" || !git.ValidRef(base) {
		return Diff{}, fmt.Errorf("invalid base ref %q", base)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	idxOut, err := gitOut(ctx, dir, nil, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return Diff{}, err
	}
	tmp, err := os.CreateTemp("", "orole-index-*")
	if err != nil {
		return Diff{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if src, err := os.Open(strings.TrimSpace(string(idxOut))); err == nil {
		_, _ = io.Copy(tmp, src)
		src.Close()
	}
	tmp.Close()
	env := []string{"GIT_INDEX_FILE=" + tmpPath}
	if _, err := gitOut(ctx, dir, env, "add", "-A", "--", "."); err != nil {
		return Diff{}, err
	}
	out, err := gitOut(ctx, dir, env, "diff", "--cached", "--no-color", "--no-ext-diff", "--no-textconv", "-M",
		"--src-prefix=a/", "--dst-prefix=b/", base, "--")
	if err != nil {
		return Diff{}, err
	}
	res := git.ParseDiff(string(out))
	if len(out) >= maxDiffBytes {
		res.Truncated = true
	}
	return res, nil
}

func gitOut(ctx context.Context, dir string, extraEnv []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotepath=off"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, extraEnv...)
	hideWindow(cmd)
	var out, errb bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, max: maxDiffBytes}
	cmd.Stderr = &limitWriter{w: &errb, max: 64 << 10}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) || ctx.Err() == nil {
			return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
		}
		return nil, ctx.Err()
	}
	return out.Bytes(), nil
}

// limitWriter drops output past max rather than failing the command.
type limitWriter struct {
	w   *bytes.Buffer
	max int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if room := l.max - l.w.Len(); room > 0 {
		if len(p) > room {
			l.w.Write(p[:room])
		} else {
			l.w.Write(p)
		}
	}
	return len(p), nil
}

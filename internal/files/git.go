package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

// NewGit returns the "git" provider: the local path policy applied to project
// (git repository) roots only. Compared with the local provider it is
// read-only inside .git (repository state is changed by git, not by file
// edits) and its listings carry `git status` markers. Its roots are its own:
// approving a directory for the local provider grants nothing here.
func NewGit(src RootSource) *Local {
	l := &Local{id: "git", src: src}
	l.rootCheck = func(real string) error {
		if _, err := os.Lstat(filepath.Join(real, ".git")); err != nil {
			return fmt.Errorf("%w: %s is not a git repository", ErrDenied, real)
		}
		return nil
	}
	l.guard = func(clean string) error {
		vol := filepath.VolumeName(clean)
		for _, c := range splitComponents(clean[len(vol):]) {
			if foldPath(c) == ".git" {
				return fmt.Errorf("%w: .git is read-only here", ErrReadOnly)
			}
		}
		return nil
	}
	l.annotate = gitAnnotate
	return l
}

// gitAnnotate sets Entry.Git from `git status`. Failures leave entries
// unmarked: status is decoration, never a reason to fail a listing.
func gitAnnotate(ctx context.Context, dir string, entries []Entry) {
	status, ok := listingStatus.get(ctx, dir)
	if !ok {
		return
	}
	for i := range entries {
		if s, ok := status[entries[i].Name]; ok {
			entries[i].Git = s
		}
	}
}

// listingStatus serves directory listings.
var listingStatus = newGitStatusCache(gitImmediateStatus, time.Second)

// gitImmediateStatus is one status limited to dir's subtree, folded to dir's
// immediate children.
func gitImmediateStatus(ctx context.Context, dir string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, prefix, err := git.GetStatusUnder(ctx, dir)
	if err != nil {
		return nil, err
	}
	return immediateGitStatus(result.Files, prefix), nil
}

// gitStatusCache shares one status computation per directory: concurrent
// listings of a directory wait for the same run (single flight) and listings
// within ttl of a finished run reuse its result, so a burst of listings costs one
// git status. A change shows up in a listing at most ttl late.
type gitStatusCache struct {
	fn  func(ctx context.Context, dir string) (map[string]string, error)
	ttl time.Duration

	mu      sync.Mutex
	entries map[string]*gitStatusEntry
}

type gitStatusEntry struct {
	done   chan struct{} // closed when status/err are set
	status map[string]string
	err    error
	until  time.Time // valid until; set when done
}

func newGitStatusCache(fn func(context.Context, string) (map[string]string, error), ttl time.Duration) *gitStatusCache {
	return &gitStatusCache{fn: fn, ttl: ttl, entries: map[string]*gitStatusEntry{}}
}

// get returns dir's immediate-child statuses; ok is false when status is unavailable.
// The returned map is shared and must not be modified.
func (c *gitStatusCache) get(ctx context.Context, dir string) (map[string]string, bool) {
	c.mu.Lock()
	e := c.entries[dir]
	if e != nil {
		select {
		case <-e.done:
			if !time.Now().Before(e.until) {
				e = nil
			}
		default:
		}
	}
	if e == nil {
		e = &gitStatusEntry{done: make(chan struct{})}
		c.prune()
		c.entries[dir] = e
		// The run is shared, so it must outlive the first caller's cancellation.
		go func() {
			e.status, e.err = c.fn(context.WithoutCancel(ctx), dir)
			e.until = time.Now().Add(c.ttl)
			close(e.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-e.done:
		return e.status, e.err == nil
	case <-ctx.Done():
		return nil, false
	}
}

// prune drops finished, expired entries; the caller holds c.mu.
func (c *gitStatusCache) prune() {
	if len(c.entries) < 64 {
		return
	}
	now := time.Now()
	for dir, e := range c.entries {
		select {
		case <-e.done:
			if !now.Before(e.until) {
				delete(c.entries, dir)
			}
		default:
		}
	}
}

// immediateGitStatus maps repository-relative paths to immediate children. A
// directory carries its first changed child's two-letter porcelain status.
func immediateGitStatus(files []git.FileStatus, prefix string) map[string]string {
	res := map[string]string{}
	for _, file := range files {
		path := strings.TrimSuffix(file.Path, "/")
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		name := strings.TrimPrefix(path, prefix)
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[:i]
		}
		if name != "" {
			if _, seen := res[name]; !seen {
				res[name] = strings.ReplaceAll(file.Index+file.Worktree, ".", " ")
			}
		}
	}
	return res
}

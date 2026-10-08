package git

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// WorktreeInfo is one entry of `git worktree list --porcelain`.
type WorktreeInfo struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch"` // short name; empty when detached
	Detached bool   `json:"detached"`
	Bare     bool   `json:"bare"`
	Locked   bool   `json:"locked"`
	Prunable bool   `json:"prunable"`
}

// WorktreeList lists worktrees of the repository containing dir; the first is the main one.
func WorktreeList(ctx context.Context, dir string) ([]WorktreeInfo, error) {
	out, err := runDefault(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return ParseWorktreeList(string(out)), nil
}

// ParseWorktreeList parses `git worktree list --porcelain` output.
func ParseWorktreeList(s string) []WorktreeInfo {
	res := []WorktreeInfo{}
	var cur *WorktreeInfo
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if ln == "" {
			if cur != nil {
				res = append(res, *cur)
				cur = nil
			}
			continue
		}
		k, v, _ := strings.Cut(ln, " ")
		if k == "worktree" {
			if cur != nil {
				res = append(res, *cur)
			}
			cur = &WorktreeInfo{Path: v}
			continue
		}
		if cur == nil {
			continue
		}
		switch k {
		case "HEAD":
			cur.Head = v
		case "branch":
			cur.Branch = strings.TrimPrefix(v, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "locked":
			cur.Locked = true
		case "prunable":
			cur.Prunable = true
		}
	}
	if cur != nil {
		res = append(res, *cur)
	}
	return res
}

// WorktreeAdd creates a worktree at path on a NEW branch newBranch started from base
// (empty = HEAD). The caller is responsible for choosing a safe path.
func WorktreeAdd(ctx context.Context, repo, path, newBranch, base string) error {
	if err := ValidBranchName(ctx, newBranch); err != nil {
		return err
	}
	if path == "" || strings.HasPrefix(path, "-") {
		return fmt.Errorf("git worktree add: invalid path %q", path)
	}
	args := []string{"worktree", "add", "--no-track", "-b", newBranch, "--", path}
	if base != "" {
		if !ValidRev(base) {
			return fmt.Errorf("%w: %q", ErrInvalidRef, base)
		}
		args = append(args, base)
	}
	_, err := runDefault(ctx, repo, args...)
	return err
}

// WorktreeRemove removes a linked worktree. Without force git refuses dirty or locked ones.
func WorktreeRemove(ctx context.Context, repo, path string, force bool) error {
	if path == "" || strings.HasPrefix(path, "-") {
		return fmt.Errorf("git worktree remove: invalid path %q", path)
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := runDefault(ctx, repo, append(args, "--", path)...)
	return err
}

// WorktreePrune removes administrative records of worktrees whose directory is gone.
func WorktreePrune(ctx context.Context, repo string) error {
	_, err := runDefault(ctx, repo, "worktree", "prune")
	return err
}

// ValidCloneURL accepts https, ssh, git and file URLs, scp-like host:path and local paths;
// it rejects option-looking values and the ext:: / fd:: transports.
func ValidCloneURL(s string) error {
	if s == "" || strings.HasPrefix(s, "-") || strings.ContainsAny(s, "\x00\r\n") {
		return fmt.Errorf("invalid clone url")
	}
	if strings.Contains(s, "::") {
		return fmt.Errorf("clone url transport not allowed")
	}
	if i := strings.Index(s, "://"); i > 0 {
		u, err := url.Parse(s)
		if err != nil {
			return fmt.Errorf("invalid clone url: %w", err)
		}
		switch u.Scheme {
		case "https", "ssh", "git", "file":
		default:
			return fmt.Errorf("clone url scheme %q not allowed", u.Scheme)
		}
	}
	return nil
}

// Clone clones url into dest (which must not exist or be empty).
func Clone(ctx context.Context, rawURL, dest string) error {
	if err := ValidCloneURL(rawURL); err != nil {
		return err
	}
	if dest == "" || strings.HasPrefix(dest, "-") {
		return fmt.Errorf("git clone: invalid destination %q", dest)
	}
	_, _, err := run(ctx, CloneTimeout, "", "clone", "--", rawURL, dest)
	return err
}

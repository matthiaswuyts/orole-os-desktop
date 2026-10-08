package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrDetachedHead is returned by CurrentBranch when HEAD is not on a branch.
var ErrDetachedHead = errors.New("git: HEAD is detached (not on a branch)")

// ErrNoIdentity is returned by CommitAll when user.name / user.email are not configured.
var ErrNoIdentity = errors.New("git: no commit identity configured; set it with `git config --global user.name \"Your Name\"` and `git config --global user.email you@example.com`")

// commitTimeout leaves room for commit signing and credential-free local hooks-less commits.
const commitTimeout = 2 * time.Minute

// CurrentBranch returns the short name of the branch checked out in dir.
func CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, err := runDefault(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && strings.TrimSpace(ge.Stderr) == "" {
			return "", ErrDetachedHead
		}
		return "", err
	}
	b := strings.TrimSpace(string(out))
	if b == "" {
		return "", ErrDetachedHead
	}
	return b, nil
}

// HasChanges reports whether the work tree containing dir has staged, unstaged or untracked
// (non-ignored) changes.
func HasChanges(ctx context.Context, dir string) (bool, error) {
	out, err := runDefault(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	return len(out) > 0, nil
}

// Identity is the author git would record for a commit.
type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func configValue(ctx context.Context, dir, key string) (string, error) {
	out, err := runDefault(ctx, dir, "config", "--get", key)
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && strings.TrimSpace(ge.Stderr) == "" {
			return "", nil // exit 1 without a message: key not set
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ConfiguredIdentity returns the user.name / user.email git would commit with in dir. The
// environment's GIT_AUTHOR_* variables are intentionally not consulted: git runs here with a
// sanitized environment, so only the user's git configuration counts. ErrNoIdentity is
// returned when either value is empty.
func ConfiguredIdentity(ctx context.Context, dir string) (Identity, error) {
	name, err := configValue(ctx, dir, "user.name")
	if err != nil {
		return Identity{}, err
	}
	email, err := configValue(ctx, dir, "user.email")
	if err != nil {
		return Identity{}, err
	}
	if name == "" || email == "" {
		return Identity{}, ErrNoIdentity
	}
	return Identity{Name: name, Email: email}, nil
}

// CommitAll stages every change in the work tree containing dir (git add --all) and commits
// it with the user's configured identity, returning the new commit id. It returns "" and no
// error when there is nothing to commit. The message goes through stdin, never an argument.
// Git runs through this package's wrapper, so repository hooks do not run (core.hooksPath is
// disabled) and verification is never bypassed with --no-verify.
func CommitAll(ctx context.Context, dir, message string) (string, error) {
	if strings.TrimSpace(message) == "" || strings.ContainsRune(message, 0) {
		return "", errors.New("git: a commit message without NUL bytes is required")
	}
	if _, err := ConfiguredIdentity(ctx, dir); err != nil {
		return "", err
	}
	changed, err := HasChanges(ctx, dir)
	if err != nil || !changed {
		return "", err
	}
	if _, err := runDefault(ctx, dir, "add", "--all"); err != nil {
		return "", err
	}
	staged, err := runDefault(ctx, dir, "diff", "--cached", "--name-only", "-z")
	if err != nil || len(staged) == 0 {
		return "", err
	}
	if _, _, err := runInput(ctx, commitTimeout, dir, nil, strings.NewReader(message), "commit", "--quiet", "-F", "-"); err != nil {
		return "", err
	}
	return ResolveCommit(ctx, dir, "HEAD")
}

// ConfiguredRemoteURL returns the URL configured for remote (credentials stripped) exactly as
// written in the repository configuration, before url.<base>.insteadOf rewriting; "" if the
// remote does not exist.
func ConfiguredRemoteURL(ctx context.Context, dir, remote string) (string, error) {
	if !ValidRef(remote) {
		return "", fmt.Errorf("%w: %q", ErrInvalidRef, remote)
	}
	u, err := configValue(ctx, dir, "remote."+remote+".url")
	return stripCredentials(u), err
}

// DefaultRemoteBranch returns the branch remote's HEAD points to according to the local
// refs/remotes/<remote>/HEAD ("" when it was never recorded, e.g. repositories created with
// `git init` + `git remote add`).
func DefaultRemoteBranch(ctx context.Context, dir, remote string) string {
	if !ValidRef(remote) {
		return ""
	}
	out, err := runDefault(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+remote+"/HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), remote+"/")
}

// BranchesAt lists branches whose tip is exactly rev: remote-tracking branches of remote first
// (with the remote prefix removed), then local branches. Names are unique and sorted by git.
func BranchesAt(ctx context.Context, dir, rev, remote string) ([]string, error) {
	if !ValidRev(rev) || !ValidRef(remote) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRef, rev)
	}
	out, err := runDefault(ctx, dir, "for-each-ref", "--points-at="+rev, "--format=%(refname)", "refs/remotes/"+remote, "refs/heads")
	if err != nil {
		return nil, err
	}
	var remotes, locals []string
	seen := map[string]bool{}
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "refs/remotes/"+remote+"/"):
			n := strings.TrimPrefix(ln, "refs/remotes/"+remote+"/")
			if n != "HEAD" && !seen[n] {
				seen[n] = true
				remotes = append(remotes, n)
			}
		case strings.HasPrefix(ln, "refs/heads/"):
			n := strings.TrimPrefix(ln, "refs/heads/")
			if !seen[n] {
				seen[n] = true
				locals = append(locals, n)
			}
		}
	}
	return append(remotes, locals...), nil
}

// Push pushes the local branch to the same-named branch on remote and records it as the
// upstream. It never forces: a rejected non-fast-forward is returned as an error. Credentials
// come from the user's git configuration (credential helpers, ssh agent); nothing is prompted.
func Push(ctx context.Context, dir, remote, branch string) error {
	if !ValidRef(remote) || remote == "HEAD" {
		return fmt.Errorf("%w: remote %q", ErrInvalidRef, remote)
	}
	if err := ValidBranchName(ctx, branch); err != nil {
		return err
	}
	ref := "refs/heads/" + branch
	_, _, err := run(ctx, CloneTimeout, dir, "push", "--porcelain", "--set-upstream", "--recurse-submodules=no", "--", remote, ref+":"+ref)
	return err
}

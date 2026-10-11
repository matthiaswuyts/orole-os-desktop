package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func identifiedRepo(t *testing.T) string {
	t.Helper()
	dir := NewRepo(t)
	gitIn(t, dir, "config", "user.name", "Publish Test")
	gitIn(t, dir, "config", "user.email", "publish@example.test")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func TestCurrentBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := NewRepo(t)
	if b, err := git.CurrentBranch(ctx, dir); err != nil || b != "main" {
		t.Fatalf("branch = %q, %v", b, err)
	}
	gitIn(t, dir, "checkout", "--detach")
	if _, err := git.CurrentBranch(ctx, dir); !errors.Is(err, git.ErrDetachedHead) {
		t.Fatalf("detached: %v", err)
	}
	if _, err := git.CurrentBranch(ctx, t.TempDir()); err == nil || errors.Is(err, git.ErrDetachedHead) {
		t.Fatalf("non-repository must be a plain error: %v", err)
	}
}

func TestCommitAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := identifiedRepo(t)

	if id, err := git.CommitAll(ctx, dir, "nothing"); err != nil || id != "" {
		t.Fatalf("clean tree: %q %v", id, err)
	}
	if _, err := git.CommitAll(ctx, dir, "  "); err == nil {
		t.Fatal("blank message accepted")
	}

	before := gitIn(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := "-- looks like an option\n\nbody line"
	id, err := git.CommitAll(ctx, dir, msg)
	if err != nil || id == "" || id == before {
		t.Fatalf("commit: %q %v", id, err)
	}
	if got := gitIn(t, dir, "rev-parse", "HEAD"); got != id {
		t.Fatalf("HEAD %s != returned %s", got, id)
	}
	if got := gitIn(t, dir, "log", "-1", "--format=%B"); got != msg {
		t.Fatalf("message = %q", got)
	}
	if got := gitIn(t, dir, "log", "-1", "--format=%an <%ae>"); got != "Publish Test <publish@example.test>" {
		t.Fatalf("author = %q", got)
	}
	if got := gitIn(t, dir, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(got, "a.txt") || !strings.Contains(got, "new.txt") {
		t.Fatalf("commit files = %q", got)
	}
	if changed, err := git.HasChanges(ctx, dir); err != nil || changed {
		t.Fatalf("tree still dirty after commit: %v %v", changed, err)
	}
}

func TestCommitAllRequiresIdentity(t *testing.T) {
	ctx := context.Background()
	dir := NewRepo(t) // no identity in the repository configuration
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	if _, err := git.ConfiguredIdentity(ctx, dir); err == nil {
		t.Skip("a system-level git configuration provides an identity")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := gitIn(t, dir, "rev-parse", "HEAD")
	if _, err := git.CommitAll(ctx, dir, "msg"); !errors.Is(err, git.ErrNoIdentity) {
		t.Fatalf("want ErrNoIdentity, got %v", err)
	}
	if got := gitIn(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatal("a commit was made without an identity")
	}
	if gitIn(t, dir, "diff", "--cached", "--name-only") != "" {
		t.Fatal("files were staged although the identity check failed")
	}
}

func TestPushRemoteHelpers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := identifiedRepo(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	gitIn(t, dir, "init", "--bare", "-b", "main", bare)

	if u, err := git.ConfiguredRemoteURL(ctx, dir, "origin"); err != nil || u != "" {
		t.Fatalf("no remote: %q %v", u, err)
	}
	// The configured URL is reported as written, before insteadOf rewriting.
	gitIn(t, dir, "remote", "add", "origin", "https://user:secret@github.com/owner/repo.git")
	gitIn(t, dir, "config", "url."+bare+".insteadOf", "https://user:secret@github.com/owner/repo.git")
	if u, err := git.ConfiguredRemoteURL(ctx, dir, "origin"); err != nil || u != "https://github.com/owner/repo.git" {
		t.Fatalf("configured url = %q %v", u, err)
	}
	if err := git.Push(ctx, dir, "-bad", "main"); err == nil {
		t.Fatal("option-like remote accepted")
	}
	if err := git.Push(ctx, dir, "origin", "--force"); err == nil {
		t.Fatal("option-like branch accepted")
	}

	if err := git.Push(ctx, dir, "origin", "main"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "remote", "set-head", "origin", "main")
	if b := git.DefaultRemoteBranch(ctx, dir, "origin"); b != "main" {
		t.Fatalf("default branch = %q", b)
	}

	gitIn(t, dir, "checkout", "-b", "orole/feature")
	if got, err := git.BranchesAt(ctx, dir, "HEAD", "origin"); err != nil || !slices.Equal(got, []string{"main", "orole/feature"}) {
		t.Fatalf("branches at HEAD = %v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("f\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := git.CommitAll(ctx, dir, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Push(ctx, dir, "origin", "orole/feature"); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, bare, "rev-parse", "refs/heads/orole/feature"); got != id {
		t.Fatalf("remote tip %s != %s", got, id)
	}
	if got := gitIn(t, dir, "rev-parse", "--abbrev-ref", "orole/feature@{upstream}"); got != "origin/orole/feature" {
		t.Fatalf("upstream = %q", got)
	}
	if err := git.Push(ctx, dir, "origin", "orole/feature"); err != nil {
		t.Fatalf("pushing an up-to-date branch: %v", err)
	}

	// Rewritten history is rejected, never forced.
	gitIn(t, dir, "commit", "--amend", "-m", "rewritten")
	if err := git.Push(ctx, dir, "origin", "orole/feature"); err == nil {
		t.Fatal("non-fast-forward push was accepted")
	}
	if got := gitIn(t, bare, "rev-parse", "refs/heads/orole/feature"); got != id {
		t.Fatal("remote branch was rewritten")
	}
}

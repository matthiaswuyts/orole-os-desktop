package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

func publicationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + filepath.Join(dir, "absent-hooks")}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func publicationWrite(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func publicationFixture(t *testing.T) string {
	t.Helper()
	dir := NewRepo(t)
	publicationGit(t, dir, "checkout", "-b", "orole/issue-54")
	publicationWrite(t, dir, "a.txt", "one\nreviewed change\nthree\n")
	publicationGit(t, dir, "add", "a.txt")
	publicationGit(t, dir, "commit", "-m", "Reviewed issue change")
	return dir
}

func TestPublicationSnapshotContainsRealCommitAndDiff(t *testing.T) {
	dir := publicationFixture(t)
	p, err := git.PublicationSnapshot(context.Background(), dir, "orole/issue-54", "main")
	if err != nil {
		t.Fatal(err)
	}
	if p.Head != publicationGit(t, dir, "rev-parse", "HEAD") || p.BaseHead != publicationGit(t, dir, "rev-parse", "refs/heads/main") {
		t.Fatalf("wrong snapshot commits: %+v", p)
	}
	if !strings.Contains(p.Diff, "+reviewed change") || !strings.Contains(p.Diff, "-two") {
		t.Fatalf("missing actual diff: %s", p.Diff)
	}
	// Another commit changes the exact snapshot, even if a caller retains its old approval.
	publicationWrite(t, dir, "a.txt", "one\nnew unreviewed change\nthree\n")
	publicationGit(t, dir, "add", "a.txt")
	publicationGit(t, dir, "commit", "-m", "Changed after review")
	changed, err := git.PublicationSnapshot(context.Background(), dir, "orole/issue-54", "main")
	if err != nil || changed.Head == p.Head || changed.Diff == p.Diff {
		t.Fatalf("changed snapshot: %+v %v", changed, err)
	}
	if err := git.PushPublication(context.Background(), dir, "owner/repo", "orole/issue-54", p.Head); err == nil {
		t.Fatal("stale SHA accepted for push")
	}
}

func TestPublicationSnapshotRejectsUnreviewableWorktrees(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string)
		branch string
		base   string
	}{
		{"unstaged", func(t *testing.T, dir string) { publicationWrite(t, dir, "a.txt", "dirty\n") }, "orole/issue-54", "main"},
		{"staged", func(t *testing.T, dir string) {
			publicationWrite(t, dir, "a.txt", "staged\n")
			publicationGit(t, dir, "add", "a.txt")
		}, "orole/issue-54", "main"},
		{"untracked", func(t *testing.T, dir string) { publicationWrite(t, dir, "untracked.txt", "new\n") }, "orole/issue-54", "main"},
		{"detached", func(t *testing.T, dir string) { publicationGit(t, dir, "checkout", "--detach") }, "orole/issue-54", "main"},
		{"branch mismatch", func(*testing.T, string) {}, "different", "main"},
		{"missing base", func(*testing.T, string) {}, "orole/issue-54", "absent"},
		{"revision expression base", func(*testing.T, string) {}, "orole/issue-54", "main~1"},
		{"empty diff", func(*testing.T, string) {}, "orole/issue-54", "orole/issue-54"},
		{"assume unchanged", func(t *testing.T, dir string) { publicationGit(t, dir, "update-index", "--assume-unchanged", "a.txt") }, "orole/issue-54", "main"},
		{"skip worktree", func(t *testing.T, dir string) { publicationGit(t, dir, "update-index", "--skip-worktree", "a.txt") }, "orole/issue-54", "main"},
		{"diverged base", func(t *testing.T, dir string) {
			publicationGit(t, dir, "checkout", "main")
			publicationWrite(t, dir, "base.txt", "base advanced independently\n")
			publicationGit(t, dir, "add", "base.txt")
			publicationGit(t, dir, "commit", "-m", "Base diverged")
			publicationGit(t, dir, "checkout", "orole/issue-54")
		}, "orole/issue-54", "main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := publicationFixture(t)
			tc.mutate(t, dir)
			if _, err := git.PublicationSnapshot(context.Background(), dir, tc.branch, tc.base); err == nil {
				t.Fatal("unreviewable worktree accepted")
			}
		})
	}
}

func TestPublicationSnapshotUsesOwnedLinkedWorktree(t *testing.T) {
	dir := NewRepo(t)
	worktree := filepath.Join(t.TempDir(), "owned")
	publicationGit(t, dir, "worktree", "add", "-b", "orole/issue-54", worktree, "main")
	publicationWrite(t, worktree, "a.txt", "owned worktree change\n")
	publicationGit(t, worktree, "add", "a.txt")
	publicationGit(t, worktree, "commit", "-m", "Owned change")
	p, err := git.PublicationSnapshot(context.Background(), worktree, "orole/issue-54", "main")
	if err != nil || !strings.Contains(p.Diff, "+owned worktree change") {
		t.Fatalf("linked worktree: %+v %v", p, err)
	}
}

func TestPublicationSnapshotIgnoresExecutableConfigAndReplaceRefs(t *testing.T) {
	dir := publicationFixture(t)
	publicationWrite(t, dir, ".gitattributes", "a.txt filter=evil diff=evil\n")
	publicationGit(t, dir, "add", ".gitattributes")
	publicationGit(t, dir, "commit", "-m", "Attributes")
	for _, key := range []string{"core.fsmonitor", "diff.external", "diff.evil.command", "diff.evil.textconv", "filter.evil.clean", "filter.evil.process", "credential.helper"} {
		publicationGit(t, dir, "config", key, "orole-command-must-never-execute")
	}
	publicationGit(t, dir, "config", "url.ext::orole-command-must-never-execute.insteadOf", "https://github.com/")
	head := publicationGit(t, dir, "rev-parse", "HEAD")
	base := publicationGit(t, dir, "rev-parse", "main")
	publicationGit(t, dir, "replace", head, base)
	p, err := git.PublicationSnapshot(context.Background(), dir, "orole/issue-54", "main")
	if err != nil || p.Head != head || !strings.Contains(p.Diff, "+reviewed change") {
		t.Fatalf("configuration or replacement affected review: %+v %v", p, err)
	}
}

func TestPublicationSnapshotRejectsTruncatedDiff(t *testing.T) {
	dir := publicationFixture(t)
	publicationWrite(t, dir, "large.txt", strings.Repeat("reviewed change\n", (17<<20)/16))
	publicationGit(t, dir, "add", "large.txt")
	publicationGit(t, dir, "commit", "-m", "Large review")
	if _, err := git.PublicationSnapshot(context.Background(), dir, "orole/issue-54", "main"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected bounded output failure: %v", err)
	}
}

func TestPushPublicationRejectsUnsafeTargetsAndChangedWorktreeLocally(t *testing.T) {
	dir := publicationFixture(t)
	head := publicationGit(t, dir, "rev-parse", "HEAD")
	for _, repo := range []string{"https://github.com/o/r", "git@github.com:o/r", "../r", "o/..", "o/r/extra", "o/r?x", "o/r#fragment", "o/r%2f", "o/r\n", ""} {
		if err := git.PushPublication(context.Background(), dir, repo, "orole/issue-54", head); err == nil {
			t.Errorf("unsafe repo accepted: %q", repo)
		}
	}
	for _, invalid := range []string{"HEAD", head[:12], "+" + head, head + ":refs/heads/main"} {
		if err := git.PushPublication(context.Background(), dir, "owner/repo", "orole/issue-54", invalid); err == nil {
			t.Errorf("non-exact SHA accepted: %q", invalid)
		}
	}
	if err := git.PushPublication(context.Background(), dir, "owner/repo", "main", head); err == nil {
		t.Fatal("wrong checked-out branch accepted")
	}
	publicationWrite(t, dir, "a.txt", "uncommitted after approval\n")
	if err := git.PushPublication(context.Background(), dir, "owner/repo", "orole/issue-54", head); err == nil {
		t.Fatal("dirty worktree accepted")
	}
}

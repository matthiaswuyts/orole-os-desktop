package git_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

func reviewAttack(t *testing.T, dir, filter string) string {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "executed")
	command := "echo executed > '" + strings.ReplaceAll(filepath.ToSlash(marker), "'", "'\\''") + "'; exit 1"
	for key, value := range map[string]string{
		"core.fsmonitor": command, "filter.hostile." + filter: command,
		"filter.hostile.required": "true", "diff.hostile.textconv": command,
		"diff.hostile.command": command, "diff.external": command,
	} {
		publicationGit(t, dir, "config", key, value)
	}
	return marker
}

func assertNoReviewAttack(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repository command executed or marker unreadable: %v", err)
	}
}

func TestDiffAndStatusDoNotExecuteRepositoryFilters(t *testing.T) {
	t.Parallel()
	for _, filter := range []string{"clean", "process"} {
		t.Run(filter, func(t *testing.T) {
			t.Parallel()
			dir := NewRepo(t)
			publicationWrite(t, dir, ".gitattributes", "*.txt filter=hostile diff=hostile\n*.bin filter=hostile\n")
			publicationWrite(t, dir, "tracked.bin", "old\x00binary")
			publicationGit(t, dir, "add", ".")
			publicationGit(t, dir, "commit", "-m", "attributes and binary")
			publicationGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
			publicationGit(t, dir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
			publicationGit(t, dir, "config", "branch.main.remote", "origin")
			publicationGit(t, dir, "config", "branch.main.merge", "refs/heads/main")
			publicationWrite(t, dir, "a.txt", "one\nchanged\nthree\n")
			publicationWrite(t, dir, "tracked.bin", "new\x00binary")
			publicationWrite(t, dir, "untracked.txt", "untracked\n")
			marker := reviewAttack(t, dir, filter)
			diff, err := git.Diff(context.Background(), dir, git.DiffOptions{})
			assertNoReviewAttack(t, marker)
			if err != nil || len(diff.Files) != 2 || diff.Additions != 1 || diff.Deletions != 1 {
				t.Fatalf("diff lost changed content: %+v %v", diff, err)
			}
			if diff.Files[0].Path != "a.txt" || diff.Files[0].Hunks[0].Lines[2].Text != "changed" || !diff.Files[1].Binary {
				t.Fatalf("diff changed text/binary semantics: %+v", diff)
			}
			status, err := git.GetStatus(context.Background(), dir)
			assertNoReviewAttack(t, marker)
			if err != nil || status.Unstaged != 2 || status.Untracked != 1 || status.Dirty != 3 || status.Branch.Head != "main" || status.Branch.Upstream != "origin/main" {
				t.Fatalf("status lost changes or upstream: %+v %v", status, err)
			}
		})
	}
}

func TestDiffAndStatusReviewDirtySubmodulesWithoutExecutingFilters(t *testing.T) {
	t.Parallel()
	dir := NewRepo(t)
	child := NewRepo(t)
	publicationWrite(t, child, ".gitattributes", "*.txt filter=hostile\n")
	publicationGit(t, child, "add", ".")
	publicationGit(t, child, "commit", "-m", "attributes")
	publicationGit(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", child, "nested")
	publicationGit(t, dir, "commit", "-am", "submodule")
	nested := filepath.Join(dir, "nested")
	publicationWrite(t, nested, "a.txt", "dirty nested file\n")
	marker := reviewAttack(t, nested, "process")
	status, err := git.GetStatus(context.Background(), dir)
	assertNoReviewAttack(t, marker)
	if err != nil || status.Dirty != 1 || status.Files[0].Path != "nested" || status.Files[0].Worktree != "M" {
		t.Fatalf("dirty submodule hidden: %+v %v", status, err)
	}
	diff, err := git.Diff(context.Background(), dir, git.DiffOptions{})
	assertNoReviewAttack(t, marker)
	if err != nil || len(diff.Files) != 1 || diff.Files[0].Path != "nested" || !strings.HasSuffix(diff.Files[0].Hunks[0].Lines[1].Text, "-dirty") {
		t.Fatalf("dirty submodule diff hidden: %+v %v", diff, err)
	}
}

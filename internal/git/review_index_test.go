package git_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

// Review reads a copy of the index: `git diff` rewrites the index it reads, and
// DiffWorktree stages every file into it. The source must stay byte-identical.
func TestReviewNeverMutatesSourceIndex(t *testing.T) {
	ctx := context.Background()
	dir := NewRepo(t)
	file := filepath.Join(dir, "a.txt")
	// Same content, newer mtime: the stat cache is stale, so git wants to refresh.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	publicationWrite(t, dir, "new.txt", "untracked\n")
	index := filepath.Join(dir, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.Diff(ctx, dir, git.DiffOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := git.GetStatus(ctx, dir); err != nil {
		t.Fatal(err)
	}
	d, err := git.DiffWorktree(ctx, dir, "HEAD")
	if err != nil || len(d.Files) != 1 || d.Files[0].Path != "new.txt" {
		t.Fatalf("DiffWorktree = %+v, %v", d, err)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("source index changed (err %v)", err)
	}
}

// Ref-to-ref diffs never read the index, so an unreadable one must not matter;
// worktree diffs and status do read it and must say so.
func TestRefDiffSkipsIndex(t *testing.T) {
	ctx := context.Background()
	dir := NewRepo(t)
	publicationWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	publicationGit(t, dir, "commit", "-am", "second")
	if err := os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("not an index"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := git.Diff(ctx, dir, git.DiffOptions{From: "HEAD~1", To: "HEAD"})
	if err != nil || len(d.Files) != 1 || d.Files[0].Path != "a.txt" || d.Additions != 1 || d.Deletions != 1 {
		t.Fatalf("ref diff = %+v, %v", d, err)
	}
	if _, err := git.GetStatus(ctx, dir); err == nil {
		t.Fatal("status of a corrupt index should fail, not report a clean tree")
	}
}

func TestStatusReadsSplitAndSpecialIndexEntries(t *testing.T) {
	ctx := context.Background()
	dir := NewRepo(t)
	publicationWrite(t, dir, ".gitignore", "ignored.txt\n")
	publicationGit(t, dir, "add", ".gitignore")
	publicationGit(t, dir, "commit", "-m", "ignore")
	publicationWrite(t, dir, "ignored.txt", "staged although ignored\n")
	publicationGit(t, dir, "add", "-f", "ignored.txt")
	publicationWrite(t, dir, "intent.txt", "intent to add\n")
	publicationGit(t, dir, "add", "-N", "intent.txt")
	publicationWrite(t, dir, "a.txt", "one\nchanged\nthree\n")
	publicationGit(t, dir, "update-index", "--split-index")
	publicationWrite(t, dir, "split.txt", "added after the split\n")
	publicationGit(t, dir, "add", "split.txt")
	if shared, _ := filepath.Glob(filepath.Join(dir, ".git", "sharedindex.*")); len(shared) == 0 {
		t.Skip("this git does not write a split index")
	}
	st, err := git.GetStatus(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range st.Files {
		got[f.Path] = f.Index + f.Worktree
	}
	want := map[string]string{"ignored.txt": "A.", "intent.txt": ".A", "a.txt": ".M", "split.txt": "A."}
	if len(got) != len(want) {
		t.Fatalf("status files = %v, want %v", got, want)
	}
	for path, xy := range want {
		if got[path] != xy {
			t.Fatalf("status files = %v, want %v", got, want)
		}
	}
}

func TestReviewOutsideRepository(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.GetStatus(context.Background(), dir); !errors.Is(err, git.ErrNotRepo) {
		t.Fatalf("status outside a repository: %v", err)
	}
	if _, err := git.Diff(context.Background(), dir, git.DiffOptions{}); !errors.Is(err, git.ErrNotRepo) {
		t.Fatalf("diff outside a repository: %v", err)
	}
}

func TestGetStatusUnderLimitsToDirectory(t *testing.T) {
	ctx := context.Background()
	dir := NewRepo(t)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	publicationWrite(t, dir, "sub/in.txt", "tracked\n")
	publicationGit(t, dir, "add", "sub/in.txt")
	publicationGit(t, dir, "commit", "-m", "sub")
	publicationWrite(t, dir, "sub/in.txt", "changed\n")
	publicationWrite(t, dir, "sub/new.txt", "untracked\n")
	publicationWrite(t, dir, "a.txt", "one\nchanged\nthree\n")
	publicationWrite(t, dir, "elsewhere.txt", "untracked\n")

	st, prefix, err := git.GetStatusUnder(ctx, filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if prefix != "sub/" {
		t.Fatalf("prefix = %q, want sub/", prefix)
	}
	got := map[string]string{}
	for _, f := range st.Files {
		got[f.Path] = f.Index + f.Worktree
	}
	want := map[string]string{"sub/in.txt": ".M", "sub/new.txt": "??"}
	if len(got) != len(want) || got["sub/in.txt"] != ".M" || got["sub/new.txt"] != "??" {
		t.Fatalf("scoped status = %v, want %v", got, want)
	}
	_, prefix, err = git.GetStatusUnder(ctx, dir)
	if err != nil || prefix != "" {
		t.Fatalf("root prefix = %q, %v", prefix, err)
	}
}

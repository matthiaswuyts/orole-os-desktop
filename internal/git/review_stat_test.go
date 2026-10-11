package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

// DiffStatWorktree must count exactly what DiffWorktree reports, for committed work, edits,
// untracked files, renames and binary files, and without running repository commands.
func TestDiffStatWorktreeMatchesDiffWorktree(t *testing.T) {
	ctx := context.Background()
	dir := NewRepo(t)
	publicationWrite(t, dir, "old-name.txt", "keep\nkeep\nkeep\nkeep\n")
	publicationWrite(t, dir, "bin.dat", "old\x00binary")
	publicationGit(t, dir, "add", ".")
	publicationGit(t, dir, "commit", "-m", "base files")
	base := publicationGit(t, dir, "rev-parse", "HEAD")
	base = base[:len(base)-1]

	steps := []struct {
		name string
		do   func()
	}{
		{"clean", func() {}},
		{"edit", func() { publicationWrite(t, dir, "a.txt", "one\nchanged\nthree\nfour\n") }},
		{"untracked", func() { publicationWrite(t, dir, "new.txt", "x\ny\nz\n") }},
		{"binary", func() { publicationWrite(t, dir, "bin.dat", "new\x00binary") }},
		{"rename", func() {
			if err := os.Rename(filepath.Join(dir, "old-name.txt"), filepath.Join(dir, "renamed.txt")); err != nil {
				t.Fatal(err)
			}
		}},
		{"committed on top of base", func() {
			publicationGit(t, dir, "add", ".")
			publicationGit(t, dir, "commit", "-m", "work")
			publicationWrite(t, dir, "more.txt", "more\n")
		}},
	}
	for _, step := range steps {
		step.do()
		diff, err := git.DiffWorktree(ctx, dir, base)
		if err != nil {
			t.Fatalf("%s: diff: %v", step.name, err)
		}
		files, additions, deletions, err := git.DiffStatWorktree(ctx, dir, base)
		if err != nil {
			t.Fatalf("%s: stat: %v", step.name, err)
		}
		if files != len(diff.Files) || additions != diff.Additions || deletions != diff.Deletions {
			t.Fatalf("%s: stat = %d files +%d -%d, diff = %d files +%d -%d", step.name, files, additions, deletions, len(diff.Files), diff.Additions, diff.Deletions)
		}
	}
	if files, _, _, _ := git.DiffStatWorktree(ctx, dir, base); files == 0 {
		t.Fatal("final stat saw no changes")
	}
}

func TestDiffStatWorktreeRejectsBadRef(t *testing.T) {
	if _, _, _, err := git.DiffStatWorktree(context.Background(), NewRepo(t), "--output=x"); err == nil {
		t.Fatal("option-looking ref accepted")
	}
}

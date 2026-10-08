package agents_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/agents"
)

func reviewWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// Git executes command-valued settings and hooks through its bundled POSIX shell,
// including on Windows. Each command writes outside the reviewed worktree.
func hostileReviewConfig(t *testing.T, dir, filter string) []string {
	t.Helper()
	commands := t.TempDir()
	var markers []string
	command := func(name string) string {
		marker := filepath.Join(commands, name+".marker")
		markers = append(markers, marker)
		return "echo executed > '" + strings.ReplaceAll(filepath.ToSlash(marker), "'", "'\\''") + "'; exit 1"
	}
	gitTest(t, dir, "config", "core.fsmonitor", command("fsmonitor"))
	gitTest(t, dir, "config", "filter.hostile."+filter, command(filter))
	gitTest(t, dir, "config", "filter.hostile.required", "true")
	gitTest(t, dir, "config", "diff.hostile.command", command("diff"))
	gitTest(t, dir, "config", "diff.hostile.textconv", command("textconv"))
	gitTest(t, dir, "config", "diff.external", command("external"))
	gitTest(t, dir, "config", "core.hooksPath", commands)
	if err := os.WriteFile(filepath.Join(commands, "post-index-change"), []byte("#!/bin/sh\n"+command("hook")+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return markers
}

func assertReviewCommandsNotRun(t *testing.T, markers []string) {
	t.Helper()
	for _, marker := range markers {
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Errorf("repository command executed or marker unreadable: %s: %v", marker, err)
		}
	}
}

func TestAgentDiffIsolatesRepositoryCommandsAndPreservesChanges(t *testing.T) {
	for _, filter := range []string{"clean", "process"} {
		t.Run(filter, func(t *testing.T) {
			repo := repoTest(t)
			reviewWrite(t, repo, ".gitattributes", "*.txt filter=hostile diff=hostile\n*.bin filter=hostile\n")
			reviewWrite(t, repo, ".gitignore", "*.ignore\n")
			reviewWrite(t, repo, "tracked.bin", "old\x00binary")
			reviewWrite(t, repo, "removed.txt", "removed\n")
			reviewWrite(t, repo, "old.txt", "rename this unique content\n")
			gitTest(t, repo, "add", ".")
			gitTest(t, repo, "commit", "-m", "review fixtures")
			if filter == "clean" {
				linked := filepath.Join(t.TempDir(), "linked")
				gitTest(t, repo, "worktree", "add", "-b", "review-test", linked)
				repo = linked
			}
			reviewWrite(t, repo, "README.txt", "changed\n")
			reviewWrite(t, repo, "tracked.bin", "new\x00binary")
			reviewWrite(t, repo, "new.txt", "first\nsecond\n")
			reviewWrite(t, repo, "new.bin", "new\x00untracked")
			reviewWrite(t, repo, "staged.ignore", "staged despite ignore\n")
			reviewWrite(t, repo, "hidden.ignore", "must stay ignored\n")
			gitTest(t, repo, "add", "-f", "staged.ignore")
			if err := os.Remove(filepath.Join(repo, "removed.txt")); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(repo, "old.txt"), filepath.Join(repo, "renamed.txt")); err != nil {
				t.Fatal(err)
			}
			if filter == "process" {
				gitTest(t, repo, "update-index", "--split-index")
			}
			indexPath := gitTest(t, repo, "rev-parse", "--path-format=absolute", "--git-path", "index")
			before, err := os.ReadFile(indexPath)
			if err != nil {
				t.Fatal(err)
			}
			markers := hostileReviewConfig(t, repo, filter)
			diff, err := (agents.GitWorktrees{}).Diff(ctx10(t), repo, "HEAD")
			assertReviewCommandsNotRun(t, markers)
			if err != nil {
				t.Fatal(err)
			}
			if diff.Truncated || len(diff.Files) != 7 || diff.Additions != 4 || diff.Deletions != 2 {
				t.Fatalf("incomplete or incorrect review: %+v", diff)
			}
			files := make(map[string]agents.DiffFile)
			for _, file := range diff.Files {
				files[file.Path] = file
			}
			for _, name := range []string{"tracked.bin", "new.bin"} {
				if !files[name].Binary {
					t.Errorf("missing binary review: %s: %+v", name, files[name])
				}
			}
			if files["README.txt"].Additions != 1 || files["README.txt"].Deletions != 1 || files["new.txt"].Additions != 2 || files["staged.ignore"].Additions != 1 || files["removed.txt"].Status != "deleted" || files["renamed.txt"].OldPath != "old.txt" {
				t.Fatalf("text/staged/deletion/rename review changed: %+v", diff)
			}
			after, err := os.ReadFile(indexPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("review changed real index: %v", err)
			}
			content, err := os.ReadFile(filepath.Join(repo, "README.txt"))
			if err != nil || string(content) != "changed\n" {
				t.Fatalf("review changed worktree: %q %v", content, err)
			}
		})
	}
}

func TestAgentAutomaticAndManualDiffDoNotExecuteRepositoryCommands(t *testing.T) {
	repo := repoTest(t)
	reviewWrite(t, repo, ".gitattributes", "*.txt filter=hostile diff=hostile\n")
	gitTest(t, repo, "add", ".gitattributes")
	gitTest(t, repo, "commit", "-m", "attributes")
	markers := hostileReviewConfig(t, repo, "process")
	e := newEnv(t, withWT(agents.GitWorktrees{Root: t.TempDir()}))
	s := e.start(t, agents.StartSpec{Dir: repo, Prompt: "EDIT"})
	e.wait(t, s.ID, "automatic review stats", func(s agents.Session) bool {
		return s.Status == agents.StatusDone && s.Files == 1 && s.Additions == 2
	})
	assertReviewCommandsNotRun(t, markers)
	diff, err := e.m.Diff(ctx10(t), s.ID)
	if err != nil || diff.Error != "" || diff.Files != 1 || diff.Additions != 2 || diff.Diff.Files[0].Path != "edited.txt" {
		t.Fatalf("manual review: %+v %v", diff, err)
	}
	assertReviewCommandsNotRun(t, markers)
}

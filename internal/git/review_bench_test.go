package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

// benchRepo is a repo with a few hundred tracked files, two commits and one dirty
// file: enough for the index/stat cache and the per-call process spawns to show.
func benchRepo(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	g := func(args ...string) {
		b.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, text string) {
		b.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	g("init", "-b", "main")
	for i := 0; i < 400; i++ {
		write(fmt.Sprintf("d%02d/f%03d.txt", i%20, i), "line one\nline two\n")
	}
	g("add", ".")
	g("commit", "-m", "first")
	write("d00/f000.txt", "changed\n")
	g("commit", "-am", "second")
	write("d01/f001.txt", "dirty\n")
	return dir
}

func BenchmarkGetStatus(b *testing.B) {
	dir := benchRepo(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := git.GetStatus(ctx, dir); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiffWorktree(b *testing.B) {
	dir := benchRepo(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := git.Diff(ctx, dir, git.DiffOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiffRefs(b *testing.B) {
	dir := benchRepo(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := git.Diff(ctx, dir, git.DiffOptions{From: "HEAD~1", To: "HEAD"}); err != nil {
			b.Fatal(err)
		}
	}
}

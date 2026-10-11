package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/git"
	"github.com/rayorole/orole-os-desktop/internal/testrepo"
)

// repoTemplate is built once per test binary; NewRepo copies it (see internal/testrepo).
var repoTemplate = testrepo.New(func(dir string) error {
	if err := testrepo.Git(dir, "init", "-b", "main"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		return err
	}
	if err := testrepo.Git(dir, "add", "."); err != nil {
		return err
	}
	return testrepo.Git(dir, "-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "first")
})

func TestMain(m *testing.M) {
	code := m.Run()
	testrepo.CloseAll()
	os.Exit(code)
}

// NewRepo makes a temp repo with one commit on branch main.
func NewRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repoTemplate.Copy(t, dir)
	return dir
}

func TestStatusAndDiff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := NewRepo(t)
	st, err := git.GetStatus(ctx, dir)
	if err != nil || st.Dirty != 0 || st.Branch.Head != "main" {
		t.Fatalf("clean status: %+v %v", st, err)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\nfour\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "sp ace.txt"), []byte("x\n"), 0o644)
	st, err = git.GetStatus(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Unstaged != 1 || st.Untracked != 2 || st.Dirty != 3 {
		t.Fatalf("status counts: %+v", st)
	}
	d, err := git.Diff(ctx, dir, git.DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 1 || d.Files[0].Path != "a.txt" || d.Additions != 2 || d.Deletions != 1 || len(d.Files[0].Hunks) != 1 {
		t.Fatalf("diff: %+v", d)
	}
	h := d.Files[0].Hunks[0]
	if h.OldStart != 1 || h.NewLines != 4 {
		t.Fatalf("hunk: %+v", h)
	}
	if l := h.Lines[2]; l.Kind != "+" || l.Text != "TWO" || l.New != 2 {
		t.Fatalf("line: %+v", l)
	}
	// staged
	exec.Command("git", "-C", dir, "add", "a.txt").Run()
	if d, _ := git.Diff(ctx, dir, git.DiffOptions{}); len(d.Files) != 0 {
		t.Fatalf("unstaged should be empty after add: %+v", d)
	}
	d, err = git.Diff(ctx, dir, git.DiffOptions{Staged: true})
	if err != nil || len(d.Files) != 1 || d.Additions != 2 {
		t.Fatalf("staged: %+v %v", d, err)
	}
	st, _ = git.GetStatus(ctx, dir)
	if st.Staged != 1 {
		t.Fatalf("staged count: %+v", st)
	}
	// between refs
	exec.Command("git", "-C", dir, "-c", "user.name=T", "-c", "user.email=t@e.c", "commit", "-qm", "second").Run()
	d, err = git.Diff(ctx, dir, git.DiffOptions{From: "HEAD~1", To: "HEAD"})
	if err != nil || len(d.Files) != 1 || d.Files[0].Status != "modified" {
		t.Fatalf("range: %+v %v", d, err)
	}
}

func TestParseDiffAddedDeletedRenamedBinary(t *testing.T) {
	text := "diff --git a/old name.txt b/new name.txt\nsimilarity index 90%\nrename from old name.txt\nrename to new name.txt\n" +
		"diff --git a/n.go b/n.go\nnew file mode 100644\n--- /dev/null\n+++ b/n.go\n@@ -0,0 +1,2 @@\n+package n\n+\n" +
		"diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\n--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n\\ No newline at end of file\n" +
		"diff --git a/i.png b/i.png\nBinary files a/i.png and b/i.png differ\n"
	r := git.ParseDiff(text)
	if len(r.Files) != 4 {
		t.Fatalf("files: %+v", r.Files)
	}
	if f := r.Files[0]; f.Status != "renamed" || f.Path != "new name.txt" || f.OldPath != "old name.txt" {
		t.Fatalf("rename: %+v", f)
	}
	if f := r.Files[1]; f.Status != "added" || f.Additions != 2 || f.Hunks[0].Lines[1].Text != "" {
		t.Fatalf("added: %+v", f)
	}
	if f := r.Files[2]; f.Status != "deleted" || f.Deletions != 1 || f.Hunks[0].OldLines != 1 {
		t.Fatalf("deleted: %+v", f)
	}
	if !r.Files[3].Binary {
		t.Fatalf("binary: %+v", r.Files[3])
	}
	if r.Additions != 2 || r.Deletions != 1 {
		t.Fatalf("totals %d %d", r.Additions, r.Deletions)
	}
}

func TestParseStatusRecords(t *testing.T) {
	out := "# branch.oid abc\x00# branch.head feat\x00# branch.upstream origin/feat\x00# branch.ab +2 -3\x00" +
		"1 M. N... 100644 100644 100644 a b staged.go\x001 .M N... 100644 100644 100644 a b work.go\x00" +
		"u UU N... 1 2 3 4 a b c conflict.go\x00? un tracked.txt\x00"
	st, err := git.ParseStatus([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch.Head != "feat" || st.Branch.Ahead != 2 || st.Branch.Behind != 3 || st.Branch.Upstream != "origin/feat" {
		t.Fatalf("branch: %+v", st.Branch)
	}
	if st.Staged != 1 || st.Unstaged != 1 || st.Conflicted != 1 || st.Untracked != 1 || st.Dirty != 4 || st.Files[3].Path != "un tracked.txt" {
		t.Fatalf("counts: %+v", st)
	}
}

func TestRefInjectionRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := NewRepo(t)
	evil := []string{"--upload-pack=calc", "-b", "--output=x", "a..b", "a b", "HEAD@{1}", "feat~x", "a^b", "~1", "", "/abs", "x.lock", "a\nb", "a:b", ".hidden", "a\\b"}
	for _, e := range evil {
		if git.ValidRef(e) {
			t.Errorf("ValidRef(%q) = true", e)
		}
		if err := git.CreateBranch(ctx, dir, e, ""); !errors.Is(err, git.ErrInvalidRef) {
			t.Errorf("CreateBranch(%q) = %v", e, err)
		}
		if _, err := git.Diff(ctx, dir, git.DiffOptions{From: e, To: "HEAD"}); e != "" && !errors.Is(err, git.ErrInvalidRef) {
			t.Errorf("Diff from %q = %v", e, err)
		}
		if _, err := git.Log(ctx, dir, e, 5); e != "" && !errors.Is(err, git.ErrInvalidRef) {
			t.Errorf("Log %q = %v", e, err)
		}
	}
	if err := git.CreateBranch(ctx, dir, "HEAD", ""); !errors.Is(err, git.ErrInvalidRef) {
		t.Errorf("branch named HEAD accepted: %v", err)
	}
	if err := git.CreateBranch(ctx, dir, "good/name-1", "main"); err != nil {
		t.Fatal(err)
	}
	if err := git.WorktreeAdd(ctx, dir, filepath.Join(dir, "..", "wt-evil"), "--detach", ""); !errors.Is(err, git.ErrInvalidRef) {
		t.Errorf("worktree add option branch: %v", err)
	}
	if err := git.WorktreeAdd(ctx, dir, "--force", "ok-branch", ""); err == nil {
		t.Error("option-looking path accepted")
	}
	for _, u := range []string{"--upload-pack=x", "ext::sh -c id", "fd::3", "ftp://x/y"} {
		if err := git.ValidCloneURL(u); err == nil {
			t.Errorf("clone url %q accepted", u)
		}
	}
	for _, u := range []string{"https://github.com/a/b.git", "git@github.com:a/b.git", `C:\code\repo`} {
		if err := git.ValidCloneURL(u); err != nil {
			t.Errorf("clone url %q rejected: %v", u, err)
		}
	}
}

func TestBranchesLogWorktreesClone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := NewRepo(t)
	if err := git.CreateBranch(ctx, dir, "orole/x", "HEAD"); err != nil {
		t.Fatal(err)
	}
	bs, err := git.Branches(ctx, dir)
	if err != nil || len(bs) != 2 {
		t.Fatalf("branches: %+v %v", bs, err)
	}
	cur := 0
	for _, b := range bs {
		if b.Current {
			cur++
			if b.Name != "main" || b.Subject != "first" {
				t.Fatalf("current: %+v", b)
			}
		}
	}
	if cur != 1 {
		t.Fatalf("current count %d", cur)
	}
	log, err := git.Log(ctx, dir, "", 10)
	if err != nil || len(log) != 1 || log[0].Subject != "first" || log[0].Author != "T" || len(log[0].Hash) != 40 {
		t.Fatalf("log: %+v %v", log, err)
	}

	wt := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-wt")
	if err := git.WorktreeAdd(ctx, dir, wt, "orole/feature", "main"); err != nil {
		t.Fatal(err)
	}
	list, err := git.WorktreeList(ctx, dir)
	if err != nil || len(list) != 2 || list[1].Branch != "orole/feature" {
		t.Fatalf("worktrees: %+v %v", list, err)
	}
	if err := git.WorktreeRemove(ctx, dir, wt, false); err != nil {
		t.Fatal(err)
	}
	if list, _ = git.WorktreeList(ctx, dir); len(list) != 1 {
		t.Fatalf("after remove: %+v", list)
	}
	if err := git.WorktreePrune(ctx, dir); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "clone")
	if err := git.Clone(ctx, dir, dest); err != nil {
		t.Fatal(err)
	}
	if got := git.RemoteURL(ctx, dest); !strings.EqualFold(filepath.Clean(got), filepath.Clean(dir)) {
		t.Fatalf("remote url %q want %q", got, dir)
	}
	if git.RemoteURL(ctx, dir) != "" {
		t.Fatal("repo without remote should have empty url")
	}
}

func TestEmptyRepoLogAndNotRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-b", "main").Run(); err != nil {
		t.Fatal(err)
	}
	if l, err := git.Log(ctx, dir, "", 5); err != nil || len(l) != 0 {
		t.Fatalf("empty log: %v %v", l, err)
	}
	if _, err := git.Toplevel(ctx, t.TempDir()); !errors.Is(err, git.ErrNotRepo) {
		t.Fatalf("not repo: %v", err)
	}
}

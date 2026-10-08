package projects_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/projects"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func newService(t *testing.T) *projects.Service {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return projects.New(db)
}

// repo creates <tmp>/<name> with one commit and returns its canonical path.
func repo(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@e.c", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "init")
	r, _ := filepath.EvalSymlinks(dir)
	return r
}

func TestAddListRemove(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	dir := repo(t, tmp, "alpha")

	if _, err := s.Add(ctx, t.TempDir()); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("non-repo add: %v", err)
	}
	if _, err := s.Add(ctx, filepath.Join(tmp, "nope")); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("missing dir add: %v", err)
	}
	if _, err := s.Add(ctx, ""); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("empty add: %v", err)
	}

	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	p, err := s.Add(ctx, sub) // a subdirectory resolves to the repo root
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != dir || p.Name != "alpha" || p.MachineID != "local" || p.Missing {
		t.Fatalf("project: %+v", p)
	}
	if want := filepath.Join(tmp, ".orole-worktrees", "alpha"); p.WorktreeRoot != want || p.WorktreeRootCustom {
		t.Fatalf("root %q want %q", p.WorktreeRoot, want)
	}
	again, err := s.Add(ctx, dir)
	if err != nil || again.ID != p.ID {
		t.Fatalf("idempotent add: %+v %v", again, err)
	}
	b := repo(t, tmp, "beta")
	pb, err := s.Add(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOpened(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := s.Remove(ctx, pb.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, pb.ID); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("double remove: %v", err)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatal("remove must not delete files")
	}
	if list, _ = s.List(ctx); len(list) != 1 {
		t.Fatalf("after remove: %+v", list)
	}
	os.RemoveAll(dir)
	if list, _ = s.List(ctx); !list[0].Missing {
		t.Fatal("deleted dir should be Missing")
	}
}

func TestWorktreeLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	dir := repo(t, tmp, "proj")
	p, err := s.Add(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.Worktrees.List(ctx, p.ID)
	if err != nil || len(list) != 1 || !list[0].IsMain || list[0].Branch != "main" || list[0].Dirty != 0 {
		t.Fatalf("initial list: %+v %v", list, err)
	}
	wt, err := s.Worktrees.Create(ctx, p.ID, "Fix Login!", "main")
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(tmp, ".orole-worktrees", "proj", "fix-login")
	if wt.Path != wantPath || wt.Branch != "orole/fix-login" || wt.IsMain || wt.BaseRef != "main" || wt.Name != "fix-login" {
		t.Fatalf("created: %+v (want path %s)", wt, wantPath)
	}
	if _, err := os.Stat(filepath.Join(wantPath, "f.txt")); err != nil {
		t.Fatal("worktree not checked out:", err)
	}
	got, err := s.Worktrees.Get(ctx, wt.ID)
	if err != nil || got.ID != wt.ID {
		t.Fatalf("get: %+v %v", got, err)
	}
	os.WriteFile(filepath.Join(wantPath, "dirty.txt"), []byte("d"), 0o644)
	if got, _ = s.Worktrees.Get(ctx, wt.ID); got.Dirty != 1 || got.Untracked != 1 {
		t.Fatalf("dirty state: %+v", got)
	}
	if list, _ = s.Worktrees.List(ctx, p.ID); len(list) != 2 || !list[0].IsMain {
		t.Fatalf("list after create: %+v", list)
	}
	if _, err := s.Worktrees.Create(ctx, p.ID, "fix login", ""); !errors.Is(err, projects.ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	if err := s.Worktrees.Remove(ctx, wt.ID, false); err == nil {
		t.Fatal("removing a dirty worktree without force should fail")
	}
	if err := s.Worktrees.Remove(ctx, wt.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Worktrees.Get(ctx, wt.ID); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("get after remove: %v", err)
	}
	if err := s.Worktrees.Remove(ctx, list[0].ID, true); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("removing main checkout: %v", err)
	}
}

func TestWorktreeRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	p, _ := s.Add(ctx, repo(t, tmp, "proj"))
	for _, name := range []string{"", "   ", "!!!", "../escape", "..\\..\\x"} {
		wt, err := s.Worktrees.Create(ctx, p.ID, name, "")
		if name == "../escape" || name == "..\\..\\x" {
			// separators are slugged away; the result must still sit inside the root
			if err != nil {
				t.Errorf("%q: unexpected %v", name, err)
			} else if !strings.HasPrefix(wt.Path, filepath.Join(tmp, ".orole-worktrees", "proj")+string(filepath.Separator)) {
				t.Errorf("%q escaped the root: %s", name, wt.Path)
			}
			continue
		}
		if !errors.Is(err, projects.ErrInvalid) {
			t.Errorf("name %q: %v", name, err)
		}
	}
	for _, ref := range []string{"--detach", "-b", "a..b", "x y", "main;", "nonexistent-ref"} {
		if _, err := s.Worktrees.Create(ctx, p.ID, "x"+strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' {
				return r
			}
			return -1
		}, ref), ref); !errors.Is(err, projects.ErrInvalid) {
			t.Errorf("ref %q: %v", ref, err)
		}
	}
	if _, err := s.Worktrees.Create(ctx, "no-such-project", "x", ""); !errors.Is(err, projects.ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
	if _, err := s.SetWorktreeRoot(ctx, p.ID, "relative/dir"); !errors.Is(err, projects.ErrInvalid) {
		t.Errorf("relative root: %v", err)
	}
	if _, err := s.SetWorktreeRoot(ctx, p.ID, filepath.Join(p.Path, "inner")); !errors.Is(err, projects.ErrInvalid) {
		t.Errorf("root inside checkout: %v", err)
	}
}

func TestConfiguredWorktreeRoot(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	p, _ := s.Add(ctx, repo(t, tmp, "proj"))
	root := filepath.Join(tmp, "elsewhere")
	p, err := s.SetWorktreeRoot(ctx, p.ID, root)
	if err != nil || p.WorktreeRoot != root || !p.WorktreeRootCustom {
		t.Fatalf("set root: %+v %v", p, err)
	}
	wt, err := s.Worktrees.Create(ctx, p.ID, "feat", "")
	if err != nil || wt.Path != filepath.Join(root, "feat") {
		t.Fatalf("create in custom root: %+v %v", wt, err)
	}
	if p, _ = s.SetWorktreeRoot(ctx, p.ID, ""); p.WorktreeRootCustom {
		t.Fatalf("reset: %+v", p)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"Fix Login!": "fix-login", "a/b_c.d": "a-b-c-d", "---": "", "UPPER": "upper", "  x  ": "x"} {
		if got := projects.Slug(in); got != want {
			t.Errorf("Slug(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCreateWithoutGitOrFolder(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	tmp, _ := filepath.EvalSymlinks(t.TempDir())

	if _, err := s.Create(ctx, "  ", ""); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("blank create: %v", err)
	}
	// Several folderless projects may coexist, even with the same name.
	a, err := s.Create(ctx, "Notes", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(ctx, "Notes", "")
	if err != nil || a.ID == b.ID {
		t.Fatalf("second folderless project: %+v %v", b, err)
	}
	if a.Path != "" || a.Missing || a.IsGit || a.WorktreeRoot != "" {
		t.Fatalf("folderless project: %+v", a)
	}
	if wts, err := s.Worktrees.List(ctx, a.ID); err != nil || len(wts) != 0 {
		t.Fatalf("folderless worktrees: %v %v", wts, err)
	}
	if _, err := s.Worktrees.Create(ctx, a.ID, "x", ""); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("folderless worktree create: %v", err)
	}

	plain := filepath.Join(tmp, "plain")
	os.Mkdir(plain, 0o755)
	p, err := s.Create(ctx, "", plain) // blank name defaults to the folder name
	if err != nil || p.Name != "plain" || p.IsGit || p.Missing {
		t.Fatalf("plain folder: %+v %v", p, err)
	}
	if again, err := s.Create(ctx, "Other", plain); err != nil || again.ID != p.ID {
		t.Fatalf("same folder twice: %+v %v", again, err)
	}

	r, err := s.Create(ctx, "My Repo", repo(t, tmp, "gitrepo"))
	if err != nil || r.Name != "My Repo" || !r.IsGit {
		t.Fatalf("git folder: %+v %v", r, err)
	}
	if wts, err := s.Worktrees.List(ctx, r.ID); err != nil || len(wts) != 1 {
		t.Fatalf("git worktrees: %v %v", wts, err)
	}

	if _, err := s.Create(ctx, "x", filepath.Join(tmp, "nope")); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("missing folder: %v", err)
	}
	if _, err := s.SetWorktreeRoot(ctx, a.ID, filepath.Join(tmp, "wt")); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("folderless worktree root: %v", err)
	}
}

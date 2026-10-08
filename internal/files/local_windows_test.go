package files

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mklinkJunction creates a real directory junction (mklink /J), no privileges needed.
func mklinkJunction(t *testing.T, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("mklink /J unavailable: %v: %s", err, out)
	}
}

func TestApprovedRootThroughAncestorJunction(t *testing.T) {
	_, base := newRoot(t)
	target := filepath.Join(base, "target")
	root := filepath.Join(target, "approved")
	outside := filepath.Join(target, "approved-sibling")
	for _, dir := range []string{root, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	mklinkJunction(t, alias, target)
	aliasRoot := filepath.Join(alias, "approved")
	canonical, err := CanonicalRoot(aliasRoot)
	if err != nil || !samePath(canonical, root) {
		t.Fatalf("canonical root: %q %v", canonical, err)
	}
	l := NewLocal(StaticRoots("local", canonical))
	file := filepath.Join(aliasRoot, "note.txt")
	info, err := l.Write(ctx, file, []byte("original"), WriteOptions{Create: true})
	if err != nil {
		t.Fatalf("create through ancestor junction: %v", err)
	}
	if _, err := l.Stat(ctx, file); err != nil {
		t.Fatalf("stat through ancestor junction: %v", err)
	}
	if _, err := l.Write(ctx, file, []byte("updated"), WriteOptions{IfMatch: info.Etag}); err != nil {
		t.Fatalf("update through ancestor junction: %v", err)
	}
	if got, err := l.Read(ctx, file, ReadOptions{}); err != nil || string(got.Data) != "updated" {
		t.Fatalf("read through ancestor junction: %+v %v", got, err)
	}
	if entries, err := l.List(ctx, aliasRoot); err != nil || len(entries) != 1 {
		t.Fatalf("list through ancestor junction: %+v %v", entries, err)
	}
	dir := filepath.Join(aliasRoot, "folder")
	if _, err := l.Mkdir(ctx, dir); err != nil {
		t.Fatalf("mkdir through ancestor junction: %v", err)
	}
	renamed := filepath.Join(dir, "renamed.txt")
	if _, err := l.Rename(ctx, file, renamed); err != nil {
		t.Fatalf("rename through ancestor junction: %v", err)
	}
	if _, err := l.Delete(ctx, renamed, DeleteOptions{Permanent: true}); err != nil {
		t.Fatalf("delete through ancestor junction: %v", err)
	}

	write(t, filepath.Join(outside, "secret.txt"), "private")
	mklinkJunction(t, filepath.Join(root, "escape"), outside)
	for _, denied := range []string{
		filepath.Join(alias, "approved-sibling"),
		filepath.Join(aliasRoot, "escape"),
	} {
		if _, err := l.Read(ctx, filepath.Join(denied, "secret.txt"), ReadOptions{}); !errors.Is(err, ErrDenied) {
			t.Fatalf("read outside approved root via %q: %v", denied, err)
		}
		if _, err := l.Write(ctx, filepath.Join(denied, "new.txt"), []byte("bad"), WriteOptions{Create: true}); !errors.Is(err, ErrDenied) {
			t.Fatalf("create outside approved root via %q: %v", denied, err)
		}
	}

	// Replacing the approved canonical directory with a junction must still
	// fail closed, even though the caller uses the original ancestor alias.
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	mklinkJunction(t, root, outside)
	if _, err := l.Read(ctx, filepath.Join(aliasRoot, "secret.txt"), ReadOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("read through retargeted root: %v", err)
	}
	if _, err := l.Read(ctx, filepath.Join(root, "secret.txt"), ReadOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("canonical read through retargeted root: %v", err)
	}
}

func TestJunctionEscapeIsDenied(t *testing.T) {
	l, root := newRoot(t)
	outside, _ := realPath(t.TempDir())
	write(t, filepath.Join(outside, "secret.txt"), "TOP SECRET")
	if err := os.Mkdir(filepath.Join(outside, "victim"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(outside, "victim", "keep.txt"), "keep")
	link := filepath.Join(root, "jn")
	mklinkJunction(t, link, outside)

	secret := filepath.Join(link, "secret.txt")
	if _, err := l.Read(ctx, secret, ReadOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("read through junction: %v", err)
	}
	if _, err := l.List(ctx, link); !errors.Is(err, ErrDenied) {
		t.Fatalf("list through junction: %v", err)
	}
	if _, err := l.Stat(ctx, secret); !errors.Is(err, ErrDenied) {
		t.Fatalf("stat through junction: %v", err)
	}
	if _, err := l.Write(ctx, filepath.Join(link, "new.txt"), []byte("x"), WriteOptions{Create: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("create through junction: %v", err)
	}
	if _, err := l.Write(ctx, secret, []byte("x"), WriteOptions{Force: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("overwrite through junction: %v", err)
	}
	if _, err := l.Mkdir(ctx, filepath.Join(link, "d")); !errors.Is(err, ErrDenied) {
		t.Fatalf("mkdir through junction: %v", err)
	}
	if _, err := l.Rename(ctx, secret, filepath.Join(root, "stolen.txt")); !errors.Is(err, ErrDenied) {
		t.Fatalf("rename out through junction: %v", err)
	}
	if _, err := l.Delete(ctx, secret, DeleteOptions{Permanent: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("delete through junction: %v", err)
	}
	if _, err := l.Search(ctx, link, "secret", SearchOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("search through junction: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "secret.txt")); string(b) != "TOP SECRET" {
		t.Fatalf("outside file modified: %q", b)
	}

	// The junction is listed as a link and is never followed by search.
	es, err := l.List(ctx, root)
	if err != nil || len(es) != 1 || !es[0].Link {
		t.Fatalf("list root: %+v %v", es, err)
	}
	hits, err := l.Search(ctx, root, "secret", SearchOptions{})
	if err != nil || len(hits) != 0 {
		t.Fatalf("search followed junction: %+v %v", hits, err)
	}
}

func TestJunctionInsideRootIsAllowedAndDeleteRemovesOnlyTheLink(t *testing.T) {
	l, root := newRoot(t)
	outside, _ := realPath(t.TempDir())
	write(t, filepath.Join(outside, "keep.txt"), "keep")
	// A junction pointing out of the root inside a directory we then delete.
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	mklinkJunction(t, filepath.Join(root, "dir", "out"), outside)

	if _, err := l.Delete(ctx, filepath.Join(root, "dir"), DeleteOptions{Permanent: true}); err != nil {
		t.Fatalf("permanent delete of dir containing junction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dir")); err == nil {
		t.Fatal("dir still exists")
	}
	if b, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("junction target was deleted through the link: %v", err)
	}

	// Deleting the junction itself (recycle mode too) removes only the link.
	mklinkJunction(t, filepath.Join(root, "j2"), outside)
	if _, err := l.Delete(ctx, filepath.Join(root, "j2"), DeleteOptions{}); err != nil {
		t.Fatalf("delete junction: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "j2")); err == nil {
		t.Fatal("junction still exists")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("target lost: %v", err)
	}

	// A junction that stays inside the root is fine.
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "real", "in.txt"), "in")
	mklinkJunction(t, filepath.Join(root, "alias"), filepath.Join(root, "real"))
	rd, err := l.Read(ctx, filepath.Join(root, "alias", "in.txt"), ReadOptions{})
	if err != nil || string(rd.Data) != "in" {
		t.Fatalf("read via internal junction: %v", err)
	}
}

func TestReservedAndIllegalNames(t *testing.T) {
	l, root := newRoot(t)
	for _, n := range []string{"CON", "con", "NUL.txt", "prn.tar.gz", "COM1", "lpt9.log", "AUX", "COM\u00b9", "trail.", "trailsp ", "a:stream", "a?b", "a<b", "x|y", `a"b`, "CONIN$"} {
		p := filepath.Join(root, n)
		if _, err := l.Write(ctx, p, []byte("x"), WriteOptions{Create: true}); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Write(%q): want ErrInvalidPath, got %v", n, err)
		}
		if _, err := l.Mkdir(ctx, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Mkdir(%q): want ErrInvalidPath, got %v", n, err)
		}
		if err := ValidateName(n); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("ValidateName(%q): %v", n, err)
		}
	}
	for _, n := range []string{"console", "com10", "nulls.txt", "my.file.txt", "$odd", "a b"} {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q): %v", n, err)
		}
	}
	// Renaming into a reserved name is refused as well.
	write(t, filepath.Join(root, "ok.txt"), "x")
	if _, err := l.Rename(ctx, filepath.Join(root, "ok.txt"), filepath.Join(root, "NUL")); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("rename to NUL: %v", err)
	}
	// Nothing was created.
	es, _ := os.ReadDir(root)
	if len(es) != 1 {
		t.Errorf("unexpected entries: %v", es)
	}
}

func TestDevicePathsRejected(t *testing.T) {
	l, root := newRoot(t)
	for _, p := range []string{`\\?\` + root, `\\.\` + root, `\\.\C:\Windows`, `\\?\C:\`, `//?/` + root, `\??\C:\`, `\\.\PhysicalDrive0`, `\\.\pipe\x`} {
		if _, err := l.Stat(ctx, p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Stat(%q): %v", p, err)
		}
	}
}

func TestDriveRelativeAndUNC(t *testing.T) {
	l, root := newRoot(t)
	for _, p := range []string{`C:foo`, `\foo`, `\\server\share\x`, `\\127.0.0.1\c$\Windows`} {
		if _, err := l.Stat(ctx, p); err == nil {
			t.Errorf("Stat(%q) succeeded", p)
		}
	}
	// A path differing only in case still maps to the root (NTFS is case-insensitive)...
	up := strings.ToUpper(root)
	if _, err := l.Stat(ctx, up); err != nil {
		t.Errorf("case-different root path: %v", err)
	}
	// ...but a longer sibling prefix does not.
	if _, err := l.Stat(ctx, root+"x"); err == nil {
		t.Error("sibling prefix accepted")
	}
}

func TestGitProviderReadOnlyDotGitAndStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	_, root := newRoot(t)
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	write(t, filepath.Join(root, "tracked.txt"), "1")
	os.Mkdir(filepath.Join(root, "sub"), 0o755)
	write(t, filepath.Join(root, "sub", "inner.txt"), "1")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	write(t, filepath.Join(root, "tracked.txt"), "2")
	write(t, filepath.Join(root, "untracked.txt"), "u")
	write(t, filepath.Join(root, "sub", "inner.txt"), "2")

	g := NewGit(StaticRoots("git", root))
	es, err := g.List(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]string{}
	for _, e := range es {
		st[e.Name] = e.Git
	}
	if strings.TrimSpace(st["tracked.txt"]) != "M" || st["untracked.txt"] != "??" || strings.TrimSpace(st["sub"]) != "M" {
		t.Fatalf("status: %+v", st)
	}
	if _, err := g.Write(ctx, filepath.Join(root, ".git", "config"), []byte("x"), WriteOptions{Force: true}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("write into .git: %v", err)
	}
	if _, err := g.Delete(ctx, filepath.Join(root, ".git"), DeleteOptions{Permanent: true}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("delete .git: %v", err)
	}
	if _, err := g.Rename(ctx, filepath.Join(root, "tracked.txt"), filepath.Join(root, ".git", "x")); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("rename into .git: %v", err)
	}
	if _, err := g.Read(ctx, filepath.Join(root, ".git", "HEAD"), ReadOptions{}); err != nil {
		t.Fatalf("read .git/HEAD: %v", err)
	}
	// A non-repository root is unusable for the git provider.
	plain, _ := realPath(t.TempDir())
	g2 := NewGit(StaticRoots("git", plain))
	if _, err := g2.List(ctx, plain); !errors.Is(err, ErrDenied) {
		t.Fatalf("non-repo root: %v", err)
	}
	if rs, _ := g2.Roots(ctx); !rs[0].Missing {
		t.Fatalf("non-repo root should be flagged: %+v", rs)
	}
}

package files

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var ctx = context.Background()

// newRoot returns a provider whose single root is a fresh temp dir (resolved,
// so 8.3 temp paths compare equal) plus that root path.
func newRoot(t *testing.T) (*Local, string) {
	t.Helper()
	dir, err := realPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewLocal(StaticRoots("local", dir)), dir
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPathPolicyLexical(t *testing.T) {
	l, root := newRoot(t)
	write(t, filepath.Join(root, "a.txt"), "x")
	outside, _ := realPath(t.TempDir())
	write(t, filepath.Join(outside, "secret.txt"), "s")

	bad := map[string]string{
		"empty":           "",
		"relative":        "a.txt",
		"dotdot":          filepath.Join(root, "sub") + string(filepath.Separator) + ".." + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(outside),
		"dotdot-file":     root + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(outside) + string(filepath.Separator) + "secret.txt",
		"outside":         filepath.Join(outside, "secret.txt"),
		"outside-sibling": root + "-evil",
		"nul":             root + string(filepath.Separator) + "a\x00.txt",
		"parent-of-root":  filepath.Dir(root),
	}
	for name, p := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := l.Stat(ctx, p); err == nil {
				t.Fatalf("Stat(%q) succeeded, want error", p)
			}
			if _, err := l.Read(ctx, p, ReadOptions{}); err == nil {
				t.Fatalf("Read(%q) succeeded, want error", p)
			}
			if _, err := l.Write(ctx, p, []byte("x"), WriteOptions{Create: true}); err == nil {
				t.Fatalf("Write(%q) succeeded, want error", p)
			}
		})
	}
	if _, err := l.Stat(ctx, filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("legit path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestNoRootsMeansNoAccess(t *testing.T) {
	dir := t.TempDir()
	l := NewLocal(StaticRoots("local"))
	if _, err := l.List(ctx, dir); !errors.Is(err, ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
}

func TestProvidersDoNotShareRoots(t *testing.T) {
	_, root := newRoot(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "f.txt"), "x")
	local := NewLocal(StaticRoots("local")) // nothing approved for local
	git := NewGit(StaticRoots("git", root))
	reg := NewRegistry(local, git)
	lp, _ := reg.Get("local")
	gp, _ := reg.Get("git")
	if _, err := lp.List(ctx, root); !errors.Is(err, ErrDenied) {
		t.Fatalf("local must not see git's root: %v", err)
	}
	if _, err := gp.List(ctx, root); err != nil {
		t.Fatalf("git list: %v", err)
	}
	if _, err := reg.Get("remote"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("remote: %v", err)
	}
	if _, err := reg.Get("drive"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("drive: %v", err)
	}
	if _, err := reg.Get("nope"); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestReadWriteRoundTripAndEtag(t *testing.T) {
	l, root := newRoot(t)
	p := filepath.Join(root, "n.txt")
	info, err := l.Write(ctx, p, []byte("hello"), WriteOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.Etag == "" || info.Size != 5 {
		t.Fatalf("info %+v", info)
	}
	rd, err := l.Read(ctx, p, ReadOptions{})
	if err != nil || string(rd.Data) != "hello" || rd.Info.Etag != info.Etag {
		t.Fatalf("read %+v err %v (want etag %s)", rd, err, info.Etag)
	}
	info2, err := l.Write(ctx, p, []byte("hello world"), WriteOptions{IfMatch: rd.Info.Etag})
	if err != nil {
		t.Fatal(err)
	}
	if info2.Etag == info.Etag {
		t.Fatal("etag did not change")
	}
	// no temp files left behind
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".orole-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func TestWriteConflict(t *testing.T) {
	l, root := newRoot(t)
	p := filepath.Join(root, "c.txt")
	info, err := l.Write(ctx, p, []byte("v1"), WriteOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	// external edit
	write(t, p, "v1-external-edit")

	_, err = l.Write(ctx, p, []byte("mine"), WriteOptions{IfMatch: info.Etag})
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want ConflictError, got %v", err)
	}
	if ce.Current.Size != int64(len("v1-external-edit")) {
		t.Fatalf("conflict current %+v", ce.Current)
	}
	if b, _ := os.ReadFile(p); string(b) != "v1-external-edit" {
		t.Fatalf("conflicting write modified the file: %q", b)
	}
	// same size, different content must conflict too
	write(t, p, "AB")
	cur, _ := l.Stat(ctx, p)
	write(t, p, "CD")
	if _, err := l.Write(ctx, p, []byte("x"), WriteOptions{IfMatch: cur.Etag}); !errors.As(err, &ce) {
		t.Fatalf("same-size change not detected: %v", err)
	}
	// missing etag is refused, not silently overwritten
	if _, err := l.Write(ctx, p, []byte("x"), WriteOptions{}); !errors.Is(err, ErrNeedsEtag) {
		t.Fatalf("want ErrNeedsEtag, got %v", err)
	}
	// explicit force overwrites
	if _, err := l.Write(ctx, p, []byte("forced"), WriteOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "forced" {
		t.Fatalf("force: %q", b)
	}
	// deleted underneath
	cur, _ = l.Stat(ctx, p)
	os.Remove(p)
	if _, err := l.Write(ctx, p, []byte("x"), WriteOptions{IfMatch: cur.Etag}); !errors.As(err, &ce) || !ce.Deleted {
		t.Fatalf("want deleted conflict, got %v", err)
	}
	// Create refuses to replace an existing file
	write(t, p, "exists")
	if _, err := l.Write(ctx, p, []byte("x"), WriteOptions{Create: true}); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
}

func TestTouchWithoutChangeIsNotAConflict(t *testing.T) {
	l, root := newRoot(t)
	p := filepath.Join(root, "t.txt")
	info, _ := l.Write(ctx, p, []byte("same"), WriteOptions{Create: true})
	later := info.ModTime.Add(5e9)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write(ctx, p, []byte("edited"), WriteOptions{IfMatch: info.Etag}); err != nil {
		t.Fatalf("identical content with new mtime must not conflict: %v", err)
	}
}

func TestConcurrentWritersOnlyOneWins(t *testing.T) {
	l, root := newRoot(t)
	p := filepath.Join(root, "race.txt")
	info, _ := l.Write(ctx, p, []byte("base"), WriteOptions{Create: true})
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, conflicts := 0, 0
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := l.Write(ctx, p, []byte("writer-"+string(rune('a'+i))), WriteOptions{IfMatch: info.Etag})
			mu.Lock()
			defer mu.Unlock()
			var ce *ConflictError
			switch {
			case err == nil:
				wins++
			case errors.As(err, &ce):
				conflicts++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || conflicts != 7 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}

func TestSizeLimits(t *testing.T) {
	l, root := newRoot(t)
	big := filepath.Join(root, "big.bin")
	if err := os.WriteFile(big, bytes.Repeat([]byte("a"), MaxReadBytes+10), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Read(ctx, big, ReadOptions{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("whole read of oversized file: %v", err)
	}
	rd, err := l.Read(ctx, big, ReadOptions{Offset: 5, Length: 100})
	if err != nil || len(rd.Data) != 100 || !rd.Truncated || rd.Size != MaxReadBytes+10 {
		t.Fatalf("range read: %+v %v", rd, err)
	}
	rd, err = l.Read(ctx, big, ReadOptions{Offset: MaxReadBytes, Length: 1000})
	if err != nil || len(rd.Data) != 10 || rd.Truncated {
		t.Fatalf("tail read: len=%d trunc=%v err=%v", len(rd.Data), rd.Truncated, err)
	}
	rd, err = l.Read(ctx, big, ReadOptions{Length: 1 << 30})
	if err != nil || len(rd.Data) != MaxReadBytes {
		t.Fatalf("clamped range: len=%d err=%v", len(rd.Data), err)
	}
	if _, err := l.Read(ctx, big, ReadOptions{MaxBytes: 10}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("custom max: %v", err)
	}
	_, err = l.Write(ctx, filepath.Join(root, "w.bin"), make([]byte, MaxReadBytes+1), WriteOptions{Create: true})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "w.bin")); err == nil {
		t.Fatal("oversized write created a file")
	}
}

func TestListMkdirRename(t *testing.T) {
	l, root := newRoot(t)
	write(t, filepath.Join(root, "b.txt"), "b")
	write(t, filepath.Join(root, "A.txt"), "a")
	if _, err := l.Mkdir(ctx, filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Mkdir(ctx, filepath.Join(root, "dir")); !errors.Is(err, ErrExists) {
		t.Fatalf("mkdir twice: %v", err)
	}
	if _, err := l.Mkdir(ctx, filepath.Join(root, "x", "y")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mkdir without parent: %v", err)
	}
	es, err := l.List(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "dir,A.txt,b.txt" {
		t.Fatalf("order: %v", names)
	}
	if _, err := l.Rename(ctx, filepath.Join(root, "b.txt"), filepath.Join(root, "A.txt")); !errors.Is(err, ErrExists) {
		t.Fatalf("rename onto existing: %v", err)
	}
	if _, err := l.Rename(ctx, filepath.Join(root, "b.txt"), filepath.Join(root, "c.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Rename(ctx, filepath.Join(root, "dir"), filepath.Join(root, "dir", "sub")); err == nil {
		t.Fatal("moved a directory into itself")
	}
	if _, err := l.Rename(ctx, root, filepath.Join(root, "..", "x")); err == nil {
		t.Fatal("renamed a root")
	}
	if _, err := l.Rename(ctx, filepath.Join(root, "A.txt"), filepath.Join(filepath.Dir(root), "esc.txt")); err == nil {
		t.Fatal("renamed out of the root")
	}
}

func TestDeletePermanentAndRootProtected(t *testing.T) {
	l, root := newRoot(t)
	p := filepath.Join(root, "d.txt")
	write(t, p, "x")
	if _, err := l.Delete(ctx, root, DeleteOptions{Permanent: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("delete root: %v", err)
	}
	res, err := l.Delete(ctx, p, DeleteOptions{Permanent: true})
	if err != nil || res.Recycled {
		t.Fatalf("permanent delete: %+v %v", res, err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("file still there")
	}
	if _, err := l.Delete(ctx, p, DeleteOptions{Permanent: true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestSearch(t *testing.T) {
	l, root := newRoot(t)
	os.MkdirAll(filepath.Join(root, "src", "deep"), 0o755)
	os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	write(t, filepath.Join(root, "src", "main.go"), "")
	write(t, filepath.Join(root, "src", "deep", "main_test.go"), "")
	write(t, filepath.Join(root, "node_modules", "pkg", "main.js"), "")
	write(t, filepath.Join(root, ".git", "main.cfg"), "")
	write(t, filepath.Join(root, "README.md"), "")
	got, err := l.Search(ctx, root, "MAIN", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "main.go,main_test.go" {
		t.Fatalf("results: %v", names)
	}
	got, _ = l.Search(ctx, root, "main", SearchOptions{IncludeHidden: true})
	if len(got) != 4 {
		t.Fatalf("includeHidden results: %d", len(got))
	}
	got, _ = l.Search(ctx, root, "main", SearchOptions{Limit: 1})
	if len(got) != 1 {
		t.Fatalf("limit: %d", len(got))
	}
	got, _ = l.Search(ctx, root, "   ", SearchOptions{})
	if len(got) != 0 {
		t.Fatal("blank query matched")
	}
	if _, err := l.Search(ctx, filepath.Dir(root), "x", SearchOptions{}); err == nil {
		t.Fatal("search outside root allowed")
	}
}

func TestRootChangedAfterApprovalIsDenied(t *testing.T) {
	l, root := newRoot(t)
	write(t, filepath.Join(root, "f"), "x")
	if rs, _ := l.Roots(ctx); len(rs) != 1 || rs[0].Missing {
		t.Fatalf("roots: %+v", rs)
	}
	gone := filepath.Join(filepath.Dir(root), "renamed-"+filepath.Base(root))
	if err := os.Rename(root, gone); err != nil {
		t.Skipf("cannot rename temp root: %v", err)
	}
	defer os.Rename(gone, root)
	if _, err := l.List(ctx, root); err == nil {
		t.Fatal("list on vanished root succeeded")
	}
	if rs, _ := l.Roots(ctx); !rs[0].Missing {
		t.Fatalf("root should be reported missing: %+v", rs)
	}
}

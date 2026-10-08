package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func TestRootStoreLifecycleAndProviderSeparation(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	local := RootStore{DB: db.DB, Provider: "local"}
	git := RootStore{DB: db.DB, Provider: "git"}
	dir, _ := realPath(t.TempDir())

	r, err := local.Add(ctx, dir, "")
	if err != nil || r.ID == "" || r.Path != dir {
		t.Fatalf("add: %+v %v", r, err)
	}
	again, err := local.Add(ctx, dir, "other label")
	if err != nil || again.ID != r.ID {
		t.Fatalf("idempotent add: %+v %v", again, err)
	}
	if rs, _ := git.Roots(ctx); len(rs) != 0 {
		t.Fatalf("git sees local's root: %+v", rs)
	}
	// Provider built on the store enforces the roots.
	p := NewLocal(local)
	if _, err := p.List(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := NewGit(git).List(ctx, dir); !errors.Is(err, ErrDenied) {
		t.Fatalf("git provider must not accept a local root: %v", err)
	}
	// Invalid roots.
	file := filepath.Join(dir, "f.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	for _, bad := range []string{"", "relative", file, filepath.Join(dir, "missing"), filepath.Join(dir, "..", "x")} {
		if _, err := local.Add(ctx, bad, ""); err == nil {
			t.Errorf("Add(%q) accepted", bad)
		}
	}
	if _, err := local.Remove(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.List(ctx, dir); !errors.Is(err, ErrDenied) {
		t.Fatalf("after remove: %v", err)
	}
	if _, err := local.Remove(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("removing a root touched files")
	}
}

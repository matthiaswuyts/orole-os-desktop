package machines

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/files"
)

func TestLocalMachineAccessThroughAncestorJunction(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	target := t.TempDir()
	root := filepath.Join(target, "approved")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", alias, target).CombinedOutput(); err != nil {
		t.Fatalf("create ancestor junction: %v: %s", err, out)
	}
	aliasRoot := filepath.Join(alias, "approved")
	file := filepath.Join(aliasRoot, "note.txt")
	grant, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{aliasRoot}, AllowWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := files.CanonicalRoot(root)
	if err != nil || len(grant.Roots) != 1 || grant.Roots[0] != canonical {
		t.Fatalf("grant did not store canonical root: %+v %q %v", grant.Roots, canonical, err)
	}
	if _, err := m.WriteFile(ctx, "user", FileWriteRequest{MachineID: "local", Path: file, Data: []byte("original"), Options: files.WriteOptions{Create: true}}); err != nil {
		t.Fatalf("create through approved alias: %v", err)
	}
	info, err := m.StatFile(ctx, "user", FileRequest{MachineID: "local", Path: file})
	if err != nil {
		t.Fatalf("stat through approved alias: %v", err)
	}
	if _, err := m.WriteFile(ctx, "user", FileWriteRequest{MachineID: "local", Path: file, Data: []byte("updated"), Options: files.WriteOptions{IfMatch: info.Etag}}); err != nil {
		t.Fatalf("update through approved alias: %v", err)
	}
	if got, err := m.ReadFile(ctx, "user", FileReadRequest{MachineID: "local", Path: file}); err != nil || string(got.Data) != "updated" {
		t.Fatalf("read through approved alias: %+v %v", got, err)
	}
	if _, err := m.ListFiles(ctx, "user", FileRequest{MachineID: "local", Path: alias}); !errors.Is(err, files.ErrDenied) {
		t.Fatalf("grant authorized alias ancestor: %v", err)
	}
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StatFile(ctx, "user", FileRequest{MachineID: "local", Path: file}); !errors.Is(err, files.ErrDenied) {
		t.Fatalf("alias retained revoked access: %v", err)
	}
}

package files

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentProviderWritesShareConcurrencyNotPermissions(t *testing.T) {
	local, root := newRoot(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	git := NewGit(StaticRoots("git", root))
	path := filepath.Join(root, "shared.txt")
	initial, err := local.Write(ctx, path, []byte("base"), WriteOptions{Create: true})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, provider := range []Provider{local, git} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := provider.Write(ctx, path, []byte{byte('a' + i)}, WriteOptions{IfMatch: initial.Etag})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	winners, conflicts := 0, 0
	for err := range errs {
		var conflict *ConflictError
		switch {
		case err == nil:
			winners++
		case errors.As(err, &conflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
}

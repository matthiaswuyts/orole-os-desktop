package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinkEscapeAndSafeLinkRename(t *testing.T) {
	l, root := newRoot(t)
	outside := filepath.Join(t.TempDir(), "private.txt")
	write(t, outside, "private content")
	link := filepath.Join(root, "shortcut.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	if _, err := l.Read(ctx, link, ReadOptions{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("outside read: %v", err)
	}
	if _, err := l.Write(ctx, link, []byte("edited"), WriteOptions{Force: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("outside write: %v", err)
	}
	renamed := filepath.Join(root, "renamed-link.txt")
	info, err := l.Rename(ctx, link, renamed)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Link || info.Etag != "" {
		t.Fatalf("rename exposed target metadata: %+v", info)
	}
	if _, err := l.Delete(ctx, renamed, DeleteOptions{Permanent: true}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "private content" {
		t.Fatalf("link target changed: %q %v", data, err)
	}
}

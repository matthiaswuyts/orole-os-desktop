package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

// recycledCopies finds items in the current user's Recycle Bin with the given extension.
func recycledCopies(t *testing.T, vol, ext string) []string {
	t.Helper()
	sid, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Skipf("token user: %v", err)
	}
	dir := filepath.Join(vol, "$Recycle.Bin", sid.User.Sid.String())
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("cannot read recycle bin %s: %v", dir, err)
	}
	var out []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "$R") && strings.HasSuffix(e.Name(), ext) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func purge(paths []string) {
	for _, p := range paths {
		os.RemoveAll(p)
		os.Remove(filepath.Join(filepath.Dir(p), "$I"+strings.TrimPrefix(filepath.Base(p), "$R")))
	}
}

func TestDeleteMovesToRecycleBin(t *testing.T) {
	l, root := newRoot(t)
	vol := filepathVolumeRoot(root)
	if vol == "" {
		t.Skip("temp dir is not on a drive-letter volume")
	}
	rp, _ := windows.UTF16PtrFromString(vol)
	if windows.GetDriveType(rp) != driveFixed {
		t.Skip("temp dir is not on a fixed drive")
	}
	ext := ".orolebintest-" + uuid.NewString()
	before := len(recycledCopies(t, vol, ext))

	p := filepath.Join(root, "recycle-me"+ext)
	write(t, p, "recycle marker content")
	res, err := l.Delete(ctx, p, DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Recycled {
		t.Fatal("not reported as recycled")
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("file still at original location")
	}
	after := recycledCopies(t, vol, ext)
	defer purge(after)
	if len(after) != before+1 {
		t.Fatalf("recycle bin copies before=%d after=%d", before, len(after))
	}
	var found bool
	for _, c := range after {
		if b, err := os.ReadFile(c); err == nil && string(b) == "recycle marker content" {
			found = true
		}
	}
	if !found {
		t.Fatal("recycled copy with original content not found")
	}

	// Directories are recycled whole.
	d := filepath.Join(root, "dir"+ext)
	os.MkdirAll(filepath.Join(d, "inner"), 0o755)
	write(t, filepath.Join(d, "inner", "f.txt"), "x")
	res, err = l.Delete(ctx, d, DeleteOptions{})
	if err != nil || !res.Recycled {
		t.Fatalf("recycle dir: %+v %v", res, err)
	}
	defer purge(recycledCopies(t, vol, ext))
	if _, err := os.Stat(d); err == nil {
		t.Fatal("dir still there")
	}
}

func TestRecycleRefusedOnVolumeWithoutBinFallsBackToExplicitPermanent(t *testing.T) {
	// A UNC path has no drive letter: recycling must be refused rather than
	// silently deleting permanently.
	if err := recycle(`\\localhost\c$\definitely-not-there`); !errors.Is(err, ErrNoRecycleBin) {
		t.Fatalf("want ErrNoRecycleBin, got %v", err)
	}
}

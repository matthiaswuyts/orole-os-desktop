package projects_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/files"
	"golang.org/x/sys/windows"
)

func TestProjectRegistrationExpandsShortNames(t *testing.T) {
	s := newService(t)
	dir := repo(t, t.TempDir(), "repository with a long name")
	canonical, err := files.CanonicalRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	input, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buffer [32768]uint16
	n, err := windows.GetShortPathName(input, &buffer[0], uint32(len(buffer)))
	if err != nil {
		t.Fatal(err)
	}
	if n >= uint32(len(buffer)) {
		t.Fatal("short path exceeds native path buffer")
	}
	short := windows.UTF16ToString(buffer[:n])
	if strings.EqualFold(short, canonical) {
		t.Skip("filesystem does not provide an 8.3 alias")
	}
	p, err := s.Add(context.Background(), short)
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != canonical || p.Name != filepath.Base(canonical) {
		t.Fatalf("registration did not pin expanded native path: %+v; want %q", p, canonical)
	}
	stored, err := s.Get(context.Background(), p.ID)
	if err != nil || stored.Path != canonical {
		t.Fatalf("stored canonical approval: %+v %v", stored, err)
	}
	again, err := s.Add(context.Background(), canonical)
	if err != nil || again.ID != p.ID {
		t.Fatalf("canonical registration must reuse short-name registration: %+v %v", again, err)
	}
}

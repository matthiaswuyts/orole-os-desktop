package agents_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
)

func TestTaskFileReadsOnlyBoundedLocalWorkspaceFiles(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "readme.md"), []byte("# Task output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := e.st.SaveSession(agents.Session{ID: "local-file-task", Kind: "claude", Machine: "local", Dir: dir, Mode: agents.ModeStructured, StartedAt: now, UpdatedAt: now}, ""); err != nil {
		t.Fatal(err)
	}
	got, err := e.m.TaskFile(context.Background(), "local-file-task", "nested/readme.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "readme.md" || got.Path != "nested/readme.md" || got.Text == nil || *got.Text != "# Task output\n" || string(got.Data) != "# Task output\n" || got.Truncated {
		t.Fatalf("unexpected preview: %#v", got)
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("file symlink unavailable on this platform/account: %v", err)
	}
	if _, err := e.m.TaskFile(context.Background(), "local-file-task", "linked.txt"); err == nil {
		t.Fatal("TaskFile followed a symlink outside the approved workspace")
	}
	for _, path := range []string{"../outside", `C:\Windows\win.ini`, "nested\\..\\..\\outside", "nested:secret"} {
		if _, err := e.m.TaskFile(context.Background(), "local-file-task", path); err == nil {
			t.Errorf("TaskFile(%q) unexpectedly succeeded", path)
		}
	}
	if _, err := e.m.TaskFile(context.Background(), "local-file-task", "missing"); err == nil || !strings.Contains(err.Error(), "unavailable or outside") {
		t.Fatalf("missing file error = %v", err)
	}
}

func TestTaskFileDoesNotExposeUnboundedBinaryOrRemotePaths(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	large := make([]byte, agents.MaxAttachmentBytes+1)
	for i := range large {
		large[i] = 0xFF
	}
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), large, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, s := range []agents.Session{
		{ID: "large-local", Kind: "claude", Machine: "local", Dir: dir, Mode: agents.ModeStructured, StartedAt: now, UpdatedAt: now},
		{ID: "remote-file", Kind: "claude", Machine: "box-a", Dir: dir, Mode: agents.ModeProcess, StartedAt: now, UpdatedAt: now},
	} {
		if err := e.st.SaveSession(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.m.TaskFile(context.Background(), "large-local", "large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.Data != nil || got.Text != nil || got.Size != int64(len(large)) {
		t.Fatalf("large binary preview exposed content or lost size: %#v", got)
	}
	if _, err := e.m.TaskFile(context.Background(), "remote-file", "large.bin"); err == nil {
		t.Fatal("remote process file was read from the GUI host")
	}
}

package projects_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/projects"
)

func TestFolderlessTaskWorkspaceStableAndPrivate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newService(t)
	root := filepath.Join(t.TempDir(), "project-workspaces")
	p, err := s.Create(ctx, "Research", "")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := s.TaskDirectory(ctx, p.ID, "local", "", root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != p.ID {
		t.Fatalf("workspace must use stable project ID: %s", dir)
	}
	artifact := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(artifact, []byte("keep these notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := s.TaskDirectory(ctx, p.ID, "local", dir, root)
	if err != nil || again != dir {
		t.Fatalf("repeat workspace = %q, %v", again, err)
	}
	current, err := s.Get(ctx, p.ID)
	if err != nil || current.Path != "" || current.IsGit || current.Missing {
		t.Fatalf("managed workspace changed optional-folder semantics: %+v, %v", current, err)
	}
	other, err := s.Create(ctx, "Research", "")
	if err != nil {
		t.Fatal(err)
	}
	otherDir, err := s.TaskDirectory(ctx, other.ID, "local", "", root)
	if err != nil || otherDir == dir {
		t.Fatalf("same-name projects shared workspace: %q, %v", otherDir, err)
	}
	if _, err := s.TaskDirectory(ctx, p.ID, "local", otherDir, root); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("unrelated directory accepted: %v", err)
	}
	if _, err := s.TaskDirectory(ctx, p.ID, "remote", "", root); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("remote machine accepted: %v", err)
	}
	if err := s.Remove(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("removing project deleted managed files: %v", err)
	}
	if _, err := s.TaskDirectory(ctx, p.ID, "local", "", root); !errors.Is(err, projects.ErrNotFound) {
		t.Fatalf("removed project resolved: %v", err)
	}
}

func TestAttachedTaskWorkspaceDoesNotFallBackWhenMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newService(t)
	p, err := s.Create(ctx, "Attached", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "project-workspaces")
	dir, err := s.TaskDirectory(ctx, p.ID, "local", "", root)
	if err != nil || dir != p.Path {
		t.Fatalf("attached directory = %q, %v", dir, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("attached project created managed root: %v", err)
	}
	if err := os.Remove(p.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TaskDirectory(ctx, p.ID, "local", "", root); err == nil {
		t.Fatal("missing attached folder silently replaced with workspace")
	}
}

func TestTaskDirectoryAcceptsOnlyOwnWorktrees(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newService(t)
	p, err := s.Add(ctx, repo(t, t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	wt, err := s.Worktrees.Create(ctx, p.ID, "task", "")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := s.TaskDirectory(ctx, p.ID, "local", wt.Path, t.TempDir())
	if err != nil || dir != wt.Path {
		t.Fatalf("own worktree = %q, %v", dir, err)
	}
	if _, err := s.TaskDirectory(ctx, p.ID, "local", t.TempDir(), t.TempDir()); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("unrelated directory accepted: %v", err)
	}
}

func TestManagedTaskWorkspaceRejectsSymlinkReplacement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newService(t)
	p, err := s.Create(ctx, "Research", "")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "project-workspaces")
	dir, err := s.TaskDirectory(ctx, p.ID, "local", "", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), dir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := s.TaskDirectory(ctx, p.ID, "local", "", root); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("redirected workspace accepted: %v", err)
	}
}

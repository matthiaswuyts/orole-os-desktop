package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

func TestProjectsOverIPC(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	stop := start(t, dir)
	defer stop()
	ctx := context.Background()
	c := control.NewClient(dir, "test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	repo, _ := filepath.EvalSymlinks(t.TempDir())
	repo = filepath.Join(repo, "demo")
	copyEmptyRepo(t, repo)

	var pr struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.Call(ctx, "projects.add", map[string]any{"path": repo}, &pr); err != nil || pr.Name != "demo" {
		t.Fatalf("add: %+v %v", pr, err)
	}
	var e *ipc.Error
	if err := c.Call(ctx, "projects.add", map[string]any{"path": t.TempDir()}, nil); !errors.As(err, &e) || e.Code != ipc.CodeBadRequest {
		t.Fatalf("non-repo add: %v", err)
	}
	var wt struct {
		ID     string `json:"id"`
		Branch string `json:"branch"`
		Path   string `json:"path"`
	}
	if err := c.Call(ctx, "worktrees.create", map[string]any{"projectId": pr.ID, "name": "ipc test"}, &wt); err != nil || wt.Branch != "orole/ipc-test" {
		t.Fatalf("create: %+v %v", wt, err)
	}
	if err := c.Call(ctx, "worktrees.create", map[string]any{"projectId": pr.ID, "name": "x", "baseRef": "--detach"}, nil); !errors.As(err, &e) || e.Code != ipc.CodeBadRequest {
		t.Fatalf("option-injection baseRef: %v", err)
	}
	var wts []struct{ ID string }
	if err := c.Call(ctx, "worktrees.list", map[string]any{}, &wts); err != nil || len(wts) != 2 {
		t.Fatalf("list all: %+v %v", wts, err)
	}
	var st struct {
		Branch struct{ Head string } `json:"branch"`
	}
	if err := c.Call(ctx, "git.status", map[string]any{"worktreeId": wt.ID}, &st); err != nil || st.Branch.Head != "orole/ipc-test" {
		t.Fatalf("status: %+v %v", st, err)
	}
	if err := c.Call(ctx, "git.status", map[string]any{"projectId": "../../etc"}, nil); !errors.As(err, &e) || e.Code != ipc.CodeNotFound {
		t.Fatalf("unknown project: %v", err)
	}
	if err := c.Call(ctx, "git.diff", map[string]any{"projectId": pr.ID, "from": "-x"}, nil); !errors.As(err, &e) || e.Code != ipc.CodeBadRequest {
		t.Fatalf("diff injection: %v", err)
	}
	if err := c.Call(ctx, "worktrees.remove", map[string]any{"id": wt.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "projects.remove", map[string]any{"id": pr.ID}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCreateProjectsOverIPC(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	stop := start(t, dir)
	defer stop()
	ctx := context.Background()
	c := control.NewClient(dir, "test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	type project struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsGit bool   `json:"isGit"`
	}
	var named, plain project
	if err := c.Call(ctx, "projects.create", map[string]any{"name": "Notes", "path": ""}, &named); err != nil || named.Name != "Notes" || named.Path != "" || named.IsGit {
		t.Fatalf("folderless: %+v %v", named, err)
	}
	folder := t.TempDir()
	if err := c.Call(ctx, "projects.create", map[string]any{"name": "", "path": folder}, &plain); err != nil || plain.Name != filepath.Base(folder) || plain.IsGit {
		t.Fatalf("plain folder: %+v %v", plain, err)
	}
	var e *ipc.Error
	if err := c.Call(ctx, "projects.create", map[string]any{"name": "", "path": ""}, nil); !errors.As(err, &e) || e.Code != ipc.CodeBadRequest {
		t.Fatalf("blank create: %v", err)
	}
	for _, id := range []string{named.ID, plain.ID} {
		if err := c.Call(ctx, "git.status", map[string]any{"projectId": id}, nil); !errors.As(err, &e) || e.Code != ipc.CodeBadRequest {
			t.Fatalf("git.status on non-git project %s: %v", id, err)
		}
	}
	var wts []struct{ ID string }
	if err := c.Call(ctx, "worktrees.list", map[string]any{}, &wts); err != nil || len(wts) != 0 {
		t.Fatalf("worktrees of non-git projects: %+v %v", wts, err)
	}
}

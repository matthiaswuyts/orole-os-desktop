package daemon_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
	os.Mkdir(repo, 0o755)
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=T", "-c", "user.email=t@e.c", "commit", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}

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

package daemon_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
	"github.com/rayorole/orole-os-desktop/internal/projects"
	"github.com/rayorole/orole-os-desktop/internal/search"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func TestFilesNotificationsSearchIPC(t *testing.T) {
	dir := t.TempDir()
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := control.NewClient(dir, "files-behavior-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	call := func(method string, p, out any) {
		t.Helper()
		if err := c.Call(ctx, method, p, out); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	rootPath := t.TempDir()
	var root files.Root
	call("files.roots.add", map[string]any{"provider": "local", "path": rootPath}, &root)
	// Overlapping approvals must not duplicate filename search hits.
	subdirectory := filepath.Join(root.Path, "nested")
	call("files.mkdir", map[string]any{"provider": "local", "path": subdirectory}, nil)
	call("files.roots.add", map[string]any{"provider": "local", "path": subdirectory}, nil)
	path := filepath.Join(subdirectory, "orole-ipc-note.txt")
	var saved daemon.FileWriteResult
	call("files.write", map[string]any{"provider": "local", "path": path, "data": []byte("first"), "create": true}, &saved)
	if saved.Info == nil || saved.Conflict {
		t.Fatalf("write %+v", saved)
	}
	var read files.ReadResult
	call("files.read", map[string]any{"provider": "local", "path": path}, &read)
	if string(read.Data) != "first" {
		t.Fatalf("read %q", read.Data)
	}
	if err := os.WriteFile(path, []byte("outside edit"), 0600); err != nil {
		t.Fatal(err)
	}
	saved = daemon.FileWriteResult{}
	call("files.write", map[string]any{"provider": "local", "path": path, "data": []byte("mine"), "ifMatch": read.Info.Etag}, &saved)
	if !saved.Conflict || saved.Current == nil {
		t.Fatalf("conflict %+v", saved)
	}
	var hits []files.Entry
	call("files.search", map[string]any{"provider": "local", "query": "orole-ipc"}, &hits)
	if len(hits) != 1 {
		t.Fatalf("hits %+v", hits)
	}
	if err := c.Call(ctx, "files.read", map[string]any{"provider": "git", "path": path}, nil); err == nil {
		t.Fatal("permissions leaked between providers")
	}
	var n notifications.Notification
	call("notifications.notify", map[string]any{"source": "files-test", "title": "orole-ipc attention", "body": "message", "level": "warning"}, &n)
	var list daemon.NotificationList
	call("notifications.list", nil, &list)
	if len(list.Items) != 1 || list.Unread != 1 {
		t.Fatalf("notifications %+v", list)
	}
	var results search.Response
	call("search.query", map[string]any{"query": "orole-ipc", "providers": []string{"files:local", "notifications"}}, &results)
	if len(results.Results) != 2 {
		t.Fatalf("unified %+v", results)
	}
	call("notifications.markRead", map[string]any{"ids": []string{n.ID}}, nil)
	list = daemon.NotificationList{}
	call("notifications.list", nil, &list)
	if len(list.Items) != 1 || list.Unread != 0 {
		t.Fatalf("read %+v", list)
	}
	call("files.delete", map[string]any{"provider": "local", "path": path, "permanent": true}, nil)
	call("files.roots.remove", map[string]any{"provider": "local", "id": root.ID}, nil)
	if err := c.Call(ctx, "files.list", map[string]any{"provider": "local", "path": root.Path}, nil); err == nil {
		t.Fatal("removed root still accessible")
	}
}

func projectFileRepo(t *testing.T, path string) string {
	t.Helper()
	copyEmptyRepo(t, path)
	if err := os.WriteFile(filepath.Join(path, "note.txt"), []byte("repository note"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := files.CanonicalRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func projectDirectoryAlias(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("create junction: %v: %s", err, out)
		}
	} else if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestGitFileProjectRootRetargetDeniedOverIPC(t *testing.T) {
	dir := t.TempDir()
	base := t.TempDir()
	approved := projectFileRepo(t, filepath.Join(base, "repositories", "approved"))
	outside := projectFileRepo(t, filepath.Join(base, "outside"))
	alias := filepath.Join(base, "alias")
	projectDirectoryAlias(t, alias, filepath.Dir(approved))
	aliasRoot := filepath.Join(alias, filepath.Base(approved))
	ctx := context.Background()
	var project projects.Project
	replaced := false
	t.Cleanup(func() {
		if replaced {
			if err := os.Remove(approved); err != nil {
				t.Errorf("remove replacement alias: %v", err)
			}
		}
		if err := os.Remove(alias); err != nil {
			t.Errorf("remove ancestor alias: %v", err)
		}
	})

	// The second daemon must load exactly the same authority from disk.
	for _, phase := range []string{"registration", "restart"} {
		t.Run(phase, func(t *testing.T) {
			stop := start(t, dir)
			defer stop()
			c := control.NewClient(dir, "project-root-security-test")
			if err := c.Connect(ctx); err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if phase == "registration" {
				if err := c.Call(ctx, "projects.add", map[string]any{"path": aliasRoot}, &project); err != nil {
					t.Fatal(err)
				}
				if project.Path != approved {
					t.Fatalf("registration stored %q, want canonical approval %q", project.Path, approved)
				}
				for _, path := range []string{aliasRoot, approved} {
					var read files.ReadResult
					if err := c.Call(ctx, "files.read", map[string]any{"provider": "git", "path": filepath.Join(path, "note.txt")}, &read); err != nil || string(read.Data) != "repository note" {
						t.Fatalf("legitimate alias read %q: %+v %v", path, read, err)
					}
				}
				if err := os.Rename(approved, approved+"-moved"); err != nil {
					t.Fatal(err)
				}
				projectDirectoryAlias(t, approved, outside)
				replaced = true
			}
			var roots []files.Root
			if err := c.Call(ctx, "files.roots.list", map[string]any{"provider": "git"}, &roots); err != nil {
				t.Fatal(err)
			}
			if len(roots) != 1 || roots[0].Path != approved || !roots[0].Missing || roots[0].Problem == "" {
				t.Fatalf("retargeted approval must stay pinned and unhealthy: %+v", roots)
			}
			for _, path := range []string{aliasRoot, approved, outside} {
				for _, method := range []string{"files.read", "files.stat", "files.write"} {
					var ipcErr *ipc.Error
					err := c.Call(ctx, method, map[string]any{
						"provider": "git", "path": filepath.Join(path, "note.txt"),
						"data": []byte("unauthorized"), "create": true,
					}, nil)
					if !errors.As(err, &ipcErr) || ipcErr.Code != ipc.CodeDenied {
						t.Errorf("%s through %q must be denied: %v", method, path, err)
					}
				}
			}
		})
	}
}

func TestGitFileLegacyProjectAliasRequiresReapprovalOverIPC(t *testing.T) {
	dir := t.TempDir()
	base := t.TempDir()
	target := projectFileRepo(t, filepath.Join(base, "repository"))
	legacy := filepath.Join(base, "legacy-alias")
	projectDirectoryAlias(t, legacy, target)
	t.Cleanup(func() {
		if err := os.Remove(legacy); err != nil {
			t.Errorf("remove legacy alias: %v", err)
		}
	})

	// Preserve an old noncanonical registration exactly as stored. Upgrading
	// must not learn its current target and turn that target into authority.
	db, err := database.Open(filepath.Join(dir, "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	_, insertErr := db.Exec(`INSERT INTO projects(id,name,path) VALUES(?,?,?)`, "legacy", "legacy", legacy)
	closeErr := db.Close()
	if insertErr != nil {
		t.Fatal(insertErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	stop := start(t, dir)
	defer stop()
	ctx := context.Background()
	c := control.NewClient(dir, "legacy-project-root-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var roots []files.Root
	if err := c.Call(ctx, "files.roots.list", map[string]any{"provider": "git"}, &roots); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Path != legacy || !roots[0].Missing {
		t.Fatalf("legacy approval was silently canonicalized: %+v", roots)
	}
	for _, path := range []string{legacy, target} {
		var ipcErr *ipc.Error
		err := c.Call(ctx, "files.read", map[string]any{"provider": "git", "path": filepath.Join(path, "note.txt")}, nil)
		if !errors.As(err, &ipcErr) || ipcErr.Code != ipc.CodeDenied {
			t.Fatalf("legacy access through %q: %v", path, err)
		}
	}
	var registered []projects.Project
	if err := c.Call(ctx, "projects.list", nil, &registered); err != nil {
		t.Fatal(err)
	}
	if len(registered) != 1 || registered[0].Path != legacy {
		t.Fatalf("legacy stored path changed: %+v", registered)
	}
	if err := c.Call(ctx, "projects.remove", map[string]any{"id": "legacy"}, nil); err != nil {
		t.Fatal(err)
	}
	var approved projects.Project
	if err := c.Call(ctx, "projects.add", map[string]any{"path": legacy}, &approved); err != nil || approved.Path != target {
		t.Fatalf("explicit reapproval: %+v %v", approved, err)
	}
	var read files.ReadResult
	if err := c.Call(ctx, "files.read", map[string]any{"provider": "git", "path": filepath.Join(legacy, "note.txt")}, &read); err != nil || string(read.Data) != "repository note" {
		t.Fatalf("reapproved alias: %+v %v", read, err)
	}
}

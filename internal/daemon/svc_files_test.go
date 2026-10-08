package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
	"github.com/rayorole/orole-os-desktop/internal/search"
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
	call("notifications.list", map[string]any{"source": "files-test"}, &list)
	if len(list.Items) != 1 || list.Unread != 1 {
		t.Fatalf("notifications %+v", list)
	}
	var results search.Response
	call("search.query", map[string]any{"query": "orole-ipc", "providers": []string{"files:local", "notifications"}}, &results)
	if len(results.Results) != 2 {
		t.Fatalf("unified %+v", results)
	}
	call("notifications.markRead", map[string]any{"ids": []string{n.ID}}, nil)
	call("notifications.dismiss", map[string]any{"ids": []string{n.ID}}, nil)
	list = daemon.NotificationList{}
	call("notifications.list", nil, &list)
	if len(list.Items) != 0 || list.Unread != 0 {
		t.Fatalf("dismissed %+v", list)
	}
	call("files.delete", map[string]any{"provider": "local", "path": path, "permanent": true}, nil)
	call("files.roots.remove", map[string]any{"provider": "local", "id": root.ID}, nil)
	if err := c.Call(ctx, "files.list", map[string]any{"provider": "local", "path": root.Path}, nil); err == nil {
		t.Fatal("removed root still accessible")
	}
}

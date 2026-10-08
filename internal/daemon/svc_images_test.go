package daemon_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
)

func TestArtifactIPCServesOnlyTheSessionsOwnImages(t *testing.T) {
	dir := t.TempDir()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, id := range []string{"art-a", "art-b"} {
		if err := d.Agents.Store().SaveSession(agents.Session{ID: id, Kind: "codex", Machine: "local", Dir: t.TempDir(), Mode: agents.ModeStructured, Status: agents.StatusDone, StartedAt: now, UpdatedAt: now}, ""); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "agents", id, "images"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(dir, "agents", "art-a", "images", "one.png")
	foreign := filepath.Join(dir, "agents", "art-b", "images", "two.png")
	for _, p := range []string{own, foreign, filepath.Join(dir, "outside.png")} {
		if err := os.WriteFile(p, pngBuf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := control.NewClient(dir, "artifact-test")
	defer c.Close()
	for {
		if err = c.Connect(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(20 * time.Millisecond):
		}
	}

	var a agents.Artifact
	if err := c.Call(ctx, "agents.artifact", map[string]string{"id": "art-a", "path": own}, &a); err != nil || a.Mime != "image/png" || !bytes.Equal(a.Data, pngBuf.Bytes()) {
		t.Fatalf("own image: %+v %v", a, err)
	}
	sep := string(filepath.Separator)
	for name, p := range map[string]map[string]string{
		"traversal":       {"id": "art-a", "path": filepath.Join(dir, "agents", "art-a", "images", "..", "..", "..", "outside.png")},
		"relative":        {"id": "art-a", "path": ".." + sep + ".." + sep + "outside.png"},
		"other session":   {"id": "art-a", "path": foreign},
		"database":        {"id": "art-a", "path": filepath.Join(dir, "orole.db")},
		"unknown session": {"id": "nope", "path": own},
		"id traversal":    {"id": ".." + sep + "art-b", "path": foreign},
	} {
		var got agents.Artifact
		if err := c.Call(ctx, "agents.artifact", p, &got); err == nil {
			t.Errorf("%s: served %d bytes", name, len(got.Data))
		}
	}
	var s agents.Session
	if err := c.Call(ctx, "images.start", map[string]string{"prompt": "   "}, &s); err == nil {
		t.Error("blank image prompt accepted")
	}
}

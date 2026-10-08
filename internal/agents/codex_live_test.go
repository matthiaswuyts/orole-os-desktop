package agents_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/terminals"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// liveCodex runs the adapter against the installed, signed-in `codex`. It is opt-in
// (OROLE_LIVE_CODEX=1; OROLE_LIVE_CODEX_MODEL picks a model the account supports) because it
// spends real model and image-generation quota.
func liveCodex(t *testing.T) *env {
	t.Helper()
	if os.Getenv("OROLE_LIVE_CODEX") != "1" {
		t.Skip("set OROLE_LIVE_CODEX=1 to run against the installed codex")
	}
	data := t.TempDir()
	db, err := database.Open(filepath.Join(data, "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{db: db, st: agents.NewStore(db), rec: &recorder{}, data: data, pubs: &pubLog{}}
	e.term = terminals.NewManager(terminals.NewBackend(), nil)
	e.m = agents.NewManager(agents.Config{
		Store: e.st, Host: &agents.Host{Terminals: e.term, DataDir: data},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Authorize: func(context.Context, string, string, string) error { return nil },
		Adapters:  []agents.Adapter{agents.Codex()},
	})
	t.Cleanup(func() { e.m.Shutdown(); e.term.Shutdown() })
	return e
}

// approveAll answers permission requests until the session leaves the working states.
func (e *env) approveAll(t *testing.T, id string, until func(agents.Session) bool) agents.Session {
	t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		s, err := e.m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range s.Pending {
			t.Logf("approving %s: %s", p.Tool, p.Summary)
			if err := e.m.Approve(context.Background(), id, p.ID, true, ""); err != nil {
				t.Fatal(err)
			}
		}
		if until(s) {
			return s
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("timed out")
	return agents.Session{}
}

func TestLiveCodexEditsAndRunsCommands(t *testing.T) {
	e := liveCodex(t)
	dir := t.TempDir()
	s, err := e.m.Start(context.Background(), agents.StartSpec{Kind: "codex", Dir: dir, Model: os.Getenv("OROLE_LIVE_CODEX_MODEL"), Reasoning: "low",
		Prompt: "Use apply_patch to create hello.txt containing the single line hi, then run the shell command `echo done`. Reply with one word."})
	if err != nil {
		t.Fatal(err)
	}
	s = e.approveAll(t, s.ID, func(s agents.Session) bool { return s.Status == agents.StatusDone || s.Status == agents.StatusFailed })
	if s.Status != agents.StatusDone {
		t.Fatalf("session: %+v", s)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "hello.txt")); err != nil || !strings.Contains(string(b), "hi") {
		t.Fatalf("hello.txt: %q %v", b, err)
	}
	k := kinds(e.events(t, s.ID))
	t.Logf("events: %v", k)
	if k[agents.EvTool] < 2 || k[agents.EvToolResult] < 2 || k[agents.EvPermission] < 2 || k[agents.EvTurnDone] != 1 || s.AgentSessionID == "" {
		t.Fatalf("unexpected event mix: %v session=%+v", k, s)
	}
	// Steer-less follow-up and resume: stop, then prompt again through thread/resume.
	if _, err := e.m.Stop(context.Background(), s.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Prompt(context.Background(), s.ID, "What is the exact content of hello.txt? Answer with just the content. Read it with `type hello.txt`."); err != nil {
		t.Fatal(err)
	}
	s = e.approveAll(t, s.ID, func(s agents.Session) bool { return s.Status == agents.StatusDone && s.Turns >= 1 && s.Live })
	t.Logf("resumed answer: %q", s.LastMessage)
}

func TestLiveCodexGeneratesAnImage(t *testing.T) {
	e := liveCodex(t)
	s, err := e.m.StartImage(context.Background(), "a flat red circle on a white background, minimal")
	if err != nil {
		t.Fatal(err)
	}
	s = e.approveAll(t, s.ID, func(s agents.Session) bool { return s.Status == agents.StatusDone || s.Status == agents.StatusFailed })
	evs := e.events(t, s.ID)
	t.Logf("events: %v", kinds(evs))
	imgs := cxOfKind(evs, agents.EvImage)
	if len(imgs) != 1 {
		t.Fatalf("want one image event, got %d (session %+v)", len(imgs), s)
	}
	img := cxData[agents.ImageData](t, imgs[0])
	a, err := e.m.Artifact(s.ID, img.Path)
	if err != nil || a.Mime != "image/png" || len(a.Data) < 1000 {
		t.Fatalf("artifact: %s %d bytes %v", a.Mime, len(a.Data), err)
	}
	t.Logf("image %s, %d bytes, prompt %q", img.Mime, len(a.Data), img.Prompt)
}

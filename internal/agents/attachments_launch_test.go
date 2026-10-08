package agents_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/agents"
)

func TestAttachmentLaunchIdempotency(t *testing.T) {
	e := newEnv(t)
	ctx := ctx10(t)
	a, err := e.m.UploadAttachment(ctx, agents.UploadAttachmentSpec{Name: "input.txt", DataBase64: base64.StdEncoding.EncodeToString([]byte("private fixture"))})
	if err != nil {
		t.Fatal(err)
	}
	spec := agents.StartSpec{Kind: "claude", Dir: t.TempDir(), Prompt: "Read the attached input.", AttachmentIDs: []string{a.ID}, Key: "attachment-idempotency"}
	s, err := e.m.Start(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Task, a.ID) {
		t.Fatalf("session task lacks file handoff: %q", s.Task)
	}
	if err := e.m.DiscardAttachment(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := e.m.Start(ctx, spec)
	if err != nil || retried.ID != s.ID {
		t.Fatalf("retry resolved discarded staging instead of original session: %+v %v", retried, err)
	}
	dirs, err := filepath.Glob(filepath.Join(spec.Dir, ".orole-attachments-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("retry duplicated input files: %v %v", dirs, err)
	}
	if got, err := os.ReadFile(filepath.Join(dirs[0], "1-"+a.ID+".txt")); err != nil || string(got) != "private fixture" {
		t.Fatalf("input handoff: %q %v", got, err)
	}
}

func TestAttachmentUnknownRejectedBeforeSession(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: t.TempDir(), Prompt: "read", AttachmentIDs: []string{uuid.NewString()}}); err == nil {
		t.Fatal("unknown attachment started an agent")
	}
	if sessions, err := e.m.List(); err != nil || len(sessions) != 0 {
		t.Fatalf("invalid upload created session: %+v %v", sessions, err)
	}
}

func TestAttachmentFailedStartRemainsIdempotent(t *testing.T) {
	e := newEnv(t)
	ctx := ctx10(t)
	a, err := e.m.UploadAttachment(ctx, agents.UploadAttachmentSpec{Name: "input.txt", DataBase64: "eA=="})
	if err != nil {
		t.Fatal(err)
	}
	// References make this otherwise valid prompt exceed the launch bound after handoff.
	spec := agents.StartSpec{Kind: "claude", Dir: t.TempDir(), Prompt: strings.Repeat("x", 100<<10), AttachmentIDs: []string{a.ID}, Key: "failed-attachment-start"}
	failed, err := e.m.Start(ctx, spec)
	if err == nil || failed.ID == "" || failed.Status != agents.StatusFailed {
		t.Fatalf("expected recorded failed launch: %+v %v", failed, err)
	}
	retried, err := e.m.Start(ctx, spec)
	if err != nil || retried.ID != failed.ID || retried.Status != agents.StatusFailed {
		t.Fatalf("failed start retry created a new session: %+v %v", retried, err)
	}
	dirs, err := filepath.Glob(filepath.Join(spec.Dir, ".orole-attachments-*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("failed retry duplicated handoff: %v %v", dirs, err)
	}
}

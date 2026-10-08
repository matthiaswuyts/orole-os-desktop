package agents

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

func attachmentManager(t *testing.T) *Manager {
	t.Helper()
	return &Manager{cfg: Config{Host: &Host{DataDir: t.TempDir()}}}
}

func uploadForTest(t *testing.T, m *Manager, name string, data []byte) Attachment {
	t.Helper()
	a, err := m.UploadAttachment(context.Background(), UploadAttachmentSpec{Name: name, DataBase64: base64.StdEncoding.EncodeToString(data)})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAttachmentUploadSecurityAndLimits(t *testing.T) {
	m := attachmentManager(t)
	for _, name := range []string{"", ".", "..", "../secret", `C:\secret`, "a/b", "x\nignore", "x\x00", " name", "name.", "NUL", "com1.txt", strings.Repeat("x", 256), "x\u202ey"} {
		if _, err := m.UploadAttachment(context.Background(), UploadAttachmentSpec{Name: name, DataBase64: "eA=="}); err == nil {
			t.Errorf("accepted unsafe filename %q", name)
		}
	}
	for _, data := range []string{"not base64!", base64.StdEncoding.EncodeToString(make([]byte, MaxAttachmentBytes+1))} {
		if _, err := m.UploadAttachment(context.Background(), UploadAttachmentSpec{Name: "ok.bin", DataBase64: data}); err == nil {
			t.Error("accepted invalid or oversized bytes")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.UploadAttachment(ctx, UploadAttachmentSpec{Name: "ok", DataBase64: "eA=="}); err == nil {
		t.Fatal("cancelled upload succeeded")
	}
	max := UploadAttachmentSpec{Name: strings.Repeat("n", 255), DataBase64: base64.StdEncoding.EncodeToString(make([]byte, MaxAttachmentBytes))}
	frame, err := json.Marshal(map[string]any{"method": "agents.uploadAttachment", "params": max, "id": 1})
	if err != nil || len(frame)+1024 >= ipc.MaxMessageBytes {
		t.Fatalf("upload exceeds IPC budget: %d %v", len(frame), err)
	}
	if a, err := m.UploadAttachment(context.Background(), max); err != nil || a.Size != MaxAttachmentBytes {
		t.Fatalf("maximum upload: %+v %v", a, err)
	}
}

func TestAttachmentResolutionRejectsUnknownDuplicateAndExcess(t *testing.T) {
	m := attachmentManager(t)
	a := uploadForTest(t, m, "input.txt", []byte("exact input"))
	for _, ids := range [][]string{{"../secret"}, {uuid.NewString()}, {a.ID, a.ID}, {a.ID, a.ID, a.ID, a.ID, a.ID, a.ID, a.ID, a.ID, a.ID}} {
		if _, err := m.resolveAttachments(ids); err == nil {
			t.Errorf("accepted invalid IDs %v", ids)
		}
	}
	// A new manager resolves durable records without an in-memory upload registry.
	other := &Manager{cfg: m.cfg}
	if got, err := other.resolveAttachments([]string{a.ID}); err != nil || len(got) != 1 || got[0].Name != a.Name {
		t.Fatalf("durable resolution: %v %v", got, err)
	}
	if err := m.DiscardAttachment(context.Background(), "../secret"); err == nil {
		t.Fatal("discard accepted traversal")
	}
}

func TestAttachmentHandoffBytesIgnoredAndIndependent(t *testing.T) {
	m := attachmentManager(t)
	input := []byte{0, 1, 2, 255, '\n'}
	a := uploadForTest(t, m, "input.bin", input)
	staged, err := m.resolveAttachments([]string{a.ID})
	if err != nil {
		t.Fatal(err)
	}
	s := Session{ID: uuid.NewString(), Machine: "local", Dir: t.TempDir(), Task: "Read the input"}
	prompt, err := m.handoffAttachments(context.Background(), s, staged)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(s.Dir, ".orole-attachments-"+s.ID, "1-"+a.ID+".bin")
	encodedPath, _ := json.Marshal(file)
	if !strings.Contains(prompt, string(encodedPath)) || !strings.Contains(prompt, s.Task) {
		t.Fatalf("prompt does not reference input: %s", prompt)
	}
	if err := m.DiscardAttachment(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.DiscardAttachment(context.Background(), a.ID); err != nil {
		t.Fatalf("discard not idempotent: %v", err)
	}
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(got, input) {
		t.Fatalf("session input lost or changed after discard: %v %v", got, err)
	}
	if _, err := m.handoffAttachments(context.Background(), s, staged); err == nil {
		t.Fatal("handoff overwrote existing input directory")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable for ignore assertion")
	}
	for _, args := range [][]string{{"init"}, {"add", "--all"}, {"status", "--porcelain", "--untracked-files=all"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = s.Dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
		if args[0] == "status" && len(bytes.TrimSpace(out)) != 0 {
			t.Fatalf("attachment would be published: %s", out)
		}
	}
}

func TestAttachmentStorageRejectsSymlinkEscape(t *testing.T) {
	m := attachmentManager(t)
	a := uploadForTest(t, m, "input.txt", []byte("safe"))
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("leak"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(m.cfg.Host.DataDir, "attachment-uploads", a.ID, "data")
	if err := os.Remove(data); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, data); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := m.resolveAttachments([]string{a.ID}); err == nil {
		t.Fatal("attachment resolution followed an escaping symlink")
	}
}

func TestAttachmentExpiryRemovesOnlyStaging(t *testing.T) {
	m := attachmentManager(t)
	a := uploadForTest(t, m, "input.txt", []byte("safe"))
	root, err := m.attachmentRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := expireAttachments(root, time.Now().Add(attachmentTTL+time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.resolveAttachments([]string{a.ID}); err == nil {
		t.Fatal("expired upload remained available")
	}
}

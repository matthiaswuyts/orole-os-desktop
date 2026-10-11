package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (m *Manager) runCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.runs)
}

func blobCount(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(dir, "chat", "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

func TestAttachmentUploadCommitAndSniff(t *testing.T) {
	e := newEnv(t, nil)
	data := bytes.Repeat([]byte("hello world\n"), 100)
	a, err := e.upload("notes.txt", data, 100)
	if err != nil {
		t.Fatal(err)
	}
	if a.Size != int64(len(data)) || !strings.HasPrefix(a.MIME, "text/plain") || a.Name != "notes.txt" {
		t.Fatalf("attachment: %+v", a)
	}
	if blobCount(t, e.dir) != 1 {
		t.Fatal("blob missing")
	}
	if ents, _ := os.ReadDir(filepath.Join(e.dir, "chat", "uploads")); len(ents) != 0 {
		t.Fatalf("staging must be cleaned after commit: %v", ents)
	}
	got, err := e.m.ReadAttachment(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(got.DataBase64)
	if !bytes.Equal(raw, data) || got.MIME != a.MIME {
		t.Fatal("read must return the stored bytes")
	}

	// The type comes from the content: an executable named .png is refused, a PNG named .dat accepted.
	if _, err := e.upload("evil.png", []byte("MZ\x90\x00\x03\x00\x00\x00binary\x00\x01"), 64); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "not a supported file type") {
		t.Fatalf("binary: %v", err)
	}
	png, err := e.upload("picture.dat", pngBytes(t), 64)
	if err != nil || png.MIME != "image/png" {
		t.Fatalf("png: %+v %v", png, err)
	}
	pdf, err := e.upload("doc.bin", []byte("%PDF-1.4 hi"), 64)
	if err != nil || pdf.MIME != "application/pdf" {
		t.Fatalf("pdf: %+v %v", pdf, err)
	}
}

func TestAttachmentsAreDeduplicatedByContent(t *testing.T) {
	e := newEnv(t, nil)
	a, _ := e.upload("a.txt", []byte("same bytes"), 4)
	b, err := e.upload("b.txt", []byte("same bytes"), 4)
	if err != nil || a.ID == b.ID {
		t.Fatalf("second upload: %+v %v", b, err)
	}
	if blobCount(t, e.dir) != 1 {
		t.Fatalf("identical content must share one blob, have %d", blobCount(t, e.dir))
	}
	// Deleting one reference keeps the blob; deleting the last removes it.
	ctx := context.Background()
	if err := e.m.DiscardAttachment(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if blobCount(t, e.dir) != 1 {
		t.Fatal("blob still referenced")
	}
	if err := e.m.DiscardAttachment(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if blobCount(t, e.dir) != 0 {
		t.Fatal("unreferenced blob must be removed")
	}
	if _, err := e.m.ReadAttachment(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read after discard: %v", err)
	}
}

func TestAttachmentProtocolErrors(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	for name, req := range map[string]AttachmentBegin{
		"traversal": {Name: "../x.txt", Size: 1},
		"reserved":  {Name: "NUL.txt", Size: 1},
		"empty":     {Name: "a.txt", Size: 0},
		"too large": {Name: "a.txt", Size: MaxAttachmentBytes + 1},
		"no name":   {Name: "", Size: 1},
	} {
		if _, err := e.m.BeginAttachment(req); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	up, err := e.m.BeginAttachment(AttachmentBegin{Name: "a.txt", Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	chunk := func(seq int, data string) error {
		return e.m.AttachmentChunk(AttachmentChunk{ID: up.ID, Seq: seq, DataBase64: base64.StdEncoding.EncodeToString([]byte(data))})
	}
	if err := chunk(1, "abc"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("out of order: %v", err)
	}
	if err := e.m.AttachmentChunk(AttachmentChunk{ID: up.ID, Seq: 0, DataBase64: "!!!"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad base64: %v", err)
	}
	if err := chunk(0, "abcde"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.CommitAttachment(ctx, AttachmentCommit{ID: up.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("incomplete commit: %v", err)
	}
	if err := chunk(1, "abcdefgh"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("overrun: %v", err)
	}
	if err := chunk(1, "fghij"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.AttachmentChunk(AttachmentChunk{ID: up.ID, Seq: 2, DataBase64: base64.StdEncoding.EncodeToString(make([]byte, AttachmentChunkMax+1))}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized chunk: %v", err)
	}
	if err := e.m.AttachmentChunk(AttachmentChunk{ID: "nope", Seq: 0, DataBase64: "AAAA"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown upload: %v", err)
	}
	if _, err := e.m.CommitAttachment(ctx, AttachmentCommit{ID: up.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.CommitAttachment(ctx, AttachmentCommit{ID: up.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double commit: %v", err)
	}
}

func TestDiscardRemovesStagedUpload(t *testing.T) {
	e := newEnv(t, nil)
	up, _ := e.m.BeginAttachment(AttachmentBegin{Name: "a.txt", Size: 4})
	_ = e.m.AttachmentChunk(AttachmentChunk{ID: up.ID, Seq: 0, DataBase64: "YWJj"})
	if err := e.m.DiscardAttachment(context.Background(), up.ID); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(filepath.Join(e.dir, "chat", "uploads")); len(ents) != 0 {
		t.Fatalf("staging left behind: %v", ents)
	}
	if err := e.m.AttachmentChunk(AttachmentChunk{ID: up.ID, Seq: 1, DataBase64: "ZA=="}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("chunk after discard: %v", err)
	}
}

func TestSentAttachmentsCannotBeDiscardedOrShared(t *testing.T) {
	e := newEnv(t, nil)
	a := e.bot("A")
	b := e.bot("B")
	att, _ := e.upload("f.txt", []byte("data"), 10)
	e.send(a.ID, "here", att.ID)
	e.idle(a.ID)
	if err := e.m.DiscardAttachment(context.Background(), att.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("discard after send: %v", err)
	}
	if _, err := e.m.Send(context.Background(), SendRequest{BotID: b.ID, Text: "mine", AttachmentIDs: []string{att.ID}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("another bot must not reuse the attachment: %v", err)
	}
	parts := e.history(a.ID)[0].Parts
	if len(parts) != 2 || parts[1].Type != PartAttachment || parts[1].AttachmentID != att.ID || parts[1].Name != "f.txt" {
		t.Fatalf("user parts: %+v", parts)
	}
	// Clearing the conversation removes the attachment and its blob.
	if err := e.m.Clear(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if blobCount(t, e.dir) != 0 {
		t.Fatal("blob must be removed with the conversation")
	}
}

func TestStagedAndUnsentAttachmentsExpire(t *testing.T) {
	e := newEnv(t, nil)
	staged, _ := e.m.BeginAttachment(AttachmentBegin{Name: "s.txt", Size: 4})
	_ = e.m.AttachmentChunk(AttachmentChunk{ID: staged.ID, Seq: 0, DataBase64: "YWJjZA=="})
	unsent, err := e.upload("u.txt", []byte("unsent"), 10)
	if err != nil {
		t.Fatal(err)
	}
	bot := e.bot("Keeper")
	sent, _ := e.upload("k.txt", []byte("sent"), 10)
	e.send(bot.ID, "keep", sent.ID)
	e.idle(bot.ID)

	e.advance(AttachmentTTLHours*time.Hour - time.Minute)
	if err := e.m.up.sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.ReadAttachment(context.Background(), unsent.ID); err != nil {
		t.Fatal("not yet expired")
	}
	e.advance(2 * time.Minute)
	if err := e.m.up.sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := e.m.AttachmentChunk(AttachmentChunk{ID: staged.ID, Seq: 1, DataBase64: "ZQ=="}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("staged upload must expire: %v", err)
	}
	if _, err := e.m.ReadAttachment(context.Background(), unsent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unsent attachment must expire: %v", err)
	}
	if _, err := e.m.ReadAttachment(context.Background(), sent.ID); err != nil {
		t.Fatalf("sent attachments never expire: %v", err)
	}
	if blobCount(t, e.dir) != 1 {
		t.Fatalf("only the sent blob remains, have %d", blobCount(t, e.dir))
	}
	if ents, _ := os.ReadDir(filepath.Join(e.dir, "chat", "uploads")); len(ents) != 0 {
		t.Fatalf("expired staging must be removed: %v", ents)
	}
}

func TestStagingIsRemovedOnCloseAndRestart(t *testing.T) {
	e := newEnv(t, nil)
	up, _ := e.m.BeginAttachment(AttachmentBegin{Name: "s.txt", Size: 4})
	_ = e.m.AttachmentChunk(AttachmentChunk{ID: up.ID, Seq: 0, DataBase64: "YWJj"})
	e.m.Close()
	if ents, _ := os.ReadDir(filepath.Join(e.dir, "chat", "uploads")); len(ents) != 0 {
		t.Fatalf("staging left after Close: %v", ents)
	}
	if err := os.MkdirAll(filepath.Join(e.dir, "chat", "uploads", "orphan"), 0o700); err != nil {
		t.Fatal(err)
	}
	m2, err := New(Config{DB: e.db.DB, DataDir: e.dir, LLM: e.llm})
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	if ents, _ := os.ReadDir(filepath.Join(e.dir, "chat", "uploads")); len(ents) != 0 {
		t.Fatalf("orphaned staging must be wiped at start: %v", ents)
	}
}

func TestAttachmentStoreCap(t *testing.T) {
	e := newEnv(t, nil)
	// Pretend the store is nearly full: one big blob row. It is stamped with the fake clock's "now"
	// so the Manager's background sweeper (which runs once at start-up, concurrently with the test)
	// never treats it as an expired, never-sent upload and deletes it.
	if _, err := e.db.Exec(`INSERT INTO chat_attachments(id, sha256, name, mime, size, created_at) VALUES ('big', ?, 'big.bin', 'text/plain', ?, ?)`,
		strings.Repeat("a", 64), maxStoreBytes-3, e.clock().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.upload("a.txt", []byte("12345"), 10); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "store is full") {
		t.Fatalf("cap: %v", err)
	}
	if _, err := e.upload("b.txt", []byte("12"), 10); err != nil {
		t.Fatalf("a file that still fits: %v", err)
	}
}

func TestReadRefusesLargePreviews(t *testing.T) {
	e := newEnv(t, nil)
	data := bytes.Repeat([]byte("x"), maxPreviewBytes+1)
	a, err := e.upload("big.txt", data, AttachmentChunkMax)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.ReadAttachment(context.Background(), a.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("large preview: %v", err)
	}
}

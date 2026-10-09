package chat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/chat/extract"
)

const (
	maxStagedUploads    = 64
	maxStoreBytes       = 2 << 30 // total attachment blobs on disk
	maxPreviewBytes     = 4 << 20
	uploadsDir          = "uploads"
	blobsDir            = "attachments"
	stagedFile          = "data"
	attachmentTTL       = AttachmentTTLHours * time.Hour
	unsupportedTypeHint = "Attach images (PNG, JPEG, GIF, WebP), PDFs or text files."
)

// uploads stages chunked uploads under <DataDir>/chat/uploads/<id>/ and stores committed files as
// content-addressed blobs at <DataDir>/chat/attachments/<sha256>. All file access goes through an
// os.Root on <DataDir>/chat, so names can never escape it or follow links out of it.
type uploads struct {
	store *store
	now   func() time.Time
	root  *os.Root

	mu     sync.Mutex // guards staged and serialises blob writes with blob garbage collection
	staged map[string]*stagedUpload
}

type stagedUpload struct {
	name    string
	size    int64
	got     int64
	next    int
	created time.Time
}

func newUploads(dataDir string, st *store, now func() time.Time) (*uploads, error) {
	dir := filepath.Join(dataDir, "chat")
	for _, sub := range []string{uploadsDir, blobsDir} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, fmt.Errorf("chat storage: %w", err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("chat storage: %w", err)
	}
	u := &uploads{store: st, now: now, root: root, staged: map[string]*stagedUpload{}}
	u.wipeStaging() // staged uploads do not survive a restart
	return u, nil
}

// wipeStaging removes every staged upload directory.
func (u *uploads) wipeStaging() {
	d, err := u.root.Open(uploadsDir)
	if err != nil {
		return
	}
	names, _ := d.Readdirnames(-1)
	d.Close()
	for _, n := range names {
		_ = u.root.RemoveAll(filepath.Join(uploadsDir, n))
	}
}

func (u *uploads) close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	clear(u.staged)
	u.wipeStaging()
	u.root.Close()
}

// validAttachmentName mirrors the coding-agent attachment rules: a plain, portable file name.
func validAttachmentName(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.TrimSpace(name) != name || strings.HasSuffix(name, ".") || strings.ContainsAny(name, `/\:<>"|?*`) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	stem, _, _ := strings.Cut(name, ".")
	switch strings.ToUpper(stem) {
	case "", "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

func stagedPath(id string) string { return filepath.Join(uploadsDir, id, stagedFile) }

func (u *uploads) begin(req AttachmentBegin) (AttachmentUpload, error) {
	if !validAttachmentName(req.Name) {
		return AttachmentUpload{}, invalidf("The file name is not valid. Use a plain file name of at most 255 characters.")
	}
	if req.Size <= 0 {
		return AttachmentUpload{}, invalidf("The file is empty.")
	}
	if req.Size > MaxAttachmentBytes {
		return AttachmentUpload{}, invalidf("Files are limited to %d MiB.", MaxAttachmentBytes>>20)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.expireStagedLocked()
	if len(u.staged) >= maxStagedUploads {
		return AttachmentUpload{}, busyf("Too many uploads are in progress. Try again in a moment.")
	}
	id := uuid.NewString()
	if err := u.root.Mkdir(filepath.Join(uploadsDir, id), 0o700); err != nil {
		return AttachmentUpload{}, err
	}
	u.staged[id] = &stagedUpload{name: req.Name, size: req.Size, created: u.now()}
	return AttachmentUpload{ID: id}, nil
}

func (u *uploads) expireStagedLocked() {
	cutoff := u.now().Add(-attachmentTTL)
	for id, s := range u.staged {
		if s.created.Before(cutoff) {
			_ = u.root.RemoveAll(filepath.Join(uploadsDir, id))
			delete(u.staged, id)
		}
	}
}

func (u *uploads) chunk(req AttachmentChunk) error {
	if len(req.DataBase64) > base64.StdEncoding.EncodedLen(AttachmentChunkMax) {
		return invalidf("Upload chunks are limited to %d KiB.", AttachmentChunkMax>>10)
	}
	data, err := base64.StdEncoding.DecodeString(req.DataBase64)
	if err != nil {
		return invalidf("The upload data is not valid base64.")
	}
	if len(data) == 0 {
		return invalidf("The upload chunk is empty.")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	s, ok := u.staged[req.ID]
	if !ok {
		return notFoundf("This upload does not exist or has expired.")
	}
	if req.Seq != s.next {
		return invalidf("Upload chunk %d is out of order (expected %d).", req.Seq, s.next)
	}
	if s.got+int64(len(data)) > s.size {
		return invalidf("The upload is larger than the announced %d bytes.", s.size)
	}
	f, err := u.root.OpenFile(stagedPath(req.ID), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	s.got += int64(len(data))
	s.next++
	return nil
}

func (u *uploads) discardStaged(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.staged, id)
	_ = u.root.RemoveAll(filepath.Join(uploadsDir, id))
}

// commit sniffs the staged file from its content, hashes it, stores the blob (deduplicated) and
// records the attachment row. The attachment ID equals the upload ID.
func (u *uploads) commit(ctx context.Context, req AttachmentCommit) (Attachment, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	s, ok := u.staged[req.ID]
	if !ok {
		return Attachment{}, notFoundf("This upload does not exist or has expired.")
	}
	if s.got != s.size {
		return Attachment{}, invalidf("The upload is incomplete (%d of %d bytes received).", s.got, s.size)
	}
	f, err := u.root.Open(stagedPath(req.ID))
	if err != nil {
		return Attachment{}, err
	}
	defer f.Close()
	head := make([]byte, min(s.size, extract.MaxTextBytes+1))
	if _, err := io.ReadFull(f, head); err != nil {
		return Attachment{}, err
	}
	mime, kind := extract.Sniff(s.name, head)
	if kind == extract.KindUnsupported {
		delete(u.staged, req.ID)
		_ = u.root.RemoveAll(filepath.Join(uploadsDir, req.ID))
		return Attachment{}, invalidf("%q is not a supported file type (detected %s). %s", s.name, mime, unsupportedTypeHint)
	}
	h := sha256.New()
	h.Write(head)
	if _, err := io.Copy(h, f); err != nil {
		return Attachment{}, err
	}
	sha := hex.EncodeToString(h.Sum(nil))

	var existing int64
	err = u.store.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(size), 0) FROM chat_attachments WHERE sha256=?`, sha).Scan(&existing)
	if err != nil {
		return Attachment{}, err
	}
	blob := filepath.Join(blobsDir, sha)
	if existing == 0 {
		var total int64
		err := u.store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0) FROM (SELECT MAX(size) AS size FROM chat_attachments GROUP BY sha256)`).Scan(&total)
		if err != nil {
			return Attachment{}, err
		}
		if total+s.size > maxStoreBytes {
			return Attachment{}, invalidf("The chat attachment store is full (%d GiB). Clear a conversation or remove old attachments to free space.", maxStoreBytes>>30)
		}
	}
	f.Close()
	if _, err := u.root.Lstat(blob); err == nil {
		_ = u.root.RemoveAll(filepath.Join(uploadsDir, req.ID))
	} else {
		if err := u.root.Rename(stagedPath(req.ID), blob); err != nil {
			return Attachment{}, err
		}
		_ = u.root.RemoveAll(filepath.Join(uploadsDir, req.ID))
	}
	a := Attachment{ID: req.ID, Name: s.name, MIME: mime, Size: s.size}
	_, err = u.store.db.ExecContext(ctx, `INSERT INTO chat_attachments(id, sha256, name, mime, size, bot_id, created_at) VALUES (?,?,?,?,?,NULL,?)`,
		a.ID, sha, a.Name, a.MIME, a.Size, u.now().UnixMilli())
	if err != nil {
		return Attachment{}, err
	}
	delete(u.staged, req.ID)
	return a, nil
}

// storedAttachment is a chat_attachments row.
type storedAttachment struct {
	Attachment
	SHA   string
	BotID string
}

func (u *uploads) get(ctx context.Context, id string) (storedAttachment, error) {
	var a storedAttachment
	var bot sql.NullString
	err := u.store.db.QueryRowContext(ctx, `SELECT id, sha256, name, mime, size, bot_id FROM chat_attachments WHERE id=?`, id).Scan(&a.ID, &a.SHA, &a.Name, &a.MIME, &a.Size, &bot)
	if errors.Is(err, sql.ErrNoRows) {
		return a, notFoundf("Attachment not found.")
	}
	a.BotID = bot.String
	return a, err
}

func validSHA(sha string) bool {
	if len(sha) != 64 {
		return false
	}
	for _, c := range sha {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// readBlob reads a stored attachment, never more than limit bytes (limit <= 0: the whole file).
func (u *uploads) readBlob(sha string, limit int64) ([]byte, error) {
	if !validSHA(sha) {
		return nil, errors.New("invalid attachment hash")
	}
	f, err := u.root.Open(filepath.Join(blobsDir, sha))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, notFoundf("The stored file is missing.")
		}
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if limit > 0 {
		r = io.LimitReader(f, limit)
	}
	return io.ReadAll(r)
}

func (u *uploads) read(ctx context.Context, id string) (AttachmentData, error) {
	a, err := u.get(ctx, id)
	if err != nil {
		return AttachmentData{}, err
	}
	if a.Size > maxPreviewBytes {
		return AttachmentData{}, invalidf("This file is larger than %d MiB and cannot be previewed.", maxPreviewBytes>>20)
	}
	data, err := u.readBlob(a.SHA, maxPreviewBytes)
	if err != nil {
		return AttachmentData{}, err
	}
	return AttachmentData{Attachment: a.Attachment, DataBase64: base64.StdEncoding.EncodeToString(data)}, nil
}

// discard drops a staged upload, or a committed attachment that was never sent.
func (u *uploads) discard(ctx context.Context, id string) error {
	u.mu.Lock()
	if _, ok := u.staged[id]; ok {
		delete(u.staged, id)
		_ = u.root.RemoveAll(filepath.Join(uploadsDir, id))
		u.mu.Unlock()
		return nil
	}
	u.mu.Unlock()
	a, err := u.get(ctx, id)
	if err != nil {
		return err
	}
	if a.BotID != "" {
		return invalidf("This attachment was already sent and cannot be discarded.")
	}
	res, err := u.store.db.ExecContext(ctx, `DELETE FROM chat_attachments WHERE id=? AND bot_id IS NULL
		AND NOT EXISTS (SELECT 1 FROM knowledge_files k WHERE k.attachment_id = chat_attachments.id)`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		u.gcBlobs([]string{a.SHA})
	}
	return nil
}

// deleteAttachmentRows deletes rows matching where (never files kept as knowledge) and returns the
// blob hashes that may now be unreferenced.
func deleteAttachmentRows(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, sha256 FROM chat_attachments WHERE (`+where+`)
		AND id NOT IN (SELECT attachment_id FROM knowledge_files)`, args...)
	if err != nil {
		return nil, err
	}
	var ids, shas []string
	for rows.Next() {
		var id, sha string
		if err := rows.Scan(&id, &sha); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		shas = append(shas, sha)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chat_attachments WHERE id=?`, id); err != nil {
			return nil, err
		}
	}
	return shas, nil
}

// gcBlobs removes blobs that no attachment row references any more.
func (u *uploads) gcBlobs(shas []string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	seen := make(map[string]bool, len(shas))
	for _, sha := range shas {
		if seen[sha] || !validSHA(sha) {
			continue
		}
		seen[sha] = true
		var n int
		if err := u.store.db.QueryRow(`SELECT COUNT(*) FROM chat_attachments WHERE sha256=?`, sha).Scan(&n); err != nil || n > 0 {
			continue
		}
		_ = u.root.Remove(filepath.Join(blobsDir, sha))
	}
}

// sweep expires stale staged uploads and committed attachments that were never sent.
func (u *uploads) sweep(ctx context.Context) error {
	u.mu.Lock()
	u.expireStagedLocked()
	u.mu.Unlock()
	cutoff := u.now().Add(-attachmentTTL).UnixMilli()
	tx, err := u.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	shas, err := deleteAttachmentRows(ctx, tx, `bot_id IS NULL AND created_at < ?`, cutoff)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	u.gcBlobs(shas)
	return nil
}

// BeginAttachment starts a chunked upload (chat.attachments.begin).
func (m *Manager) BeginAttachment(req AttachmentBegin) (AttachmentUpload, error) {
	return m.up.begin(req)
}

// AttachmentChunk appends the next sequential chunk of an upload.
func (m *Manager) AttachmentChunk(req AttachmentChunk) error { return m.up.chunk(req) }

// CommitAttachment sniffs, hashes and stores a finished upload.
func (m *Manager) CommitAttachment(ctx context.Context, req AttachmentCommit) (Attachment, error) {
	return m.up.commit(ctx, req)
}

// DiscardAttachment drops a staged upload or an attachment that was never sent.
func (m *Manager) DiscardAttachment(ctx context.Context, id string) error {
	return m.up.discard(ctx, id)
}

// ReadAttachment returns the bytes of an attachment for previews (at most 4 MiB).
func (m *Manager) ReadAttachment(ctx context.Context, id string) (AttachmentData, error) {
	return m.up.read(ctx, id)
}

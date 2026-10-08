package agents

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const taskTextPreviewBytes = 64 << 10

// TaskFilePreview contains inert bytes from a session's working directory. Data
// is omitted above the IPC budget; Text is a bounded UTF-8 preview, never HTML.
type TaskFilePreview struct {
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	Mime      string  `json:"mime"`
	Size      int64   `json:"size"`
	Text      *string `json:"text,omitempty"`
	Data      []byte  `json:"data,omitempty"`
	Truncated bool    `json:"truncated"`
}

// TaskFile reads through an OS-enforced root, including symlink confinement.
// The renderer cannot use a harness-provided path to read outside the workspace.
func (m *Manager) TaskFile(ctx context.Context, id, path string) (TaskFilePreview, error) {
	if err := ctx.Err(); err != nil {
		return TaskFilePreview{}, err
	}
	s, err := m.Get(id)
	if err != nil {
		return TaskFilePreview{}, err
	}
	if !localMachine(s.Machine) {
		return TaskFilePreview{}, errRemoteOperation
	}
	if s.Dir == "" || path == "" || len(path) > 32768 || strings.ContainsRune(path, 0) {
		return TaskFilePreview{}, errors.New("a workspace file path is required")
	}
	if filepath.IsAbs(path) {
		path, err = filepath.Rel(s.Dir, path)
		if err != nil {
			return TaskFilePreview{}, errors.New("file is outside this task's workspace")
		}
	}
	// Reject alternate data streams as well as drive-relative/device paths.
	if !filepath.IsLocal(path) || strings.Contains(path, ":") {
		return TaskFilePreview{}, errors.New("file is outside this task's workspace")
	}
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return TaskFilePreview{}, errors.New("task workspace is unavailable")
	}
	defer root.Close()
	f, err := root.Open(path)
	if err != nil {
		return TaskFilePreview{}, errors.New("file is unavailable or outside this task's workspace")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return TaskFilePreview{}, errors.New("only regular workspace files can be previewed")
	}
	limit := int64(MaxAttachmentBytes)
	if info.Size() > limit {
		limit = taskTextPreviewBytes
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return TaskFilePreview{}, errors.New("file could not be read")
	}
	if err := ctx.Err(); err != nil {
		return TaskFilePreview{}, err
	}
	out := TaskFilePreview{
		Name: info.Name(), Path: filepath.ToSlash(filepath.Clean(path)),
		Mime: http.DetectContentType(data), Size: info.Size(),
	}
	complete := int64(len(data)) <= limit && info.Size() <= limit
	if complete {
		out.Data = data
	}
	out.Truncated = !complete
	if len(data) > taskTextPreviewBytes {
		data = data[:taskTextPreviewBytes]
		out.Truncated = true
		// The byte boundary may split one UTF-8 code point, not the text itself.
		for len(data) > 0 && !utf8.RuneStart(data[len(data)-1]) {
			data = data[:len(data)-1]
		}
		if len(data) > 0 && !utf8.Valid(data[len(data)-1:]) {
			data = data[:len(data)-1]
		}
	}
	if utf8.Valid(data) && !bytes.ContainsRune(data, 0) && strings.HasPrefix(out.Mime, "text/") {
		text := string(data)
		out.Text = &text
	}
	return out, nil
}

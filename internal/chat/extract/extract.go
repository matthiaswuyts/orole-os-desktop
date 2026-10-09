// Package extract classifies chat attachments from their bytes (never from the extension alone)
// and extracts text from text-like files.
package extract

import (
	"bytes"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"
)

// Kind is how the engine treats an attachment.
type Kind string

const (
	KindImage       Kind = "image"
	KindPDF         Kind = "pdf"
	KindText        Kind = "text"
	KindUnsupported Kind = "unsupported"
)

// MaxTextBytes caps text injected into a prompt or indexed as knowledge per file.
const MaxTextBytes = 1 << 20

var textExt = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true, ".json": true, ".jsonl": true,
	".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".xml": true, ".html": true, ".htm": true, ".css": true,
	".js": true, ".mjs": true, ".ts": true, ".tsx": true, ".jsx": true, ".go": true, ".py": true, ".rs": true,
	".java": true, ".kt": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true, ".cs": true,
	".rb": true, ".php": true, ".sh": true, ".ps1": true, ".bat": true, ".sql": true, ".log": true, ".tex": true,
	".swift": true, ".lua": true, ".r": true, ".env": false,
}

// Sniff returns the detected media type and kind. Images must be PNG, JPEG, GIF or WebP by magic bytes.
func Sniff(name string, data []byte) (mime string, kind Kind) {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	detected := http.DetectContentType(head)
	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return "application/pdf", KindPDF
	case detected == "image/png" || detected == "image/jpeg" || detected == "image/gif" || detected == "image/webp":
		return detected, KindImage
	}
	ext := strings.ToLower(path.Ext(name))
	if textExt[ext] || strings.HasPrefix(detected, "text/") {
		if _, ok := Text(data); ok {
			if textExt[ext] {
				return "text/plain; charset=utf-8", KindText
			}
			return detected, KindText
		}
	}
	return detected, KindUnsupported
}

// Text returns data as text when it is valid UTF-8 without NUL bytes, truncated to MaxTextBytes
// at a rune boundary. ok is false for binary content.
func Text(data []byte) (text string, ok bool) {
	probe := data
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		return "", false
	}
	if len(data) > MaxTextBytes {
		data = data[:MaxTextBytes]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) {
		return "", false
	}
	return strings.TrimPrefix(string(data), "\ufeff"), true
}

package drive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func previewJSON(mime, text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"fileId": "f1", "mimeType": mime, "text": text})
	return raw
}
func TestTextPreviewKindsAndLimits(t *testing.T) {
	for _, tc := range []struct{ mime, text, kind string }{{"text/plain", "hello", "text"}, {"text/markdown", "# Heading", "markdown"}, {"application/json", `{"ok":true}`, "json"}, {"text/csv", "name,value\nx,2", "csv"}} {
		p, err := parsePreview(previewJSON(tc.mime, tc.text), "f1")
		if err != nil || p.Kind != tc.kind || p.Text != tc.text || p.DataURL != "" {
			t.Fatalf("preview: %+v %v", p, err)
		}
	}
	p, err := parsePreview(previewJSON("text/plain", strings.Repeat("x", MaxPreviewText+100)+"\u202e"), "f1")
	if err != nil || !p.Truncated || len(p.Text) > MaxPreviewText+4 || strings.ContainsRune(p.Text, '\u202e') {
		t.Fatalf("limit: %d %+v %v", len(p.Text), p.Truncated, err)
	}
	for _, raw := range []json.RawMessage{
		previewJSON("text/html", "<script>x()</script>"), previewJSON("image/svg+xml", "<svg/>"), previewJSON("application/pdf", "PDF"), previewJSON("application/json", "not json"),
		json.RawMessage(`{"mimeType":"image/png","url":"http://127.0.0.1/secret"}`), json.RawMessage(`{"mimeType":"text/plain","url":"https://example.test/content"}`), json.RawMessage(`{"mimeType":"text/plain","text":{}}`), json.RawMessage(`{"fileId":"other","mimeType":"text/plain","text":"x"}`),
	} {
		if _, err := parsePreview(raw, "f1"); err == nil {
			t.Fatalf("unsafe preview accepted: %s", raw)
		}
	}
}

func TestImagePreviewReencodesInlineRaster(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	data := append(buf.Bytes(), []byte("<script>trailing payload</script>")...)
	raw, _ := json.Marshal(map[string]any{"mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(data)})
	p, err := parsePreview(raw, "f1")
	if err != nil || p.Kind != "image" || !strings.HasPrefix(p.DataURL, "data:image/png;base64,") {
		t.Fatalf("image: %+v %v", p, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(p.DataURL, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(decoded, []byte("script")) {
		t.Fatal("image was not re-encoded")
	}
	if _, err := png.Decode(bytes.NewReader(decoded)); err != nil {
		t.Fatal(err)
	}
	for _, value := range []map[string]any{
		{"mimeType": "image/jpeg", "data": base64.StdEncoding.EncodeToString(buf.Bytes())},
		{"mimeType": "image/png", "data": "not-base64"},
		{"mimeType": "image/png", "data": strings.Repeat("A", base64.StdEncoding.EncodedLen(MaxImageBytes)+4)},
	} {
		raw, _ := json.Marshal(value)
		if _, err := parsePreview(raw, "f1"); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
	var huge bytes.Buffer
	if err := png.Encode(&huge, image.NewGray(image.Rect(0, 0, 2001, 2000))); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(huge.Bytes())})
	if _, err := parsePreview(raw, "f1"); err == nil {
		t.Fatal("pixel limit ignored")
	}
}

func TestFileValidationAndSanitation(t *testing.T) {
	f, err := parseFile(json.RawMessage(`{"id":"f1","name":"a\u202eb","mimeType":"text/plain","parents":["root"],"size":"12","modifiedTime":"today\u202e"}`))
	if err != nil || f.Name != "ab" || f.ParentID != "root" || f.Size != 12 || f.ModifiedTime != "today" {
		t.Fatalf("file: %+v %v", f, err)
	}
	for _, raw := range []string{`{}`, `{"id":"x","name":"file"}`, `{"id":"x","name":"file","mimeType":"text/plain","size":-1}`, `{"id":"x","name":"file","mimeType":"text/plain","parents":[4]}`, `{"id":"x","name":"file","mimeType":"text/plain","parentId":"bad\n"}`} {
		if _, err := parseFile(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid file accepted: %s", raw)
		}
	}
}

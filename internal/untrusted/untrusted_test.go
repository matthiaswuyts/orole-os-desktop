package untrusted

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTextAndLine(t *testing.T) {
	raw := "Subject\x1b[2J\u202eexe.txt\u200b\x00\nHello\tworld"
	text := Text(raw, 0)
	if strings.ContainsAny(text, "\x1b\u202e\u200b\x00") || !strings.Contains(text, "\nHello\tworld") {
		t.Fatalf("text %q", text)
	}
	if strings.ContainsAny(Line(raw, 0), "\n\t") {
		t.Fatal("line retained separators")
	}
	if got := Text("éééé", 3); got != "é…" {
		t.Fatalf("rune truncation %q", got)
	}
}

func TestJSONDecodesEscapedControlsWithoutLosingNumbers(t *testing.T) {
	raw := json.RawMessage(`{"text":"ok\u202e\u0007", "nested":["a\u200b"], "id":9007199254740993, "safe\u202e":"drop"}`)
	out := JSON(raw, 1024)
	if !json.Valid(out) || strings.Contains(string(out), `\u0007`) || strings.ContainsAny(string(out), "\u202e\u200b") || !strings.Contains(string(out), "9007199254740993") {
		t.Fatalf("sanitised JSON: %s", out)
	}
	var v map[string]any
	_ = json.Unmarshal(out, &v)
	if v["text"] != "ok" || len(v) != 3 {
		t.Fatalf("unexpected %v", v)
	}
	if JSON(raw, 2) != nil || JSON(json.RawMessage(`bad`), 50) != nil {
		t.Fatal("invalid or oversized JSON accepted")
	}
}

// Package untrusted neutralises text that originates outside the app (MCP tool
// descriptions and results, mail, file names) before it reaches the UI, audit log
// or an approval prompt. It never makes content "safe to follow": it only removes
// characters that can hide, reorder or forge what the user sees.
package untrusted

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text removes control characters (keeping \n and \t), bidirectional override and
// isolate marks, zero-width and other invisible format characters, invalid UTF-8,
// and truncates to max bytes (on a rune boundary, with an ellipsis).
func Text(s string, max int) string { return clean(s, max, true) }

// Line is Text for single-line labels: newlines and tabs become spaces.
func Line(s string, max int) string { return clean(s, max, false) }

func clean(s string, max int, multiline bool) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	var b strings.Builder
	b.Grow(min(len(s), max+4))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			if multiline {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		case r == '\r':
			if !multiline {
				b.WriteByte(' ')
			}
		case unicode.IsControl(r):
		case isInvisible(r):
		default:
			b.WriteRune(r)
		}
		if max > 0 && b.Len() > max {
			break
		}
	}
	out := b.String()
	if !multiline {
		out = strings.TrimSpace(out)
	}
	if max > 0 && len(out) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		out = out[:cut] + "…"
	}
	return out
}

func isInvisible(r rune) bool {
	switch {
	case r >= 0x200B && r <= 0x200F: // zero width space/joiners, LRM/RLM
		return true
	case r >= 0x202A && r <= 0x202E: // bidi embeddings/overrides
		return true
	case r >= 0x2060 && r <= 0x206F: // word joiner, invisible operators, isolates
		return true
	case r == 0xFEFF, r == 0x00AD, r == 0x061C, r == 0x180E:
		return true
	case r >= 0xE0000 && r <= 0xE007F: // tag characters
		return true
	case r >= 0xFFF9 && r <= 0xFFFB: // interlinear annotation
		return true
	}
	return false
}

// JSON neutralises decoded string values as well as literal characters in JSON.
// Unsafe object keys are discarded rather than renamed onto a trusted field.
// Numbers retain their original precision. Invalid or oversized documents fail closed.
func JSON(raw json.RawMessage, limit int) json.RawMessage {
	if len(raw) == 0 || len(raw) > limit {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if dec.Decode(&value) != nil {
		return nil
	}
	var cleanValue func(any, int) any
	cleanValue = func(v any, depth int) any {
		if depth > 64 {
			return nil
		}
		switch x := v.(type) {
		case string:
			return Text(x, limit)
		case []any:
			for i := range x {
				x[i] = cleanValue(x[i], depth+1)
			}
		case map[string]any:
			for key, item := range x {
				if Line(key, 0) != key {
					delete(x, key)
				} else {
					x[key] = cleanValue(item, depth+1)
				}
			}
		}
		return v
	}
	out, err := json.Marshal(cleanValue(value, 0))
	if err != nil || len(out) > limit {
		return nil
	}
	return out
}

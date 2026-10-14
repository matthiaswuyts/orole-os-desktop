package jarvis

import (
	"strings"
	"unicode"
)

// All limits are in runes (characters), not bytes.
const (
	maxOpener = 240
	maxName   = 80
)

// normalize trims and collapses every whitespace run to a single space.
func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }

// runesCut keeps at most n runes of s.
func runesCut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func equalFoldRune(a, b rune) bool {
	return a == b || unicode.ToLower(a) == unicode.ToLower(b) || unicode.ToUpper(a) == unicode.ToUpper(b)
}

// nameSpans returns the rune ranges [start, end) where name occurs in text, case-insensitively,
// with no letter or digit directly before or after the match.
func nameSpans(name, text []rune) [][2]int {
	n := len(name)
	if n == 0 {
		return nil
	}
	var spans [][2]int
	for i := 0; i+n <= len(text); i++ {
		match := true
		for j := 0; j < n && match; j++ {
			match = equalFoldRune(text[i+j], name[j])
		}
		if !match || (i > 0 && isWordRune(text[i-1])) || (i+n < len(text) && isWordRune(text[i+n])) {
			continue
		}
		spans = append(spans, [2]int{i, i + n})
		i += n - 1
	}
	return spans
}

// CheckOpener accepts a model-written first line only if it names the bot as a whole word, never
// names the app outside the bot's own name, and is one short paragraph of at most maxOpener runes.
func CheckOpener(name, text string) (string, bool) {
	raw := strings.TrimSpace(text)
	if raw == "" || strings.ContainsAny(raw, "\r\n  ") {
		return "", false
	}
	nr := []rune(normalize(name))
	if len(nr) == 0 || len(nr) > maxName {
		return "", false
	}
	text = normalize(raw)
	tr := []rune(text)
	if len(tr) > maxOpener {
		return "", false
	}
	spans := nameSpans(nr, tr)
	if len(spans) == 0 {
		return "", false
	}
	// Blank out the bot's own name, then look for the app name in what remains.
	rest := append([]rune(nil), tr...)
	for _, sp := range spans {
		for k := sp[0]; k < sp[1]; k++ {
			rest[k] = ' '
		}
	}
	if strings.Contains(strings.ToLower(string(rest)), "orole") {
		return "", false
	}
	return text, true
}

// FallbackOpener is used when the model's opener is late or fails CheckOpener: a name-only greeting in the call
// language (lang, see PhoneSettings.CallLanguage). An opener never says why the bot is calling, because the call may
// have reached voicemail; the reason comes in the first model turn after the owner answers. For any name CheckOpener
// accepts (1 to maxName runes), the result passes CheckOpener with that name.
func FallbackOpener(lang, name string, seed int) string { return fallbackOpener(lang, name, seed) }

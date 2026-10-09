package memory

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Write safety. Memory entries and proposed skills end up in later prompts, so anything a bot or a
// background job writes is screened for prompt-injection phrasing, credentials, shell backdoors
// and invisible text before it is stored. This is a tripwire, not a proof of safety: untrusted
// provenance (trust labels, fences, never promoting untrusted facts to core) is the real boundary.

var errUnsafe = errors.New("unsafe content")

type pattern struct {
	re     *regexp.Regexp
	reason string
}

func p(reason, expr string) pattern {
	return pattern{regexp.MustCompile(`(?is)` + expr), reason}
}

var unsafePatterns = []pattern{
	p("looks like an instruction override", `\b(ignore|disregard|forget|override|bypass)\b[^.\n]{0,30}\b(instructions?|prompts?|guidelines|directives|system message)\b`),
	p("looks like an instruction override", `\b(ignore|disregard|forget|override)\b[^.\n]{0,20}\b(previous|prior|above|earlier|all)\b[^.\n]{0,20}\b(rules|messages|context)\b`),
	p("looks like a prompt-injection attempt", `\b(reveal|print|show|repeat|output|leak)\b[^.\n]{0,30}\b(system|hidden|initial)\b[^.\n]{0,15}\b(prompt|instructions?|message)\b`),
	p("looks like a prompt-injection attempt", `\b(do not|don't|never)\b[^.\n]{0,20}\b(tell|inform|notify|reveal to)\b[^.\n]{0,20}\bthe user\b`),
	p("looks like a prompt-injection attempt", `\byou are now\b|\bnew instructions?\s*:|\bfrom now on,? (you|always|ignore)\b|<\s*/?\s*(system|assistant|untrusted)\b|\[\s*/?\s*(inst|system)\s*\]`),
	// Credentials and secrets.
	p("contains what looks like an API key or token", `\bsk-(or-v1-|ant-|proj-)?[A-Za-z0-9_-]{20,}`),
	p("contains what looks like an API key or token", `\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{30,}|\bgithub_pat_[A-Za-z0-9_]{20,}`),
	p("contains what looks like an API key or token", `\bAKIA[0-9A-Z]{16}\b|\bAIza[0-9A-Za-z_-]{35}\b|\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	p("contains what looks like an API key or token", `\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}`),
	p("contains what looks like an API key or token", `\bbearer\s+[A-Za-z0-9._~+/=-]{20,}`),
	p("contains a private key", `-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	p("contains what looks like a credential", `\b(api[_ -]?key|secret|password|passwd|passphrase|access[_ -]?token|auth[_ -]?token)\b\s*(is\s*)?[:=]\s*\S{6,}`),
	p("contains a URL with embedded credentials", `[a-z][a-z0-9+.-]*://[^\s/:@]+:[^\s/@]+@`),
	// Shell backdoors and exfiltration.
	p("contains a download-and-execute command", `\b(curl|wget|iwr|invoke-webrequest)\b[^\n|]*\|\s*(sudo\s+)?(sh|bash|zsh|pwsh|powershell|iex|python\d?)\b`),
	p("contains an encoded-command execution", `\b(powershell|pwsh)\b[^\n]*\s-(e|enc|encodedcommand)\b|\bbase64\s+(-d|--decode)\b[^\n|]*\|\s*(sh|bash)\b|\binvoke-expression\b|\biex\s*\(`),
	p("contains a reverse shell or backdoor", `/dev/(tcp|udp)/|\bnc(at)?\b[^\n]*\s-e\b|\bbash\s+-i\b|\bauthorized_keys\b|\bchmod\s+[0-7]*[+]?s\b`),
	p("contains a destructive command", `\brm\s+-[a-z]*r[a-z]*f?[a-z]*\s+(/|~|\*)(\s|$)|\bformat\s+[a-z]:|\bdel\s+/[a-z]*\s+/[a-z]*\s+[a-z]:\\`),
}

// checkUnsafe returns a user-facing reason when text must not be stored. When strict is false only
// invisible-character checks run (used for text the user typed themselves).
func checkUnsafe(text string, strict bool) error {
	if !utf8.ValidString(text) {
		return unsafef("text is not valid UTF-8")
	}
	for _, r := range text {
		if isHiddenRune(r) {
			return unsafef("contains invisible or direction-control characters (U+%04X)", r)
		}
	}
	if !strict {
		return nil
	}
	for _, pt := range unsafePatterns {
		if pt.re.MatchString(text) {
			return unsafef("%s", pt.reason)
		}
	}
	return nil
}

func unsafef(format string, a ...any) error {
	return &safetyError{msg: fmt.Sprintf(format, a...)}
}

type safetyError struct{ msg string }

func (e *safetyError) Error() string { return "rejected: " + e.msg }
func (e *safetyError) Unwrap() error { return errUnsafe }

// isHiddenRune reports characters that can hide or reorder text: control characters other than
// newline and tab, and format characters (zero-width, bidi, tag characters) except the joiners
// emoji sequences need.
func isHiddenRune(r rune) bool {
	switch {
	case r == '\n' || r == '\t' || r == '\r':
		return false
	case r == 0x200C || r == 0x200D: // ZWNJ/ZWJ: emoji and Indic scripts
		return false
	case r == 0x2028 || r == 0x2029:
		return true
	case unicode.IsControl(r):
		return true
	case unicode.Is(unicode.Cf, r):
		return true
	case r >= 0xE0000 && r <= 0xE007F:
		return true
	case unicode.Is(unicode.Co, r):
		return true
	}
	return false
}

// normText is the comparison form for duplicate detection: case-folded, punctuation and
// whitespace collapsed.
func normText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// collapseSpace trims the text and folds CRLF; it keeps interior newlines.
func collapseSpace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(s)
}

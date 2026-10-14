package jarvis

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxOpener = 240
	maxName   = 64
	maxReason = 160
)

var appName = regexp.MustCompile(`(?i)orole`)

// CheckOpener accepts a model-written first line only if it names the bot, never names the app, and is one short paragraph.
func CheckOpener(name, text string) (string, bool) {
	name = strings.TrimSpace(name)
	text = strings.TrimSpace(text)
	if name == "" || len(name) > maxName || text == "" || len(text) > maxOpener || strings.ContainsAny(text, "\r\n") || appName.MatchString(text) {
		return "", false
	}
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
	if !re.MatchString(text) {
		return "", false
	}
	return text, true
}

var fallbacks = []string{
	"%s here, quick one: %s.",
	"Hey, it's %s. %s.",
	"Hi, %s calling. %s.",
	"%s here. Heads up: %s.",
	"It's %s. %s.",
	"Hey, %s here. %s.",
}

// cutRunes shortens s to at most max bytes without splitting a UTF-8 sequence.
func cutRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	n := max
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// FallbackOpener is used when the model's opener is late or fails CheckOpener.
// The result always passes CheckOpener for the (possibly shortened) name it was given.
func FallbackOpener(name, reason string, seed int) string {
	name = cutRunes(strings.Join(strings.Fields(name), " "), maxName)
	reason = strings.TrimSuffix(strings.Join(strings.Fields(reason), " "), ".")
	if reason == "" {
		reason = "I have an update for you"
	}
	reason = appName.ReplaceAllString(reason, "the app")
	f := fallbacks[((seed%len(fallbacks))+len(fallbacks))%len(fallbacks)]
	// Budget the reason so the whole line fits maxOpener.
	overhead := len(fmt.Sprintf(f, name, ""))
	budget := min(maxReason, maxOpener-overhead)
	reason = strings.TrimSpace(cutRunes(reason, budget))
	if strings.HasPrefix(f, "%s") {
		return fmt.Sprintf(f, name, reason)
	}
	r, size := utf8.DecodeRuneInString(reason)
	if size > 0 {
		reason = string(unicode.ToUpper(r)) + reason[size:]
	}
	return fmt.Sprintf(f, name, reason)
}

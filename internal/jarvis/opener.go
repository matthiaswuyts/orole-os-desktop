package jarvis

import (
	"fmt"
	"regexp"
	"strings"
)

const maxOpener = 240

var appName = regexp.MustCompile(`(?i)orole`)

// CheckOpener accepts a model-written first line only if it names the bot, never names the app, and is one short paragraph.
func CheckOpener(name, text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxOpener || strings.Contains(text, "\n") || appName.MatchString(text) {
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

// FallbackOpener is used when the model's opener is late or fails CheckOpener.
func FallbackOpener(name, reason string, seed int) string {
	reason = strings.TrimSuffix(strings.TrimSpace(reason), ".")
	if reason == "" {
		reason = "I have an update for you"
	}
	if len(reason) > 160 {
		reason = reason[:160]
	}
	reason = appName.ReplaceAllString(reason, "the app")
	f := fallbacks[((seed%len(fallbacks))+len(fallbacks))%len(fallbacks)]
	if strings.HasPrefix(f, "%s") {
		return fmt.Sprintf(f, name, reason)
	}
	return fmt.Sprintf(f, name, strings.ToUpper(reason[:1])+reason[1:])
}

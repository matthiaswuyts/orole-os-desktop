package applications

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

var ErrDeepLink = errors.New("applications: unsupported deep link; use orole://open/<module>")

// InternalView is deliberately independent of user-configured executable shortcuts.
// New modules must be built in the React registry before adding them here.
func InternalView(view string) bool {
	switch view {
	case "home", "agents", "review", "worktrees", "automations", "mail", "drive", "github", "projects", "terminals", "applications", "browser", "files", "settings":
		return true
	default:
		return false
	}
}

// ParseDeepLink accepts only one canonical syntax and an exact built-in module.
// No queries, fragments, credentials, ports, encoded paths, files, or executable IDs.
func ParseDeepLink(raw string) (string, error) {
	if len(raw) > 128 || strings.ContainsFunc(raw, unicode.IsControl) {
		return "", ErrDeepLink
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "orole" || u.Host != "open" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.RawPath != "" {
		return "", ErrDeepLink
	}
	view := strings.TrimPrefix(u.Path, "/")
	if !InternalView(view) || raw != "orole://open/"+view {
		return "", ErrDeepLink
	}
	return view, nil
}

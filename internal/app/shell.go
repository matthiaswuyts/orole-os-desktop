package app

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/egoist/mygo"
)

// Shell exposes only explicitly permitted URL schemes, never arbitrary commands or filesystem paths.
type Shell struct{}

func (Shell) OpenExternal(raw string) error {
	if err := validateExternalURL(raw); err != nil {
		return err
	}
	return mygo.Shell.OpenExternal(raw)
}
func validateExternalURL(raw string) error {
	if len(raw) > 8192 || strings.ContainsFunc(raw, unicode.IsControl) {
		return fmt.Errorf("invalid external URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid external URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("HTTP URL requires a host and cannot contain credentials")
		}
	case "mailto":
		if u.Opaque == "" && u.Path == "" {
			return fmt.Errorf("mailto URL requires a recipient")
		}
	default:
		return fmt.Errorf("only http, https, and mailto URLs may open externally")
	}
	return nil
}

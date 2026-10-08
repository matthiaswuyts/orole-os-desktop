// Package paths resolves Orole OS's per-user data locations.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// DataDir returns the per-user data directory, creating it (0700).
// OROLE_HOME overrides it for development and tests.
func DataDir() (string, error) {
	dir := os.Getenv("OROLE_HOME")
	if dir == "" {
		switch runtime.GOOS {
		case "windows":
			base := os.Getenv("LOCALAPPDATA")
			if base == "" {
				h, err := os.UserHomeDir()
				if err != nil {
					return "", err
				}
				base = filepath.Join(h, "AppData", "Local")
			}
			dir = filepath.Join(base, "Orole")
		default:
			base, err := os.UserConfigDir()
			if err != nil {
				return "", err
			}
			dir = filepath.Join(base, "orole")
		}
	}
	// Children may start in another working directory; pass an absolute home
	// so the database, token file and pipe always identify the same instance.
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	dir = absolute
	return dir, os.MkdirAll(dir, 0o700)
}

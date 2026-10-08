package files

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Path policy. All paths reaching a provider are untrusted. A path is accepted
// only when it is absolute, free of ".." components and platform-reserved
// names, and its fully resolved location (symlinks, junctions, short names)
// lies inside an approved root whose own resolved location is unchanged since
// approval.

// validateRaw checks the lexical shape of an absolute path and returns it
// cleaned. It performs no filesystem access.
func validateRaw(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidPath)
	}
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%w: NUL in path", ErrInvalidPath)
	}
	if err := platformRejectPrefix(p); err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: path must be absolute", ErrInvalidPath)
	}
	vol := filepath.VolumeName(p)
	rest := p[len(vol):]
	for _, c := range splitComponents(rest) {
		if c == ".." {
			return "", fmt.Errorf("%w: '..' is not allowed", ErrInvalidPath)
		}
	}
	clean := filepath.Clean(p)
	for _, c := range splitComponents(clean[len(filepath.VolumeName(clean)):]) {
		if err := ValidateName(c); err != nil {
			return "", err
		}
	}
	return clean, nil
}

// ValidateName checks a single path component (a file or directory name).
func ValidateName(name string) error {
	switch name {
	case "", ".", "..":
		return fmt.Errorf("%w: bad name %q", ErrInvalidPath, name)
	}
	if strings.ContainsAny(name, "/\\") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: name %q contains a separator", ErrInvalidPath, name)
	}
	if len(name) > 255 {
		return fmt.Errorf("%w: name too long", ErrInvalidPath)
	}
	return platformCheckName(name)
}

func splitComponents(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' })
}

// within reports whether p equals root or lies below it (both cleaned).
func within(root, p string) bool {
	r, q := foldPath(root), foldPath(p)
	if q == r {
		return true
	}
	if !strings.HasSuffix(r, string(filepath.Separator)) {
		r += string(filepath.Separator)
	}
	return strings.HasPrefix(q, r)
}

func samePath(a, b string) bool { return foldPath(a) == foldPath(b) }

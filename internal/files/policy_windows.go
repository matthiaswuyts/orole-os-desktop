package files

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// foldPath makes comparisons case-insensitive like NTFS.
func foldPath(s string) string { return strings.ToLower(s) }

// platformRejectPrefix refuses Win32 device/extended namespaces: \\?\ and \\.\
// bypass normalisation (and reach raw devices), so no root can legitimately be
// addressed through them.
func platformRejectPrefix(p string) error {
	q := strings.ReplaceAll(p, "/", `\`)
	if strings.HasPrefix(q, `\\?\`) || strings.HasPrefix(q, `\\.\`) || strings.HasPrefix(q, `\??\`) {
		return fmt.Errorf("%w: device and extended-length paths are not allowed", ErrInvalidPath)
	}
	return nil
}

var reservedDevice = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	"COM\u00b9": true, "COM\u00b2": true, "COM\u00b3": true, "LPT\u00b9": true, "LPT\u00b2": true, "LPT\u00b3": true,
}

// platformCheckName rejects names Windows treats specially: reserved device
// names (with or without extension), trailing dots/spaces, alternate data
// streams and characters invalid in file names.
func platformCheckName(name string) error {
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: invalid encoding", ErrInvalidPath)
	}
	for _, r := range name {
		if r < 0x20 || strings.ContainsRune(`<>:"|?*`, r) {
			return fmt.Errorf("%w: %q contains a character Windows does not allow", ErrInvalidPath, name)
		}
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("%w: %q ends with a dot or space", ErrInvalidPath, name)
	}
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	stem = strings.TrimRight(stem, " ")
	if reservedDevice[strings.ToUpper(stem)] {
		return fmt.Errorf("%w: %q is a reserved device name", ErrInvalidPath, name)
	}
	return nil
}

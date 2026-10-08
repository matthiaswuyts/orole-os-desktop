//go:build !windows

package files

// foldPath: case-sensitive filesystems compare exactly.
func foldPath(s string) string { return s }

func platformRejectPrefix(string) error { return nil }

func platformCheckName(string) error { return nil }

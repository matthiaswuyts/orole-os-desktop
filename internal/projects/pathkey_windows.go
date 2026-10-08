package projects

import "strings"

// pathKey is the comparison form of a path: Windows paths are case-insensitive.
func pathKey(p string) string { return strings.ToLower(p) }

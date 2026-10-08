package applications

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Deny UNC, device namespaces, drive-relative paths, and alternate data streams.
func localPath(path string) bool {
	volume := filepath.VolumeName(path)
	return len(volume) == 2 && volume[1] == ':' &&
		((volume[0] >= 'A' && volume[0] <= 'Z') || (volume[0] >= 'a' && volume[0] <= 'z')) &&
		filepath.IsAbs(path) && !strings.Contains(path[2:], ":")
}

func nativeExecutable(path string, _ fs.FileMode) bool {
	return localPath(path) && strings.EqualFold(filepath.Ext(path), ".exe")
}

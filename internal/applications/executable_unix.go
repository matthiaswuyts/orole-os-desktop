//go:build !windows

package applications

import (
	"io/fs"
	"path/filepath"
)

func localPath(path string) bool { return filepath.IsAbs(path) }
func nativeExecutable(path string, mode fs.FileMode) bool {
	return localPath(path) && mode&0111 != 0
}

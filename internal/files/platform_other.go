//go:build !windows

package files

import (
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
)

func platformHidden(fs.FileInfo) bool { return false }

func hideWindow(*exec.Cmd) {}

// recycle: there is no portable trash here; callers must delete permanently.
func recycle(real string) error {
	return fmt.Errorf("%w: no Recycle Bin on this platform", ErrNoRecycleBin)
}

// realPath resolves symlinks of an existing path.
func realPath(p string) (string, error) { return filepath.EvalSymlinks(p) }

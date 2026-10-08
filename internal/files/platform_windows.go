package files

import (
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const fileAttributeHidden = 0x2

func platformHidden(fi fs.FileInfo) bool {
	if a, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes&fileAttributeHidden != 0
	}
	return false
}

// hideWindow keeps spawned console programs (git) from flashing a window.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

const driveFixed = 3

// filepathVolumeRoot returns "C:\" for a drive path, "" for UNC paths.
func filepathVolumeRoot(p string) string {
	v := filepath.VolumeName(p)
	if len(v) != 2 || v[1] != ':' {
		return ""
	}
	return v + `\`
}

// realPath returns the fully resolved location of an existing path. Unlike
// filepath.EvalSymlinks it also resolves directory junctions (mount points),
// which Go treats as irregular files and leaves in place: the handle-based
// GetFinalPathNameByHandle sees through every reparse point and 8.3 name.
func realPath(p string) (string, error) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(u, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", &fs.PathError{Op: "resolve", Path: p, Err: err}
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0) // FILE_NAME_NORMALIZED | VOLUME_NAME_DOS
		if err != nil {
			return "", &fs.PathError{Op: "resolve", Path: p, Err: err}
		}
		if int(n) < len(buf) {
			s := windows.UTF16ToString(buf[:n])
			switch {
			case strings.HasPrefix(s, `\\?\UNC\`):
				s = `\\` + s[len(`\\?\UNC\`):]
			case strings.HasPrefix(s, `\\?\`):
				s = s[len(`\\?\`):]
			}
			return filepath.Clean(s), nil
		}
		buf = make([]uint16, n+1)
	}
}

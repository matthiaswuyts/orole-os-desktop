package dictation

import (
	"errors"
	"syscall"
)

const (
	errorHandleDiskFull syscall.Errno = 39  // ERROR_HANDLE_DISK_FULL
	errorDiskFull       syscall.Errno = 112 // ERROR_DISK_FULL
)

// isDiskFull reports whether err means the volume has no space left.
func isDiskFull(err error) bool {
	return errors.Is(err, errorDiskFull) || errors.Is(err, errorHandleDiskFull) || errors.Is(err, syscall.ENOSPC)
}

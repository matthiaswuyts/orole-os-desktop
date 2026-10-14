//go:build !windows

package dictation

import (
	"errors"
	"syscall"
)

// isDiskFull reports whether err means the volume has no space left.
func isDiskFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }

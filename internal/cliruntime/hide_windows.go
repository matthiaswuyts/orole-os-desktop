package cliruntime

import (
	"os/exec"
	"syscall"
)

// HideWindow keeps a console window from flashing up for a background CLI call.
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

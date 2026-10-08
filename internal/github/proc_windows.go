//go:build windows

package github

import (
	"os/exec"
	"syscall"
)

// hideWindow stops a console window flashing when the daemon spawns gh/git.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

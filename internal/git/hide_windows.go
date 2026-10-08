package git

import (
	"os/exec"
	"syscall"
)

// hideWindow stops a console window flashing when the GUI/daemon spawns git.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

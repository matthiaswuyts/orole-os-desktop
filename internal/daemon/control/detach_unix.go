//go:build !windows

package control

import (
	"os"
	"os/exec"
	"syscall"
)

func exeSuffix() string { return "" }

// StartDetached launches the daemon in its own session.
func StartDetached(exe, dataDir string) error {
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "OROLE_HOME="+dataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

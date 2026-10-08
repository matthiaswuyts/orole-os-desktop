//go:build windows

package control

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	detachedProcess = 0x00000008
	createNoWindow  = 0x08000000
	newProcessGroup = 0x00000200
)

func exeSuffix() string { return ".exe" }

// StartDetached launches the daemon so it outlives the caller and shows no console.
func StartDetached(exe, dataDir string) error {
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "OROLE_HOME="+dataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNoWindow | newProcessGroup, HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

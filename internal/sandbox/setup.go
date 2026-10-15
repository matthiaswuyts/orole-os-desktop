package sandbox

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/setupcode"
)

// SetupResult is what turning on Windows Hypervisor Platform did.
type SetupResult struct {
	RebootRequired bool `json:"rebootRequired"`
}

var (
	ErrSetupDeclined      = errors.New("sandbox: Windows did not get permission to turn on Windows Hypervisor Platform")
	ErrSetupUnsupported   = errors.New("sandbox: setup is only available on Windows")
	ErrSetupHelperMissing = errors.New("sandbox: orole-sandbox-setup.exe is missing next to the daemon; reinstall Orole")
)

// SetupHelperPath is where the installer puts orole-sandbox-setup.exe: next to orole-daemon.exe.
func SetupHelperPath(daemonExe string) string {
	return filepath.Join(filepath.Dir(daemonExe), "orole-sandbox-setup.exe")
}

func setupOutcome(code uint32) (SetupResult, error) {
	switch code {
	case setupcode.OK:
		return SetupResult{}, nil
	case setupcode.RebootRequired:
		return SetupResult{RebootRequired: true}, nil
	case setupcode.NotElevated:
		return SetupResult{}, errors.New("sandbox: the setup helper did not run as administrator")
	default:
		return SetupResult{}, fmt.Errorf("sandbox: turning on Windows Hypervisor Platform failed (code %d); run `dism /online /enable-feature /featurename:HypervisorPlatform /all` in an administrator terminal to see why", code)
	}
}

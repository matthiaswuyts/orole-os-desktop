package sandbox

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/setupcode"
)

func TestSetupOutcomeMapsHelperExitCodes(t *testing.T) {
	if r, err := setupOutcome(setupcode.OK); err != nil || r.RebootRequired {
		t.Fatalf("OK: %+v %v", r, err)
	}
	if r, err := setupOutcome(setupcode.RebootRequired); err != nil || !r.RebootRequired {
		t.Fatalf("3010: %+v %v", r, err)
	}
	if _, err := setupOutcome(setupcode.NotElevated); err == nil || !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("740: %v", err)
	}
	if _, err := setupOutcome(setupcode.Failed); err == nil || !strings.Contains(err.Error(), "dism") {
		t.Fatalf("1: %v", err)
	}
}

func TestSetupHelperSitsNextToTheDaemon(t *testing.T) {
	exe := filepath.Join("C:", "Program Files", "Orole OS", "resources", "orole-daemon.exe")
	if got := SetupHelperPath(exe); got != filepath.Join(filepath.Dir(exe), "orole-sandbox-setup.exe") {
		t.Fatal(got)
	}
}

package platform

import (
	"strings"
	"testing"
)

func TestSystemdUnit(t *testing.T) {
	u := systemdUnit("/opt/orole/orole-daemon")
	for _, want := range []string{"ExecStart=/opt/orole/orole-daemon\n", "WantedBy=default.target", "Restart=on-failure"} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q:\n%s", want, u)
		}
	}
	if !strings.Contains(systemdUnit("/opt/my app/d"), `ExecStart="/opt/my app/d"`) {
		t.Error("path with spaces not quoted")
	}
}

func TestTaskXMLEscapesAndScopesToUser(t *testing.T) {
	x := taskXML(`DOM\a&b`, `C:\Program Files\Orole\orole-daemon.exe`)
	for _, want := range []string{
		`<UserId>DOM\a&amp;b</UserId>`,
		`<RunLevel>LeastPrivilege</RunLevel>`,
		`<Command>C:\Program Files\Orole\orole-daemon.exe</Command>`,
		`<LogonTrigger>`,
	} {
		if !strings.Contains(x, want) {
			t.Errorf("task xml missing %q", want)
		}
	}
	if strings.Contains(x, "HighestAvailable") {
		t.Error("task must never request elevation")
	}
}

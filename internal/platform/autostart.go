// Package platform holds OS-specific behaviour behind small interfaces:
// start-at-login registration and environment diagnostics.
package platform

import (
	"context"
	"fmt"
	"strings"
)

// TaskName is the Windows Task Scheduler task name.
const TaskName = "Orole OS Daemon"

// UnitName is the systemd user unit name used on Linux.
const UnitName = "orole-daemon.service"

// AutostartStatus describes whether the daemon starts at user logon.
type AutostartStatus struct {
	Enabled   bool   `json:"enabled"`
	Supported bool   `json:"supported"`
	Mechanism string `json:"mechanism"` // "Task Scheduler (per-user logon task)", "systemd user unit", ...
	Detail    string `json:"detail"`    // task/unit name or why it is unsupported
}

// Autostart registers orole-daemon to start when the current user logs on.
// It never needs administrator rights and never installs a Session 0 service.
type Autostart interface {
	Status(ctx context.Context) (AutostartStatus, error)
	// Enable registers daemonExe (absolute path); it overwrites an existing registration.
	Enable(ctx context.Context, daemonExe string) error
	// Disable removes the registration; removing a missing one is not an error.
	Disable(ctx context.Context) error
}

// runner executes an external command and returns combined output.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// systemdUnit renders the user unit for daemonExe. Pure, so it is testable on every OS.
func systemdUnit(daemonExe string) string {
	return fmt.Sprintf(`[Unit]
Description=Orole OS daemon
Documentation=https://github.com/rayorole/orole-os-desktop

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, systemdQuote(daemonExe))
}

// systemdQuote quotes an ExecStart executable path.
func systemdQuote(p string) string {
	if !strings.ContainsAny(p, " \t\"'\\") {
		return p
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(p) + `"`
}

// xmlEscape escapes text for an XML element body.
func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// taskXML renders the per-user logon task. It is used instead of
// `schtasks /Create /SC ONLOGON` because the latter returns "Access is denied"
// for non-elevated users; a LogonTrigger scoped to the user's own UserId does not need elevation.
func taskXML(user, daemonExe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Starts the Orole OS background daemon when you sign in.</Description></RegistrationInfo>
  <Triggers><LogonTrigger><Enabled>true</Enabled><UserId>%[1]s</UserId></LogonTrigger></Triggers>
  <Principals><Principal id="Author"><UserId>%[1]s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Hidden>false</Hidden>
  </Settings>
  <Actions Context="Author"><Exec><Command>%[2]s</Command></Exec></Actions>
</Task>
`, xmlEscape(user), xmlEscape(daemonExe))
}

# 0005: Task Scheduler logon task, not a Windows service

Status: accepted

## Context
The daemon should be able to start at login without administrator rights. A Windows service starts at boot in
session 0, isolated from the interactive desktop.

## Decision
Optional autostart is a per-user Task Scheduler task "Orole OS Daemon" with a `LogonTrigger` scoped to the current
user, `InteractiveToken`, `LeastPrivilege`, registered via `schtasks /Create /XML` (`internal/platform`).
A service is not the default and is not implemented. Reasons are in `docs/architecture/windows-runtime.md`:
session 0 isolation, per-user Credential Manager and HKCU, no elevation, lifetime tied to the user session.

Measured: `schtasks /Create /SC ONLOGON /RL LIMITED` fails with "Access is denied" for a standard user on this
machine, so the XML form is used.

## Consequences
- Works without admin; uninstall removes the task; no state outside the user's profile.
- No daemon before the user logs on, none after log off. Acceptable: everything the daemon does is for the logged-in user.
- A future headless/remote mode would be a separate decision (own account, data dir and pipe ACL).
- `Autostart` is an interface with a Windows and a systemd-user implementation, so adding other platforms is local.

# Windows runtime: how the daemon starts

Orole's daemon runs **inside the user's interactive session**, as a normal per-user process. It is started
in one of three ways, none of which needs administrator rights:

| Trigger | Mechanism |
|---|---|
| The app starts | `Link.Start` -> `control.Ensure` launches `orole-daemon.exe` detached (`DETACHED_PROCESS | CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP`) if no daemon answers |
| `orole daemon start` | the same `control.Ensure` path |
| User signs in (opt-in) | Task Scheduler logon task **"Orole OS Daemon"** |

## Start at logon: per-user Task Scheduler task

`internal/platform` defines the `Autostart` interface (`Status`, `Enable`, `Disable`) with a Windows
implementation (`autostart_windows.go`) and a systemd user-unit implementation for Linux
(`autostart_unix.go`, writes `~/.config/systemd/user/orole-daemon.service` and runs `systemctl --user enable`;
unit rendering is unit-tested, applying it is not).

Enable (`orole daemon install`, or Settings > Diagnostics > "Start daemon at login") registers the task with
`schtasks /Create /TN "Orole OS Daemon" /XML <file> /F`:

- Trigger: `LogonTrigger` scoped to the current user only (`<UserId>DOMAIN\user</UserId>`).
- Principal: that user, `InteractiveToken`, `RunLevel=LeastPrivilege` (never elevated).
- Action: the absolute path of `orole-daemon.exe`; no execution time limit; ignore a second instance.
- Disable (`orole daemon uninstall`): `schtasks /Delete /TN "Orole OS Daemon" /F`; deleting a task that does not
  exist is not an error.

Why XML and not `schtasks /Create /SC ONLOGON /RL LIMITED ...`? Measured on this Windows 11 machine as a
standard (non-elevated) user, the flag form fails with `ERROR: Access is denied.` because `/SC ONLOGON` without
a user scope is a trigger for *any* user's logon. The XML form with `LogonTrigger/UserId` registers fine without
elevation. Verified with `schtasks /Query /TN "Orole OS Daemon" /V /FO LIST` (Schedule Type: At logon time,
Run As User: current user), then removed.

The packaged app's own start-at-login (`mygo.App.SetOpenAtLogin`, a `HKCU\...\Run` value) is a separate,
not-yet-used switch for the GUI; the daemon task starts the daemon without opening a window.

## Why not a Windows service (Session 0)

A Windows service is **not** the default and is not implemented. Reasons:

1. **Session 0 isolation.** Services run in session 0 with no desktop. Orole's daemon launches the user's shells
   (ConPTY), git, editors and agents that must see the user's environment, profile, `HKCU`, drive mappings
   and credential store. A service would run as `LocalSystem` or a service account and see none of that.
2. **Credential Manager is per-user.** `internal/credentials` stores secrets in the *user's* vault; a service
   account cannot read them, and running as the user needs the user's password at install time.
3. **Privilege.** Installing a service needs elevation; the daemon needs none. A per-user process cannot be abused
   to escalate, and its pipe DACL (current user SID only) is simple to reason about.
4. **Lifecycle.** Terminals are interactive sessions of this user; they should end at log off.

A service would only make sense for a future headless/remote mode that does not touch the interactive desktop
(it would need its own data directory, account and pipe ACL). Decision record: `docs/decisions/0005-task-scheduler-vs-service.md`.

## Single instance

One daemon per data directory: the pipe name embeds the user SID and a hash of the data dir, and `Listen`
fails if the pipe exists. The token file is written only after listening succeeds.

## WebView2

The MyGo window needs the Evergreen WebView2 runtime (bundled with Windows 11). `orole daemon diagnostics`
reads its version from `EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}\pv` (HKLM WOW6432Node, HKLM, HKCU).

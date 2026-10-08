# Desktop integration and application launcher

## Ownership and security

The Applications module combines built Orole modules from the existing frontend `NAV` + `views` registries with explicitly configured native application shortcuts. It does not scan installed applications, synthesize example entries, or claim unbuilt modules work.

Shortcuts are persisted in daemon-owned SQLite (`0080_applications.sql`, `application_shortcuts`). The bound `app.Applications` service is a stateless authenticated-IPC bridge. Saving, editing, and removing shortcuts does not execute anything. Updates use optimistic revisions so a stale editor cannot silently replace a newer configuration.

A launch request contains only the persisted shortcut ID. The daemon loads its configuration and requires Go policy authorization for `applications.launch`, actor `user`, resource `applications/<id>/revision/<revision>`, effect `destructive`. Native executable execution is not a safe/read-only operation. The policy engine controls whether a configured rule denies/allows the request or presents an approval; the default is an approval, with no remember checkbox for destructive actions. This feature does **not** register an agent or automation launch capability. Agent process/session execution has its own Go authorization in the agent manager and must not route through this user-only launcher entry point.

After approval, the manager rechecks the shortcut revision, existence, executable, working directory, and request cancellation. An edit or deletion during approval invalidates that launch. A lock serializes that final check/start with shortcut mutations, without holding a lock while the user decides. Policy decisions and launch outcomes are audited. A successful response means the OS created a process, not that the application finished initialization or completed a task.

Executable boundaries on Windows:

- An existing absolute local `.exe` file is required. No `PATH` search, UNC paths, device namespaces, alternate data streams, `.lnk` files, batch files, or PowerShell scripts.
- Paths are canonicalized at save time; changing a canonical path into a different symlink target invalidates the launch. This is not a binary-signature or file-content attestation mechanism. An executable installed at the same path can change between launches.
- Arguments are an explicit bounded string array, passed directly through `exec.Command`, never parsed by a shell. A user can intentionally configure a shell executable; that remains arbitrary native execution and requires the same authorization. The launcher is not a sandbox.
- Working directory is an optional existing absolute local folder; the default is the executable's folder.
- Name, arguments, and paths reject control characters. The complete command description must fit inside the policy approval detail limit; hidden truncated argument suffixes cannot be authorized.
- There are no credential or environment-override fields. No secret is fetched from Credential Manager or sent to the frontend. Shortcut arguments are ordinary plaintext metadata: never put passwords, API keys, or tokens in them. Use the launched application's own secure sign-in instead.
- Child processes are independent of the launch request and desktop lifetime. The daemon reaps process completion but does not present this launcher as a process supervisor, and quitting the desktop does not kill launched applications.

## Real framework APIs

Implementation research read both the local `../_upstream/mygo` source and the pinned `github.com/egoist/mygo@v0.3.3` module. Relevant sources are `app.go`, `modules.go`, `menu.go`, `window.go`, `deeplink.go`, `single_instance.go`, and `docs/app.md`. No tray, menu, scheme-registration, or instance-forwarding framework was copied or reimplemented.

`cmd/desktop/native.go` uses MyGo's `NewTray`, `NewMenu`, `App.SetMenu`, `Window.SetAutoHideMenuBar`, `OnClose`, `OnBeforeQuit`, `OnWillQuit`, `RequestSingleInstanceLock`, `OnSecondInstance`, `RegisterURLScheme`, and `OnOpenURL` APIs.

- The tray has Show, Hide, Applications, and Quit actions. Show also restores a minimized window and focuses it.
- Closing the main window hides it while a tray exists. Quit marks the GUI as quitting before window-close interception, then destroys the tray while the event loop is alive. No daemon shutdown or start-at-login setting changes occur.
- If tray creation fails, Hide remains disabled and closing the window uses the normal application shutdown behavior. The error is logged; there is no fake tray state.
- The application menu is auto-hidden to preserve the existing title bar, accessible with Alt/F10. Shortcuts: Applications `Ctrl+Shift+L`, Home `Ctrl+Shift+1`, Settings `Ctrl+,`, Hide `Ctrl+Shift+H`, Quit `Ctrl+Q`. MyGo supplies normal Edit menu roles. These are application-menu shortcuts, not registered system-wide hotkeys.
- Existing Files/Notifications code owns native notifications. This slice creates no parallel notification integration.

### Deep links

The only accepted syntax is `orole://open/<module>`, with an exact built-in allowlist in `applications.InternalView`. Examples: `orole://open/applications`, `orole://open/files`, `orole://open/settings`.

Queries, fragments, encoded aliases, credentials, ports, external schemes/hosts, executable IDs, arbitrary paths, and unsupported modules are rejected. A deep link can only request internal navigation. It cannot launch a configured shortcut, access a file, run a command, change settings, or submit an agent prompt. Rejected URL contents are not logged.

MyGo receives both cold-start launch arguments and forwarded second-instance arguments. A GUI-lifetime controller stores the latest navigation request and sequence, and emits the typed `desktop:navigate` event. The React shell subscribes before calling `Desktop.ready()`, ignoring an older snapshot if a newer event arrived. This handles React startup/reload without evaluating JavaScript or navigating the WebView to untrusted URLs. The pending navigation is ephemeral native-shell state, not daemon/business state.

`mygo.json` already declares `urlSchemes: ["orole"]`. Production builds also register the scheme through MyGo's current-user API. Development runs deliberately do not replace the installed app's OS scheme association. Explicit launch arguments still work in a MyGo development bundle using its declared scheme metadata. Binding generation bypasses native setup entirely, so it cannot register a temporary generator executable as a URL handler.

### Framework limitations, not fabricated support

- MyGo v0.3.3's single-instance transport uses a local Unix-domain socket on Windows as well as Unix, not a TCP/network-interface listener. This is framework-owned GUI activation, separate from Orole's authenticated named-pipe daemon IPC. Incoming activation data is still untrusted and passes the strict parser.
- The framework lock is best-effort: if its socket cannot be created or it exhausts retries, its implementation returns true and permits another instance. This implementation does not claim a stronger lock or replace it with a hand-rolled service.
- Native tray availability is platform/framework dependent. Linux requires AppIndicator support; click behavior differs by platform. This is a Windows-first feature, not a promise of identical tray behavior everywhere.
- Programmatically registering a portable production executable makes that path the current-user URL handler. Moving/deleting that executable can leave a stale OS association. Installed builds should use the installer/uninstaller lifecycle; registration success is not proof that a user's OS/browser will permit opening a link.
- Native UI, installer routing, and OS process creation require Windows runtime acceptance. Source-level implementation and added regression tests are not a claim that this manual acceptance has already run.

## Integration

`internal/daemon/services.go` registers applications after MCP initializes the policy engine.
`internal/app/bridge.go` subscribes to application changes, forwards them through the generated
typed event, and gives native launch requests the same approval-aware timeout as agent/MCP requests.
`cmd/desktop/main.go` prepares the native lifecycle before binding services, binds Applications and
Desktop, and attaches the lifecycle controller to the main window. `NativeNavigation` is mounted
once beside the global policy approval surface; Applications is a lazy registered feature.
`frontend/src/mygo.ts` contains the generated service/event contracts.

## Verification

Behavior regressions cover persistence/revisions, stale edits, denial and absent policy preventing
execution, edits/deletes during approval, cancellation, executable boundaries, deep-link allowlisting,
and navigation snapshot/event races. The combined Go run passed `internal/applications` under the
race detector; the frontend unit suite passed the native-navigation regressions.

On 2026-10-08, a real isolated daemon was reached over authenticated named-pipe IPC. A configured
PowerShell shortcut was denied first: no output file was created. A second launch was explicitly
approved through the policy queue and the native process wrote the expected proof file. This verifies
daemon-side persistence/authorization/process creation, not the interactive launcher editor.
The NSIS-installed MyGo/WebView2 application also launched and connected to its daemon.

Additional installed-desktop smoke exercised the native Settings and Applications
menu accelerators, Hide to tray, second-instance activation, and minimized-window
restoration. The second instance exited with code 0; the original desktop PID and
daemon PID remained unchanged. Window visibility/minimized state was observed
through Win32 APIs, and native navigation was observed in the actual WebView.

Remaining interactive acceptance: shortcut editor/reopen/removal, tray menu
Show/Hide/close/Quit, cold/warm OS deep-link routing, and daemon continuity after
desktop Quit. Use isolated data and the intended installed build for OS-handler
acceptance. Do not drive the native window while it is receiving other user input.

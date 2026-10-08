# MyGo compatibility

Source read: `C:/code/projects/_upstream/mygo/docs` (HEAD `8bf0035a951881c7bdaebe12c9cfb1e5dbdb85e8`). Orole pins
`github.com/egoist/mygo v0.3.3` (`go.mod`); the docs checkout may be newer than the pin [INFERENCE]. 

Legend: **RAN** = exercised on this machine (Windows 11 10.0.22000, WebView2 154.0.4258.62) in this work;
**USED** = used by Orole code that builds, not otherwise exercised; **READ** = only read in docs, not run.

| Capability | Orole use | Evidence |
|---|---|---|
| Typed Go service bindings -> generated `frontend/src/mygo.ts` | `mygo.Bind(app.X{...})`, `go tool mygo generate` | **RAN** (generate produced `System`, `AutostartStatus`, `Report`) |
| Hidden title bar and Windows caption-button insets | main window options | **RAN**: installed WebView2 screenshots and header-controls smoke; reserved right inset measured 138 px on the acceptance machine |
| Frameless windows (`Frameless: true`, `--app-region: drag`) | not used (hidden title bar chosen) | **READ** (`frontend.md`) |
| Window icon (`Window.SetIcon(png)`) | `cmd/desktop/window-icon.png` embedded | **USED** (call compiled and ran in the packaged app; its stderr was not captured and the icon pixels were not inspected) |
| App/exe icon from `resources/icon.png` | generated icon | **RAN** (build embedded it; `mygo build` log, exe produced) |
| `resources` copied next to the exe | `build/orole-daemon.exe` | **RAN** (installed dir contained `orole-daemon.exe`; `App.Path(PathResources)` found and launched it) |
| NSIS installer (per-user, `/S`, `/D=`, uninstaller) | bundled desktop + daemon | **RAN**: silent isolated install, launch and uninstall; current artifact measurements are in `docs/development/packaging.md` |
| NSIS auto-download (3.13, SHA-256 checked) | first build | **RAN** (log: "downloading NSIS 3.13") |
| `mygo dev` (HMR, rebuild on Go change) | `devCommand` builds daemon then Vite | **READ**; not exercised in this task |
| Deep links / `urlSchemes: ["orole"]`, `App.OnOpenURL` | strict `orole://open/<module>` navigation handler | **RAN**: cold and warm installed OS-scheme routing; query-bearing links rejected without execution |
| Single instance lock (`RequestSingleInstanceLock`) | GUI activation and argument forwarding | **RAN**: second process exited while original GUI/daemon PIDs remained; framework best-effort limits documented |
| Start at login (`SetOpenAtLogin`: HKCU Run value on Windows) | not used; daemon uses Task Scheduler | **READ** |
| Tray icons (`NewTray`) | Show, Hide, Applications, Quit | **RAN**: hide/restore and GUI-only quit through native menu actions; tray-menu mouse selection remains unverified |
| GlobalShortcut | system-wide Ctrl+Alt+Space activation, unregister on quit | **USED**: framework API integration; see desktop integration guide for runtime acceptance status |
| Updater (`mygo.Updater`, signed, delta updates, `mygo keygen`) | bound status/check/install bridge; no keys configured | **RAN**: truthful disabled-update UI and process-exit barrier regression; signed release replacement remains blocked |
| DevTools / WebView2 CDP | opt-in loopback CDP for isolated installed-app acceptance | **RAN**: inspected and drove the installed WebView2 app over CDP; not a production listener |
| Child/embedded webviews for browser panes | frontend uses sandboxed iframe instead | no child-WebView console/network debugging claim; sites may reject iframe embedding |
| Terminal and SQLite plugins | not used: Orole has its own ConPTY manager and modernc SQLite in the daemon | **READ** |

## Measured

Packaged build and run: see `docs/development/packaging.md` and `docs/performance.md` (window with title visible
47-160 ms after launch warm, 571 ms cold, 5 runs; this is window creation, not first paint of the React UI).

## Gaps and risks

- Pre-1.0 API; pinned version, upgrade deliberately.
- Signed updates and child-WebView debugging are not verified; tray-menu mouse acceptance is still open. Native lifecycle evidence and framework limitations are in `docs/architecture/desktop-integration.md`.
- Installer and exe are unsigned.

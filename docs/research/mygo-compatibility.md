# MyGo compatibility

Source read: `C:/code/projects/_upstream/mygo/docs` (HEAD `8bf0035a951881c7bdaebe12c9cfb1e5dbdb85e8`). Orole pins
`github.com/egoist/mygo v0.3.3` (`go.mod`); the docs checkout may be newer than the pin [INFERENCE]. 

Legend: **RAN** = exercised on this machine (Windows 11 10.0.22000, WebView2 154.0.4258.62) in this work;
**USED** = used by Orole code that builds, not otherwise exercised; **READ** = only read in docs, not run.

| Capability | Orole use | Evidence |
|---|---|---|
| Typed Go service bindings -> generated `frontend/src/mygo.ts` | `mygo.Bind(app.X{...})`, `go tool mygo generate` | **RAN** (generate produced `System`, `AutostartStatus`, `Report`) |
| Hidden title bar (`TitleBarHidden`, `TitleBarHeight: 40`), Windows 11 caption buttons and snap layouts | main window options | **RAN**: packaged app opened a window titled "Orole OS"; visual appearance not screenshotted |
| Frameless windows (`Frameless: true`, `--app-region: drag`) | not used (hidden title bar chosen) | **READ** (`frontend.md`) |
| Window icon (`Window.SetIcon(png)`) | `cmd/desktop/window-icon.png` embedded | **USED** (call compiled and ran in the packaged app; its stderr was not captured and the icon pixels were not inspected) |
| App/exe icon from `resources/icon.png` | generated icon | **RAN** (build embedded it; `mygo build` log, exe produced) |
| `resources` copied next to the exe | `build/orole-daemon.exe` | **RAN** (installed dir contained `orole-daemon.exe`; `App.Path(PathResources)` found and launched it) |
| NSIS installer (per-user, `/S`, `/D=`, uninstaller) | `Orole OS Setup 0.1.0.exe` 7.8 MB | **RAN**: silent install to temp dir, exit code 0, uninstall via `Uninstall.exe /S _?=<dir>` |
| NSIS auto-download (3.13, SHA-256 checked) | first build | **RAN** (log: "downloading NSIS 3.13") |
| `mygo dev` (HMR, rebuild on Go change) | `devCommand` builds daemon then Vite | **READ**; not exercised in this task |
| Deep links / `urlSchemes: ["orole"]`, `App.OnOpenURL` | scheme declared in `mygo.json`; no handler yet | **READ**; registration by the installer not inspected |
| Single instance lock (`RequestSingleInstanceLock`) | not used yet (the daemon is single-instance by pipe) | **READ** |
| Start at login (`SetOpenAtLogin`: HKCU Run value on Windows) | not used; daemon uses Task Scheduler | **READ** |
| Tray icons (`menus.md`) | not used | **READ** |
| Updater (`mygo.Updater`, signed, delta updates, `mygo keygen`) | not enabled, no keys | **READ**; documented in `docs/development/updates.md` |
| DevTools (F12 / Ctrl+Shift+I when enabled in the window) | not verified | **READ** |
| Child/embedded webviews for browser panes | needed by Shipyard's browser pane | unverified in `shipyard-analysis.md`; **not investigated here** |
| Terminal and SQLite plugins | not used: Orole has its own ConPTY manager and modernc SQLite in the daemon | **READ** |

## Measured

Packaged build and run: see `docs/development/packaging.md` and `docs/performance.md` (window with title visible
47-160 ms after launch warm, 571 ms cold, 5 runs; this is window creation, not first paint of the React UI).

## Gaps and risks

- Pre-1.0 API; pinned version, upgrade deliberately.
- No verified story yet for tray, deep-link handling, single-instance GUI, updater, or multi-webview.
- Installer and exe are unsigned.

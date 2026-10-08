# Troubleshooting

First run `orole daemon diagnostics` (or Settings > Diagnostics in the app). It shows the daemon state, schema
version, credential-store check, WebView2 version and the last log lines.

| Symptom | Cause / fix |
|---|---|
| Status bar says offline; `orole daemon status` says "not running ... cannot find the file specified" | No daemon owns the pipe. `orole daemon start`, or read `logs/daemon.log` for a startup error (database locked, migration failure). |
| Daemon exits immediately with "listen ...: is another daemon running?" | Another daemon holds the same data dir's pipe. `orole daemon status` to find it; `orole daemon stop`. Distinct data directories are separate instances; relative and absolute paths to the same directory use the same pipe. |
| `go build -o build/orole-daemon.exe` fails with "Access is denied" | The old daemon exe is running (Windows locks running images). `orole daemon stop`, or stop `orole-daemon.exe` in Task Manager, then rebuild. |
| `go tool mygo build` fails copying resources | `build/orole-daemon.exe` missing. Use `./scripts/build.ps1`. |
| `schtasks /Create /SC ONLOGON ...` returns "Access is denied" | Expected for non-admin users. Orole registers the task from XML scoped to your user; use `orole daemon install`. |
| "credential store: FAILED" in diagnostics | Credential Manager unavailable for this session (e.g. running as a different user or a service). Orole needs your interactive user session. |
| "webview2: not found" | Install the Evergreen WebView2 runtime from Microsoft. |
| Port 5173 busy in dev | Another Vite is running; reuse it, or set another port in `frontend/vite.config.ts` and `mygo.json` `devUrl`. |
| App window opens but UI shows "browser preview" / needs desktop app | You opened the Vite URL in a browser; use `go tool mygo dev` or the installed app. |
| Bindings out of date (`frontend/src/mygo.ts` diff in CI) | `go tool mygo generate` and commit. |
| Terminal gone after restarting the daemon | By design: sessions end with the daemon (`docs/architecture/windows-terminals.md`). |
| Installer: SmartScreen warning | Unsigned build; signing is not configured (`packaging.md`). |
| Development IPC authentication fails after upgrading an older build | Rebuild both desktop and daemon. Current builds normalize `OROLE_HOME` to an absolute path before resolving the token and pipe; older builds could use separate pipes for relative and absolute names while sharing a token. Stop only the affected development instance; do not remove real user data. |

## Logs

`%LOCALAPPDATA%\Orole\logs\daemon.log` (or under `OROLE_HOME`), JSON lines, rotated at 5 MiB keeping five
files (`daemon.log.1`..`.5`). `orole daemon logs -n 200` or `-f` to follow.

# Testing

## Commands

```powershell
go fmt ./... ; go vet ./... ; go test -race ./...     # Go
bun run --cwd frontend check                          # tsc + oxlint + vitest
go test -run '^$' -bench . -benchmem ./tests/performance   # benchmarks (docs/performance.md)
```

CI (`.github/workflows/ci.yml`, pull requests only, read-only token, no secrets) runs vet, `test -race`, the
frontend check, a bindings-up-to-date check (`go tool mygo generate` then `git diff --exit-code frontend/src/mygo.ts`),
`scripts/build.ps1` (daemon, CLI, `mygo build`) and asserts an installer exists, on `windows-latest`. An
`ubuntu-latest` job runs `go test -race` on `internal/{ipc,database,platform,paths,credentials}` only.

## Style

- Table/behaviour tests, `-race` clean, stdlib first. Temp dirs (`t.TempDir()`), temp git repos and
  `httptest` servers instead of mocks of the real thing.
- Isolated IPC: `ipc.Endpoint(t.TempDir())` gives a unique pipe per test (`internal/ipc/ipc_test.go`).
- Windows-only tests live in `*_windows_test.go` (e.g. ConPTY shells, Credential Manager round trip, which
  uses a throwaway key and deletes it).
- Autostart: the Windows task XML and the systemd unit are rendered by pure functions and asserted in
  `internal/platform`; the systemd path is exercised with an injected command runner and temp unit dir. Registering
  a **real** Task Scheduler task is a manual check (below), never part of `go test`.
- Frontend tests only for real logic (`commands`, `themes/custom`, `ascii/motion`, ...).

## Manual checks used for releases

```powershell
./scripts/build.ps1 -DaemonOnly
$env:OROLE_HOME = "$env:TEMP\orole-check"
build\orole.exe daemon start ; build\orole.exe daemon status ; build\orole.exe daemon diagnostics ; build\orole.exe daemon stop
build\orole.exe daemon install ; schtasks /Query /TN "Orole OS Daemon" /V /FO LIST ; build\orole.exe daemon uninstall
./scripts/build.ps1 ; ./scripts/smoke-installer.ps1      # installs to a temp dir, launches the GUI, checks the daemon, uninstalls
./scripts/measure-daemon.ps1                              # cold start, idle memory and CPU
```

`smoke-installer.ps1` needs an interactive desktop session with WebView2 because it launches the real window.

## Browser acceptance and visual baselines (#27)

Run from `frontend` after `bun install`:

```powershell
bunx playwright install chromium
bun run test:e2e
# Create/review baselines only from a real browser run on the reference platform:
bunx playwright test e2e/shell.spec.ts --update-snapshots
bunx playwright show-report playwright-report/browser
```

`playwright.config.ts` starts Vite on `127.0.0.1:5173` and explicitly excludes `e2e/native`.
The shell suite uses the real browser-preview behavior, not a desktop bridge shim. It covers
Ctrl+B, Ctrl+K, palette focus/escape/keyboard execution, theme/density/reduced-motion persistence,
workspace creation/deletion, widget add/remove, pointer drag/resize and saved notes after reload.
Light and Midnight baselines cover the shell, appearance panel, palette and Notes widget.
Time, locale, viewport and reduced motion are fixed; the ASCII backdrop is disabled via browser
appearance preferences. Axe checks WCAG A/AA rules without excluding failing rules or elements.
Keyboard assertions complement axe; neither replaces a screen-reader release check.

The agent/project/file suites use explicit test-only MyGo fixtures and remain browser UI contract
tests, **not evidence that installed services work**. Screenshots from those scenarios are run
artifacts under `test-results/browser`, not hard-coded paths into a developer's home.
Visual references live beside `shell.spec.ts` in Playwright's snapshot directory. Generate them
with the actual run above, review the images, and commit only intentional reference changes.
Do not manufacture references or silently update them to make a failing comparison pass.
Snapshot names include the platform; use the same Chromium version and OS for comparisons.

## Installed WebView2 acceptance (#65)

Prerequisites: Windows interactive desktop, PowerShell 7.2+, WebView2, an already installed current
Orole OS build (with its adjacent `orole-daemon.exe`), a built `orole.exe` CLI, Bun and installed
frontend dependencies. Quit your normal Orole OS **yourself** first: the suite refuses an existing GUI
rather than forwarding through its single-instance lock. Do not sign into integrations in the test
home; these tests need no credentials, remote machines, projects or external agent execution.

From the repository root:

```powershell
pwsh -File scripts/test-installed-e2e.ps1 `
  -AppPath "$env:LOCALAPPDATA\Programs\Orole OS\Orole OS.exe" `
  -CliPath "$PWD\build\orole.exe" `
  -AllowIsolatedHome
```

Supply your actual installed executable path if different. `-Port 9227` selects the loopback CDP
port; a busy port is rejected. This does not build or install the app. It launches the **installed**
daemon and GUI with a newly created `OROLE_HOME` and separate `WEBVIEW2_USER_DATA_FOLDER`, then
runs `bunx playwright test --config playwright.native.config.ts`. It restores inherited environment
variables and terminates only process handles it created, including owned WebView2 children.
Temporary app data is removed even on test failure; test artifacts remain in
`frontend/test-results/native` and `frontend/playwright-report/native`.

The native config has no Vite server, browser launch or application mocks. Its fixture uses
`chromium.connectOverCDP` only after requiring `OROLE_E2E_ALLOW_ISOLATED_HOME=1`, an explicit numeric
loopback HTTP `OROLE_E2E_CDP_ENDPOINT`, an absolute `OROLE_HOME`, and an `OROLE_E2E_MANIFEST`.
The launcher writes a random run nonce through the real daemon; the fixture verifies that nonce
through the installed `Settings.All` bridge and verifies the daemon PID before mutating anything.
Missing opt-in/manifest or a mismatched home fails the suite, rather than skipping or attaching to
a convenient browser. The fixture never closes the attached browser, context or page; only the
launcher owns process lifetime. Do not expose the CDP port to a network or reuse a normal profile.

Native acceptance creates a uniquely named workspace through the UI, adds/edits/moves/resizes a
widget, uses the palette to change theme, and checks persisted daemon records plus reloaded UI.
It removes the widget/workspace and verifies removal after reload. Notification acceptance uses
the actual Settings **Test notification** action with native delivery/sound disabled for the test
source, then marks read and dismisses through the center, checking persistence after reload.
Tests restore preferences and clean only their owned records in `finally`; the disposable home
also removes dismissed history. Browser layout persistence covers #22; real notification mutations
cover #23. This does not assert native Windows toast delivery, installer correctness, or survival
across a daemon restart; use the separate installer/manual checks for those release gates.

Observed on Windows, 2026-10-08: the rebuilt NSIS-installed app passed both native
Playwright cases (workspace/grid/palette persistence and notification preferences,
read/dismiss/reload) against its isolated real daemon. The browser suite separately
passed 16 tests; frontend checks passed 115 tests, with existing lint warnings.

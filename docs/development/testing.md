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

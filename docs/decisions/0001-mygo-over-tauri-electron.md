# 0001: MyGo desktop shell instead of Tauri or Electron

Status: accepted

## Context
The product is a Windows-native environment whose real work (terminals, git, integrations, permissions) is Go.
Shipyard, the design reference, is a Tauri 2 app (Rust shell + React) talking to Go daemons over HTTP
(`docs/research/shipyard-analysis.md` section 16 lists the Tauri coupling to replace). Options: Tauri, Electron, MyGo.

## Decision
Use MyGo (`github.com/egoist/mygo`, pinned v0.3.3 in `go.mod`): a Go-native shell using the system WebView2, typed
Go-to-TypeScript bindings generated from Go services (`go tool mygo generate` -> `frontend/src/mygo.ts`), and
built-in NSIS packaging, hidden-title-bar windows, deep links, single instance and an updater.

## Consequences
- One language for shell, daemon and CLI; the bound services in `internal/app` share types with the daemon
  (`api.Health`, `terminals.Info`), so the frontend types cannot drift.
- Small output: measured 15.1 MB app exe, 7.8 MB installer (`docs/development/packaging.md`); no Chromium or Rust
  toolchain to ship or build.
- Dependency on a young project (0.x): API changes and gaps are our risk. `docs/research/mygo-compatibility.md`
  records which features were verified by running and which only read.
- Needs the WebView2 runtime (present on Windows 11; diagnostics reports it).
- The shell is deliberately thin so replacing it later would touch `cmd/desktop` and `internal/app/bridge.go` only.

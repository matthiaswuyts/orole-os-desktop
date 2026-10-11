# Orole OS

A Windows-native personal operating environment: one desktop window for terminals, projects,
worktrees, coding agents, files, automations, and configured integrations. It is a thin
[MyGo](https://github.com/egoist/mygo) desktop shell with a React UI, in front of a separate
headless Go daemon that owns all durable state.

```
React UI (frontend/)  ->  MyGo typed services (internal/app)  ->  orole-daemon (cmd/daemon)
 UI only, not a            desktop process, stateless,            per-user background process:
 security boundary         forwards calls                         SQLite, terminals, permissions
                                      \__ authenticated named pipe (internal/ipc) __/
```

Terminals live in the daemon, so they survive closing and reopening the window. They do **not**
survive the daemon exiting or a reboot; the UI says so. See `docs/architecture/overview.md`.

## Status

State of this tree; the code is the source of truth (`docs/architecture/daemon.md` lists the IPC methods that exist).

| Area | Status | Notes |
|---|---|---|
| Daemon, IPC (named pipe + token), SQLite migrations | Implemented | `internal/daemon`, `internal/ipc`, `internal/database` |
| CLI `orole` (daemon start/stop/status/logs/diagnostics/install, terminal list, settings) | Implemented | `cmd/orole-cli` |
| Start at logon via Task Scheduler (no admin) | Implemented | `internal/platform`, Settings > Diagnostics |
| Terminals (ConPTY: PowerShell, pwsh, cmd, Git Bash) | Implemented | `internal/terminals`; scrollback restored on re-attach |
| Settings, workspaces/layouts, themes, ASCII band, command palette | Implemented | |
| Projects, git status/diff/log, worktrees | Implemented (daemon methods + views) | `internal/projects`, `internal/git` |
| GitHub | Daemon methods and UI integrated | Existing GitHub CLI authentication; repositories, issues, PRs, checks and issue-to-agent flow |
| Local/Git files, editor, notifications, search | Daemon methods and UI integrated | Approved roots, bounded reads, optimistic-save conflicts, recycle-aware deletion |
| MCP and policy approvals | Daemon methods and UI integrated | Runtime discovery, OS-protected credentials, global approval queue |
| Mail and Drive | Adapters and setup UI integrated; live authorization blocked | Servers reject dynamic OAuth registration; a registered public client ID is required before live capabilities can be verified |
| Agent runtime and review | Daemon methods and UI integrated | Real Codex turn exercised from installed Home composer with selected model/reasoning and a daemon-owned worktree; Claude structured smoke blocked by expired CLI authorization |
| Automations | Daemon runtime and editor integrated | Real command + persisted notification workflow exercised; journaled steps, approvals, schedules, cancellation |
| Machines and remote files | Native and SSH implementations integrated | Local health/files/execution exercised; external Windows/Linux SSH hosts not yet verified |
| Application launcher and native shell | Integrated; installed-app smoke passed | Shortcut editing and policy-gated execution, hide/restore, cold/warm deep links, and GUI quit/relaunch with the same daemon and PowerShell process exercised |
| Guided first-run setup | Integrated | Optional steps, daemon-persisted progress; native reload persistence exercised |
| Packaging: NSIS installer, signed auto-update | Installer built and installed; release pipeline defined; **signed upgrade unverified, no Authenticode** | Installed WebView2 app connected to its daemon; `docs/development/packaging.md`, `docs/development/updates.md`, `docs/development/releasing.md` |
| Linux / macOS | Not supported as complete desktop targets | Platform-independent packages have tests; no cross-platform desktop-release claim |
| Declarative MCP plugins | Integrated; daemon and installed-UI smoke passed | Exact-manifest trust, independent MCP approval, real command/widget execution, persisted revocation; no arbitrary JavaScript plugin host |
| Chat (bots, direct OpenRouter/OpenAI APIs, web search, research, uploads, memory, provider manifests) | Daemon and UI integrated; daemon IPC wiring covered by tests; **not yet exercised against live provider APIs** | `docs/architecture/chat.md`; opt-in live smokes `OROLE_OPENROUTER_LIVE=1`, `OROLE_OPENAI_LIVE=1` |

## Quickstart

Requirements: Windows 10/11, WebView2 runtime (bundled with Windows 11), Go (the `go` directive in `go.mod`; the toolchain downloads automatically), [Bun](https://bun.sh).

```powershell
.\start.ps1                          # isolated dev desktop with hot reload; installs frontend dependencies if missing
bun run --cwd frontend dev           # browser preview only, http://localhost:5173 (no daemon)
go fmt ./... ; go vet ./... ; go test -race ./...
bun run --cwd frontend check         # tsc + lint + vitest
./scripts/build.ps1                  # daemon + CLI + packaged app + installer under build/
```

`start.ps1` uses `.orole-dev` under the repository, works from any working directory, and restores
your shell's `OROLE_HOME` afterward. Use `.\start.ps1 -DataDir <path>` for another data directory.
It does not stop existing daemons or reset data. If a stale daemon rejects authentication, recover
that instance before starting; restarting a daemon ends its terminals and agents.
For a manual launch, set `$env:OROLE_HOME = ".orole-dev"` and run `go tool mygo dev`.

The development command builds the bundled daemon **once at startup**. Frontend hot reload does not
rebuild or restart that separate process. After daemon changes (including harness model discovery),
finish running tasks/terminals, stop the dev runner, run `go run ./cmd/orole-cli daemon stop` with the
same `OROLE_HOME`, then start `go tool mygo dev` again. Restarting only the window can retain an old
daemon. Claude's model menu comes from the installed CLI's initialization catalog, not a fixed list;
an older daemon without that discovery code will show “Model catalog unavailable” even when Claude
is installed and working.

CLI (after `./scripts/build.ps1 -DaemonOnly`, or `go run ./cmd/orole-cli ...`):

```powershell
build\orole.exe daemon start ; build\orole.exe daemon status ; build\orole.exe daemon diagnostics
build\orole.exe terminal list ; build\orole.exe settings get ; build\orole.exe daemon stop
```

## Documentation

- `AGENTS.md` architecture rules (binding), `docs/architecture/` design, `docs/decisions/` ADRs
- `docs/development/` setup, testing, packaging, releasing, updates, troubleshooting, resetting dev data
- `docs/security/threat-model.md`, `docs/performance.md`, `docs/research/`

## Licence

MIT, see `LICENSE`. Third-party code copied into the tree is listed in `docs/research/upstream-attribution.md`.

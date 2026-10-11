# Reuse inventory

What Orole takes from outside, what it re-implements, and what it leaves. Companion to
`shipyard-analysis.md` (read it for evidence and line references) and `upstream-attribution.md` (exact copied files).

| Item | Source | Decision | Where in Orole |
|---|---|---|---|
| UI kit `components/ui` (coss/Base UI) | Shipyard `app/src/components/ui` | **Copied** (MIT) | `frontend/src/components/ui` |
| Design tokens, density, base CSS | Shipyard `app/src/index.css` | **Copied, trimmed** | `frontend/src/index.css` |
| Theme colour sets | Shipyard `app/src/themes/builtin.ts` | **Copied, renamed** | `frontend/src/themes/builtin.ts` |
| `cn` util | Shipyard `lib/utils.ts` | **Copied** | `frontend/src/lib/utils.ts` |
| Sidebar + projects tree, dashboard/review layout language | Shipyard views | **Re-implemented** to match look and density | `components/shell`, `features/*` |
| Widget model (home) | Shipyard Harbour widgets | **Re-implemented** small registry | `components/widgets/registry.tsx` |
| Animated ASCII scenes | ascii.rest 0.1.0 | **Dependency** (MIT) | `components/ascii/AsciiBackground.tsx` |
| Desktop shell, bindings, installer | MyGo v0.3.3 | **Dependency** | `cmd/desktop`, `mygo.json` |
| Terminal host | Shipyard: tmux + Unix PTY | **Replaced**: ConPTY manager in the daemon | `internal/terminals` |
| Local API transport | Shipyard: loopback HTTP / Unix socket | **Replaced**: named pipe + token | `internal/ipc` |
| Service install | Shipyard: systemd/launchd | **Replaced**: Task Scheduler logon task | `internal/platform` |
| Session durability via tmux | Shipyard | **Not reproduced**; honest persistence instead | ADR 0006 |
| Remote boxes, tailnet/ssh onboarding, pairing/trust Go packages | Shipyard | **Re-implemented**, not copied; native + pinned SSH transport | `internal/machines`; external SSH acceptance remains open |
| Plugin SDK / host | Shipyard | **Replaced** with declarative MCP capability manifests, no arbitrary React plugin code | `internal/plugins`, `features/plugins`, `docs/architecture/plugins.md` |
| Terminal emulator in the UI | xterm.js (also used by Shipyard) | **Dependency** | `features/terminals/TerminalPane.tsx` |
| Secrets | Windows Credential Manager via wincred | **Dependency** | `internal/credentials` |
| SQLite | modernc.org/sqlite | **Dependency** | `internal/database` |

Rules: copied code is recorded in `upstream-attribution.md` with path, commit and modification; dependencies are
pinned in `go.mod` / `frontend/bun.lock`; anything not read or run is not claimed here.

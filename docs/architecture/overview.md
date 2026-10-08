# Architecture overview

Orole OS is three layers plus a database. The rules below are binding (`AGENTS.md`).

```
+-------------------------------+   MyGo bindings (Go <-> JS)   +--------------------------+
| React UI  frontend/           | ----------------------------> | internal/app (desktop)   |
| Vite, TS strict, Tailwind v4, |   generated frontend/src/     | typed services, stateless|
| coss/Base UI kit, Zustand     |   mygo.ts                     | cmd/desktop              |
+-------------------------------+                               +------------+-------------+
                                                                             | named pipe + token
                                                                             | (internal/ipc)
                                                                +------------v-------------+
                                                                | orole-daemon cmd/daemon  |
                                                                | internal/daemon          |
                                                                |  SQLite (modernc)        |
                                                                |  terminals (ConPTY)      |
                                                                |  projects/git/github/... |
                                                                +--------------------------+
```

| Layer | Owns | Must not |
|---|---|---|
| UI (`frontend/`) | rendering, client-only state (theme, layout draft), preview mode in a plain browser | decide permissions, hold secrets, fake data |
| Desktop (`cmd/desktop`, `internal/app`) | window, bound services, launching/finding the daemon, desktop-side OS integration (autostart, diagnostics) | keep durable state |
| Daemon (`cmd/daemon`, `internal/daemon`) | SQLite, terminals, integrations, permission decisions, audit log | listen on a network interface |
| CLI (`cmd/orole-cli`) | control the daemon from a terminal via `internal/daemon/control` | bypass IPC auth |

## Why a separate daemon

The window can close, crash or restart while daemon-owned terminals and agents keep running. Both the
desktop app and the CLI are clients of the same daemon. See `docs/decisions/0002-daemon-separate-process.md`.

## Request flow

1. A component calls a generated binding, e.g. `Terminals.create(...)` (`frontend/src/mygo.ts`).
2. MyGo invokes the bound Go method in `internal/app`, which calls `Link.call` (`internal/app/bridge.go`).
3. `Link` holds one authenticated `ipc.Client` to the daemon and reconnects on demand.
4. The daemon method registered with `handle[P,R](d, "terminals.create", ...)` runs, writes to SQLite and the
   audit log when relevant, and may `IPC.Publish` an event.
5. The desktop process re-emits subscribed events to pages as typed MyGo events.

Desktop-only services (`app.System`: autostart, diagnostics) do not go through the daemon: registering the
daemon to start at logon is not something the daemon can do for itself when it is not running.

## Data and secrets

- Data directory: `%LOCALAPPDATA%\Orole` (override: `OROLE_HOME`). Contents: `orole.db` (+ `-wal`, `-shm`),
  `ipc.token` (only while the daemon runs), `logs/daemon.log` (+ rotated `.1`..`.5`).
- Secrets only in Windows Credential Manager through `internal/credentials` (`docs/architecture/credentials.md`).

## Untrusted content

Mail, issue text, MCP tool output and repository files are data, never instructions. `internal/untrusted` strips
control and bidi characters before such text reaches display and approval boundaries; `internal/policy`
is the central Go authorization gate for sensitive actions (`docs/security/threat-model.md`).

## Related

`daemon.md`, `ipc-protocol.md`, `windows-runtime.md`, `windows-terminals.md`, `database-schema.md`,
`design-system.md`, `integration-matrix.md`, `component-structure.md`, `security-permissions.md`, `credentials.md`.

# Daemon (`orole-daemon`)

Headless per-user background process. Entry point `cmd/daemon/main.go`; wiring in `internal/daemon`.

## Lifecycle

1. `main` resolves the data dir (`internal/paths`: `OROLE_HOME`, else `%LOCALAPPDATA%\Orole`), opens
   `logs/daemon.log` through `daemon.OpenRotating` (5 MiB per file, keeps `daemon.log.1`..`.5`, a write is never
   split across files) and builds a JSON `slog` logger (`-v` debug, `-stderr` also to stderr).
2. `daemon.New` opens `orole.db` (WAL, foreign keys on, busy timeout 5 s), applies pending migrations, creates a
   random token, the IPC server and the terminal manager, then `registerServices()`.
3. `Run` listens on the endpoint **first**; failing means another daemon owns it (single instance per data dir).
   Only then is `ipc.token` written (0600).
4. Shutdown (`daemon.shutdown` over IPC, or Ctrl-C): close listener, close client connections, terminate all
   terminals, delete `ipc.token`, close the DB.

The desktop app and CLI never start a second daemon: `control.Ensure` first asks the existing one
(`daemon.health`), and only launches `orole-daemon` (detached, no console window) if nothing answers.
`control.FindDaemonExe` looks next to the running executable, then on `PATH`; the packaged app passes the
resource path explicitly.

## Where services live

Each module is `internal/daemon/svc_<name>.go` with `func (d *Daemon) register<Name>()`, called from
`registerServices()` in `services.go`. Methods are typed with `handle[P,R](d, "name", func(ctx, P) (R, error))`.
Sensitive actions record `d.audit(action, target, outcome, detail)` into `audit_events`.

Core methods (`registerCore`):

| Method | Behaviour |
|---|---|
| `daemon.health` | version, pid, start time, uptime, DB schema version, platform, Go version, goroutines, heap MB, terminal count, protocol version |
| `daemon.shutdown` | replies, then stops ~100 ms later |
| `settings.list` / `settings.set` | opaque JSON values by string key (key <= 128 chars, value <= 256 KiB, valid JSON); `set` publishes `settings.changed` |
| `workspaces.list` / `save` / `delete` | named layouts (JSON, <= 1 MiB); save upserts, delete returns `not_found` for unknown ids |
| `terminals.*` | see `windows-terminals.md`; create is audited and publishes `terminal.changed` |

`registerServices` also wires automations, MCP/policy, declarative plugins, host metrics,
application shortcuts, Mail/Drive adapters, projects/Git, GitHub, machines, files and agents.
Notifications and search are registered through the integration service initializers.
These modules are reachable through authenticated IPC; unavailable configuration is reported
explicitly rather than replaced with mock data. See the module guides and `ipc-protocol.md`.

## Operating it

```
orole daemon start | stop | restart | status | logs [-n N] [-f] | diagnostics [-json]
orole daemon install | uninstall      # start at logon via Task Scheduler (see windows-runtime.md)
```

`diagnostics` prints app/Go versions, data dir, pipe name, daemon exe, daemon health, DB schema version
(from the daemon, else read-only from `orole.db`), whether the credential store round-trips, the WebView2
runtime version, autostart state and the last 20 log lines. The same report backs Settings > Diagnostics.

## Logging

JSON lines in `logs/daemon.log`. Secrets are never logged (`AGENTS.md`); credential store values are not read
by diagnostics beyond a throwaway write/read/delete probe key.

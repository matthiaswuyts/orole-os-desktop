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
| `terminals.*` | see `windows-terminals.md`; create is audited and publishes `terminal.changed` |

`registerServices` also wires automations, MCP/policy, declarative plugins, host metrics,
application shortcuts, Mail/Drive adapters, projects/Git, GitHub, machines, files and agents.
Notifications and search are registered through the integration service initializers.
These modules are reachable through authenticated IPC; unavailable configuration is reported
explicitly rather than replaced with mock data. See the module guides and `ipc-protocol.md`.

### Dictation (`svc_dictation.go`, `internal/dictation`)

| Method | Behaviour |
|---|---|
| `dictation.status` | `{ready, provider, maxSeconds, maxBytes}`: `ready` is whether an OpenAI API key is stored; no network call |
| `dictation.transcribe` | `{mimeType, dataBase64, durationMs}` -> `{text, model}`; sends one recording to OpenAI `/v1/audio/transcriptions` |

The desktop's `Dictation` service forwards these. The webview records with `MediaRecorder` (`audio/webm;codecs=opus`
where supported) and Chromium's Web Speech API is deliberately not used: WebView2 has no speech service behind it.
The daemon reads the OpenAI key from Credential Manager (`llm/openai/key`, the same entry as the AI providers page) for
every call; it never reaches the frontend, logs or SQLite, and is scrubbed from every error. Models: `gpt-4o-mini-transcribe`,
then `whisper-1` only when the account cannot use the first. Caps: 2 MiB of audio, 120 s (the frontend stops there; the daemon
rejects more than 125 s), 25 s upstream timeout, two recordings in flight. Neither audio nor transcript is logged or stored.
Every attempt is audited as `dictation.transcribe` on `dictation/openai` with the outcome, model and audio size, never the text.
Failures are IPC errors coded `dictation_<code>` (`not_connected`, `auth`, `quota`, `rate_limit`, `network`, `timeout`, `server`,
`rejected`, `too_large`, `too_long`, `no_audio`, `unsupported`, `busy`, `canceled`, `credentials`) with a user-ready message.
The MyGo WebView2 shell grants the microphone to the app's own pages by default (`Page.SetPermissionHandler` is not needed);
Windows can still block it under Settings > Privacy & security > Microphone, which the UI explains.

## Operating it

```
orole daemon start | stop | restart | status | logs [-n N] [-f] | diagnostics [-json]
orole daemon install | uninstall      # start at logon via Task Scheduler (see windows-runtime.md)
```

`diagnostics` prints app/Go versions, data dir, pipe name, daemon exe, daemon health, DB schema version
(from the daemon, else read-only from `orole.db` in the desktop app; the CLI reports it unavailable), whether the credential store round-trips, the WebView2
runtime version, autostart state and the last 20 log lines. The same report backs Settings > Diagnostics.

`orole-cli` is a thin IPC client: it must not link the daemon, sqlite, sftp/ssh or the MCP SDK. Wire types it needs
(`Health`, token path) live in the leaf package `internal/daemon/api`; `scripts/check-cli-linkage.sh` enforces this in CI.

## Logging

JSON lines in `logs/daemon.log`. Secrets are never logged (`AGENTS.md`); credential store values are not read
by diagnostics beyond a throwaway write/read/delete probe key.

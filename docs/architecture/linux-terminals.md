# Linux terminals and optional tmux persistence

`internal/terminals/backend_linux.go` implements the existing `Backend`/`Process`
contract with `github.com/creack/pty`. It has no ConPTY or Windows imports. The
Windows backend and existing default terminal creation calls remain unchanged.
This is terminal-backend support, not a claim that all desktop, agent, credential,
or packaging components build or run on Linux.

## Native PTY contract

- `StartSpec.Command` is a **POSIX shell command line** on Linux, executed with
  `/bin/sh -c`. Quoting, spaces, pipelines and redirections are interpreted by
  that shell; Windows command lines are not translated. Discovered bash, zsh,
  fish and sh commands launch interactive shells with safely quoted paths.
- `Cwd` is the child working directory. Nil `Env` inherits the daemon environment;
  an explicit environment replaces it. An absent/empty/dumb `TERM` defaults to
  `xterm-256color`.
- The child has a controlling PTY; stdin/stdout/stderr share the terminal.
  Resize updates the kernel window size; typed Ctrl-C reaches the foreground
  terminal job. Valid dimensions are 1–1000 columns/rows.
- `Wait` is repeatable and context-cancellable. Cancelling a wait does not kill
  the process. Nonzero process exits are reported as exit codes, not transport
  errors. `Close` releases the PTY and kills its root process exactly once.
- GUI reattachment uses the unchanged bounded 1 MiB daemon scrollback and byte
  sequence protocol. No output is persisted to disk by the terminal manager.

## Honest persistence

| Event | Native Linux / Windows ConPTY | Opt-in Linux tmux |
|---|---|---|
| GUI close, crash, reopen | Survives while daemon lives | Survives while daemon lives |
| Daemon shutdown or crash | No reconnectable session | Survives **only while the tmux server and child remain alive** |
| User logout | No guarantee | Depends on the host's user-service/session cleanup policy |
| tmux server killed | Not applicable | Sessions end; enumeration is empty |
| Reboot | No process restoration | No process restoration |

There is no tmux-resurrect integration, process checkpointing, replay-as-execution,
or reboot recovery. Detached programs may themselves finish or fail. An empty
session list is not a promise that previously launched work completed.

`Manager.Shutdown` closes native terminals but **detaches** tmux clients.
`Manager.Close(id)` is the explicit destructive action: it terminates that tmux
session as well as releasing its client. The process wrapper's `Close` closes
only the local tmux attachment, so normal pump cleanup cannot accidentally kill
persistent work. For tmux terminals, `Info.Pid` and `ExitCode` describe the local
**attachment client**, not the inner shell or its exit status.

## Real reconnect API

Persistence is explicit and defaults off:

1. Call `terminals.create` with `{"shell":"sh","persistence":"tmux"}`.
   Save the returned `persistentId`; `id` is the current daemon attachment ID.
2. After a daemon restart, call `terminals.persistent`. It enumerates actual live
   tmux session IDs, rather than saved launch specifications.
3. Call `terminals.restore` with `{"id":"<persistentId>","cols":120,"rows":30}`.
   The backend attaches a new PTY client to that existing session without
   relaunching its command. The response contains a new daemon attachment ID.
   Restoring an already-running local attachment returns that attachment.
4. Use the normal `terminals.attach|write|resize|close` methods with its daemon ID.

The bound `Terminals` service exposes `CreatePersistent`, `PersistentSessions`
and `Restore` in addition to the existing native `Create` method. CLI entrypoints
(where the daemon is supported) provide the same creation/discovery/reconnect:

```sh
orole terminal create -tmux -shell sh -cwd /tmp
orole terminal persistent
orole terminal restore <persistent-id>
orole terminal close <terminal-id>
```

A restored terminal receives tmux's current screen redraw. Previous daemon
scrollback, byte sequence numbers, title, shell selection, ownership metadata
and timestamps are **not** restored; shell state and tmux's own in-memory history
belong to the surviving tmux server. Restore reports a new attachment timestamp
and a generic tmux title. GUI reconnect without daemon restart still has the
ordinary daemon scrollback.

The server is isolated using a socket name derived from the absolute Orole home
(`OROLE_HOME`, otherwise the normal per-user data directory), not the user's
ordinary/default tmux server. Only Orole-prefixed UUID sessions can be restored
or terminated. Server configuration is `/dev/null`, excluding personal tmux
configuration. Distinct Orole homes use distinct servers. This is isolation of
application instances, not a security boundary against another process of the
same Unix user. No credentials or unrelated tmux sessions are read or killed.

Missing tmux, unsupported platforms, invalid IDs and attach failures are explicit
errors; they never silently degrade to daemon-lifetime sessions. A stopped tmux
server enumerates as an empty list. Only Linux supports the persistence option.

## Runtime prerequisites and regression commands

- Linux with mounted, usable `/dev/ptmx` and `/dev/pts` (including in containers).
- Go version from `go.mod`, `/bin/sh`, and standard `stty`/`sleep` utilities.
- Optional **tmux 3.2+** on `PATH`, `/usr/bin/env`, and a usable per-user tmux socket
  directory. Terminfo for `xterm-256color` is needed for attachment clients.
- Tests use temporary directories and dedicated UUID-named tmux servers; cleanup
  kills only those test-owned servers. No production data directory is needed.

```sh
go test -count=1 -timeout=90s ./internal/terminals
go test -race -count=1 -timeout=120s ./internal/terminals
```

The Linux tests exercise controlling-terminal IO, quoted Unicode/environment/CWD,
actual window size, Ctrl-C foreground interruption, exit code and repeated waits,
context cancellation, close/read unblocking, invalid starts, home isolation and
missing tmux. The tmux integration test shuts down one manager, enumerates from a
new one, restores the same live shell variable, resizes it, and explicitly kills
it. This is real PTY/tmux execution; tmux integration skips only if tmux is absent.
Cross-compilation alone does not exercise these behaviors.


A second tmux regression creates a terminal in a helper process, kills that
process without running shutdown, then attaches from a new manager and verifies
that the same inner shell state survived the simulated daemon crash.

## Observed acceptance

Ubuntu WSL with Go 1.27.1 ran the complete terminal package tests, including native PTY,
tmux manager restart and subprocess-crash persistence. A separate executable smoke
observed Unicode `NATIVE=unicode-π`, a `29 91` terminal resize, and
`RESTORED=survived-π` from the same shell variable after manager shutdown/reattachment.
Explicit close removed the owned persistent session. A tmux pane-target grammar bug
found during this run was corrected before the passing run.

Local Linux race execution was unavailable because this Ubuntu installation has no C
compiler; the Ubuntu CI lane installs tmux and runs the terminal package with `-race`.
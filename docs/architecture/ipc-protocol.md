# IPC protocol

Source: `internal/ipc/protocol.go` (types), `server.go`, `client.go`, `transport_windows.go`,
`transport_unix.go`. Protocol version: **1** (`ipc.ProtocolVersion`).

## Transport

| OS | Endpoint | Access control |
|---|---|---|
| Windows | named pipe `\\.\pipe\orole-daemon-<user SID>-<12 hex of sha256(lowercased data dir)>` | DACL `D:P(A;;GA;;;<current user SID>)` (protected, one ACE); no remote clients |
| Unix | socket `<data dir>/daemon.sock` | mode 0700 data dir, socket readable by the user only |

The data-dir hash lets one user run isolated instances (`OROLE_HOME`, tests, benchmarks) without colliding.
There is no TCP/HTTP listener.

## Framing

Newline-delimited JSON, one `Message` per line, at most `MaxMessageBytes` = 4 MiB per frame (enforced when
writing as well as when reading).

```
request : {"id":1,"method":"daemon.health","params":{...}}
response: {"id":1,"result":{...}}   or   {"id":1,"error":{"code":"not_found","message":"..."}}
event   : {"event":"terminal.output","data":{...}}
```

`Message` fields: `id` (uint64), `method`, `params`, `result`, `error{code,message}`, `event`, `data`.
Byte payloads (terminal input/output) are base64 strings via Go's `[]byte` JSON encoding.

## Session

1. The first frame on a connection **must** be `hello` with `HelloParams{minVersion, version, token, client}`.
   Unauthenticated peers get a 5 s window; any other method, malformed frame or bad token closes the connection.
2. The token is 32 random bytes (hex), generated per daemon run, compared in constant time, written to
   `<data dir>/ipc.token` (mode 0600) only after the daemon owns the endpoint, and deleted on shutdown.
   `control.NewClient` re-reads it on every dial so clients survive daemon restarts.
3. Reply: `HelloResult{version, daemonVersion, pid}`. If `minVersion > ProtocolVersion` the server answers
   `version_mismatch`.

## Built-in methods

| Method | Params | Result |
|---|---|---|
| `hello` | `HelloParams` | `HelloResult` |
| `subscribe` | `{topics:["*" \| "prefix.*" \| "exact.name"]}` | `{ok:true}` (client re-applies after reconnect) |
| `cancel` | `{id}` | `{ok:true}`; cancels the in-flight call with that id |
| `ping` | none | `{t: unix ms}` |

Every other method is registered by the daemon. Each call runs in its own goroutine under a 30 s default
deadline (`Server.CallTimeout`); a panic in a handler returns `internal` and is logged.

## Error codes

`unauthenticated`, `bad_request`, `not_found`, `canceled`, `timeout`, `version_mismatch`, `internal`, `denied`,
`too_large`; the client also synthesizes `disconnected` when the link drops.

## Events and backpressure

`Server.Publish(event, data)` sends to connections whose subscriptions match. Each connection has a bounded
outbound queue (`OutQueue` = 256 frames) and a 10 s write deadline; a subscriber that falls behind is closed
rather than allowed to block the daemon (it reconnects and re-attaches, e.g. terminals restore scrollback).
An event whose encoded frame would exceed `MaxMessageBytes` is dropped and logged (the connection stays up).

## Reply size and retries

The limit applies in both directions. A handler result that does not fit in one frame is answered with a
`too_large` error (error messages are truncated to 16 KiB) on the same connection. `Client.Call` reconnects
and re-sends only when the request was never written (not connected, or the write failed). Once a request has
been written, a lost link is returned as `disconnected` and the request is not re-sent, since the daemon may
already have executed it; the next call reconnects. The client closes its end of the connection whenever its
read loop stops, including after an oversized or unreadable frame.

## Daemon methods in this tree

Registered with `handle(d, name, ...)` in `internal/daemon` (see `daemon.md` for what each does):

`daemon.health`, `daemon.shutdown`, `settings.list`, `settings.set`, `workspaces.list|save|delete`,
`terminals.shells|list|create|write|resize|attach|close`, `projects.list|add|open|remove`,
`git.status|diff|log|branches`, `worktrees.list|create|remove`, `github.status|login|logout|clone|issue.create|issue.update|issue.comment`,
`files.providers|roots.list|roots.add|roots.remove|list|stat|read|write|mkdir|rename|delete|search`.

Events published: `terminal.output`, `terminal.changed`, `terminal.removed`, `settings.changed`.

## Compatibility rules

Additive changes (new methods, new optional fields) keep version 1. An incompatible change bumps
`ProtocolVersion`; the server accepts a client whose range contains the server's version.

## Chat and LLM methods

Typed contracts live in `internal/chat/api.go`, `internal/chat/memory_api.go` and `internal/llm/service.go`:
`chat.bots.list|get|save|archive|export|import`, `chat.messages.list`, `chat.send|cancel|regenerate|clear`, `chat.draft.send`,
`chat.attachments.begin|chunk|commit|discard|read`, `chat.tools.list`, `chat.memory.*`, `chat.knowledge.*`, `chat.skills.pending.*`,
`llm.providers.list|connect|disconnect|test`, `llm.models`, `llm.defaults.get|set`, `llm.openrouter.auth.start|finish`,
`llm.manifests.list|save|trust|remove`. Events: `chat.message`, `chat.message.delta`, `chat.run`, `chat.bot`, `chat.memory`, `llm.changed`.

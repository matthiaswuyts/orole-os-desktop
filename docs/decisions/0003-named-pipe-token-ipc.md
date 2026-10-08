# 0003: Named pipe plus token for local IPC

Status: accepted

## Context
UI and CLI must talk to the daemon without exposing a network port. Shipyard uses loopback HTTP with a token for its
app-to-agent API and Unix sockets elsewhere; Windows has no Unix-socket-with-ACL convention for this.

## Decision
Newline-delimited JSON over a per-user named pipe (`github.com/Microsoft/go-winio`) whose DACL grants only the
current user SID, plus a random 256-bit token written to a 0600 file after the daemon owns the pipe, required in
the first `hello` frame. Unix builds use a 0700-directory socket. Versioned protocol (`ProtocolVersion`).
Details: `docs/architecture/ipc-protocol.md`.

## Consequences
- No listener on any network interface; remote processes cannot connect.
- Two independent barriers (ACL, token). The token file protects against other processes running as the same
  user only weakly (same-user code can read it): same-user malware is out of scope (`docs/security/threat-model.md`).
- Simple to debug (text frames), bounded frames (4 MiB), bounded per-connection queues (slow subscribers are dropped).
- Hand-rolled protocol: we own cancellation, events and versioning rather than getting them from gRPC/HTTP.

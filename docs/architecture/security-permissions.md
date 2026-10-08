# Security and permissions

Principles from `AGENTS.md`: permissions are decided in Go; the UI is not a security boundary; secrets only in
the credential store; no network listener; external content is data.

## Local access control

| Boundary | Control | Code |
|---|---|---|
| Who can talk to the daemon | pipe DACL = current user SID only, protected; plus a per-run 256-bit token in a 0600 file, constant-time compare; `hello` first, 5 s deadline | `internal/ipc` |
| Network exposure | none: no TCP/HTTP listener anywhere | |
| Frontend to Go | MyGo bindings expose only methods of bound services; they hold no authority beyond what the daemon enforces | `internal/app` |
| Terminals created from the UI | `owner` and `command` fields are cleared; only daemon code can mark agent-owned terminals | `svc` `terminals.create` |
| Files | only inside approved, fully-resolved roots (`file_roots`) | `internal/files` |

## Decision engine (`internal/policy`)

Single place that answers "may this actor do this?". Inputs: actor, action, resource, effect
(read/write/destructive), optional `FromHint` (effect derived from untrusted server annotations).

- Match stored `policy_rules` (wildcards `*`; most specific wins; at equal specificity deny > ask > allow;
  `max_effect` limits a rule to requests not stronger than it) and `mcp_permissions`.
- Defaults: read -> allow, write/destructive -> ask, unclassified -> deny. Annotations from servers never unlock more than ask.
- `ask` parks the request (max 100 pending, 5 min timeout) until the UI resolves it; destructive actions cannot be remembered.
- Every decision is written to `audit_events`. Pending-approval text is sanitised with `internal/untrusted`.

**Wiring:** `internal/daemon/svc_mcp.go` exposes pending requests, resolution, and rule management over
authenticated IPC. The global `PolicyApprovals` surface subscribes before loading its initial snapshot.
MCP invocation uses `InvokeChecked`; Mail/Drive operations pass through the daemon adapters and policy.
The agent manager requires an authorizer for launch, restart, prompts, checks, and destructive worktree
discard, including calls from automations. Missing authorization fails closed. A retry of an already
dispatched agent key reads the existing result without launching again or requesting another approval.
CLI-reported tool approvals are also checked against policy before an Allow reaches the CLI.
This does not sandbox an agent's process: worktree separation is not OS isolation.

## Untrusted text (`internal/untrusted`)

`Text`/`Line` remove control characters, bidi override/isolate marks, zero-width and other invisible format
characters, invalid UTF-8, and truncate. This stops spoofing a prompt or a file name; it does **not** make
content safe to obey.

## Audit log

`audit_events(at, actor, action, target, outcome, detail)`. The daemon writes entries for sensitive actions
(`d.audit`), e.g. `terminal.create`. Do not put secrets or full command output in `detail`.

See `credentials.md` for secret handling and `docs/security/threat-model.md` for the threat model.

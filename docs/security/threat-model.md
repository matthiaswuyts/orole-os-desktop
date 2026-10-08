# Threat model

Scope: desktop shell, daemon, CLI, local SQLite, authenticated IPC, Credential Manager,
agents, integrations, automations, machine transport and declarative plugins as wired in this tree.

## Assets

| Asset | Where | Impact if lost |
|---|---|---|
| Secrets (GitHub, MCP/OAuth, SSH and automation credentials) | Windows Credential Manager | account takeover |
| Local files and repositories the user opens | disk; file roots in `file_roots`; worktrees | data loss, code tampering |
| Terminal sessions | daemon memory (shell processes, 1 MiB scrollback) | arbitrary command execution as the user |
| Audit log, settings, project registry | `orole.db` | loss of accountability / config integrity |
| IPC token | `ipc.token` (0600), memory | control of the daemon |

## Trust boundaries

1. **Other users / remote machines -> daemon:** no network listener; pipe DACL admits only the current user SID.
2. **Other processes of the same user -> daemon:** can read `ipc.token` and call IPC, i.e. can already run commands as the
   user. **Out of scope:** same-user malware is not defended against (stated plainly in ADR 0003).
3. **Frontend -> Go:** the webview content is our bundle (served by MyGo), but UI code is not a security boundary;
   every sensitive action is decided in the daemon. Terminal creation strips `owner` and `command` from UI requests.
4. **External content -> app:** mail, issue/PR text, MCP tool output and descriptions, file names and repository files
   (including `AGENTS.md`-style instruction files) are attacker-controllable data.
5. **Daemon -> external services** (GitHub API, MCP servers): HTTPS to user-configured endpoints; OAuth endpoints
   must be https (or loopback http) with no embedded credentials (`internal/mcp/oauth_discovery.go`).

## Threats and controls

| Threat | Control (today) | Gap |
|---|---|---|
| Local network attacker reaches daemon control API | named-pipe control API, no network-interface management listener | explicit OAuth callback and configured webhook listeners are separate, narrowly scoped endpoints |
| Another local user connects to the pipe | protected DACL, current user only | none known |
| Token guessing / replay | 256-bit random per run, constant-time compare, 5 s hello window, file deleted on exit | same-user read (above) |
| Secret leakage to disk/logs/UI | secrets only via `internal/credentials`; DB columns hold no tokens; diagnostics uses a probe key only | review discipline; no automated secret scan yet |
| Hostile text spoofs UI (bidi, control chars, invisible chars) | `internal/untrusted.Text/Line` at display/approval boundaries | sanitization does not make external content safe to obey; not a universal prompt-injection defense |
| Unapproved side effects by agents/MCP/automations | IPC-wired `internal/policy`; read/write/destructive defaults, audit, approval queue, fail-closed missing authorizer | approving a local CLI does not sandbox its process or guarantee that every CLI-internal action emits an approval |
| Path escape through files provider | operations only inside approved roots stored fully resolved (symlinks/junctions expanded) | tested in `internal/files` (as written by that slice) |
| Daemon DoS by a stuck client | bounded frames (4 MiB), bounded queues (256), write deadline 10 s, per-call 30 s deadline | no per-method rate limits |
| Tampered installer/update | updates require a signature that the embedded public key verifies; installer is **unsigned** | no code signing and no update keys configured yet (`docs/development/updates.md`) |
| Persistence abuse via autostart | per-user logon task, `LeastPrivilege`, runs only the daemon path chosen at install | task points at a fixed exe path; whoever can write that file can run code at logon (normal per-user install risk) |

## Prompt injection

Rule: **external content is data, never instructions.** Concretely, for every module that feeds text to an LLM or to
the user:

- Wrap external text as clearly delimited, labelled data in prompts; never concatenate it into the instruction role.
- Never let external text choose tools, targets, recipients or approvals. Tool selection and arguments that cause side
  effects go through `internal/policy.Check`; effects inferred from server-supplied annotations are hints and never
  unlock more than `ask`.
- Show approvals using sanitised text (`untrusted.Text`) with the actor, action and resource; the user approves the
  action, not a model-authored description of it.
- Repository instruction files are inputs the user opted into per project; they cannot widen permissions.
- Agent-owned terminals are created only by daemon code (`owner` set server-side), never by a UI or external request.

**Status:** the agent, MCP, automation and plugin services are registered in the daemon.
Go authorization and approval resolution are reachable through authenticated IPC, and
installed-app smoke exercised denied/approved launches and independent plugin trust/tool
approvals. These checks prove those boundaries, not immunity to arbitrary prompt injection.
Agent worktrees separate changes; they do not isolate credentials, the host filesystem or
the network from an approved process. Untrusted local agents require a separately reviewed
OS/container/VM boundary; no Windows Sandbox backend is claimed here.

### Repository review without implicit execution

Agent automatic statistics and manual review, project Git diff/status, and Git-file
listing annotations share `internal/git/review.go`. Review snapshots refs and the
index into a disposable Git directory, reads existing objects through alternates,
and writes new review objects only into that temporary directory. It imports native
normalization, binary classification, ignore/attribute data and upstream mappings,
not executable configuration. Repository clean/process filters, fsmonitor hooks,
index hooks, external diff/textconv drivers and lazy-fetch transports cannot run
through this boundary. Inherited Git configuration overrides are not forwarded.

Text/binary changes, untracked files, staged ignored files, linked worktrees and
split indexes remain reviewable without changing the real index. Generic status
and unstaged diff inspect initialized submodules recursively through the same
boundary rather than executing nested repositories' status commands; dirty
submodule indicators remain visible. Metadata overflow fails explicitly rather
than silently dropping changed files; large diffs retain the shared truncation flag.
External clean filters are deliberately not applied: review shows native Git
normalization of on-disk content, not arbitrary program output.

**Verification:** the integrated Windows `go vet ./...` and `go test -race ./...` passed.
Crafted-repository marker regressions are in `internal/agents/worktrees_security_test.go`,
`internal/git/review_security_test.go` and `internal/files/git_security_test.go`.
They cover automatic/manual agent review, real diff/status/listing results,
clean/process filters, fsmonitor, diff drivers, index hooks and nested dirty
submodules. This is regression evidence, not a claim that arbitrary approved agent code is sandboxed.

### MCP approval and credential boundaries

Direct MCP calls disclose a frozen JSON argument document and dispatch that same
document after approval. Calls fail closed if the complete server/tool/argument
detail cannot fit the 1,500-character disclosure limit, contains known session
credentials, or would change under display sanitization; a truncated prefix never
authorizes undisclosed arguments.

MCP auth sessions learn secret values when credentials are loaded, set and refreshed.
Tool results (including structured content and error blocks) and surfaced call errors
are scrubbed at that boundary before truncation or delivery to adapters, so automation
journals do not depend on a frontend or test registering MCP secrets. Retired values
remain in session memory to scrub responses already in flight during rotation.
OAuth token failures retain only HTTP status and recognized error identifiers;
unknown identifiers and provider descriptions are not reflected into diagnostics,
authorization events, logs or audit records. This is exact-value redaction, not a
claim to detect arbitrary encodings or transformations of secrets.

## Plugin trust

The implemented plugin system is declarative: a validated capability manifest contributes
fixed MCP actions, command entries and widgets. It never loads arbitrary React/JavaScript,
HTML or native plugin executables. Exact-digest trust requires a separate policy decision;
every invocation still checks the declared server/tool and MCP permission using the plugin
actor. Saving changed content clears trust; revocation/removal cancels active work. A remote
side effect already dispatched cannot be rolled back by cancellation.

Manifest arguments are plaintext configuration, not a secret store. Output is bounded,
sanitized text. See `docs/architecture/plugins.md` for validation limits, cancellation races,
runtime proof and the distinction between trusting a manifest and approving a tool call.

## Reporting

Open a private security advisory on the repository; do not post tokens or diagnostics JSON containing paths publicly
without reviewing them.

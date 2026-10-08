# Threat model

Scope: Orole OS as built in this tree: desktop shell, daemon, CLI, local SQLite, named-pipe IPC, Credential Manager.
Items marked **planned** describe controls for modules whose packages exist but are not yet reachable.

## Assets

| Asset | Where | Impact if lost |
|---|---|---|
| Secrets (GitHub PAT, future MCP/OAuth tokens) | Windows Credential Manager, `Orole OS/<key>` | account takeover |
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
| Local network attacker reaches daemon | no TCP/HTTP listener | none |
| Another local user connects to the pipe | protected DACL, current user only | none known |
| Token guessing / replay | 256-bit random per run, constant-time compare, 5 s hello window, file deleted on exit | same-user read (above) |
| Secret leakage to disk/logs/UI | secrets only via `internal/credentials`; DB columns hold no tokens; diagnostics uses a probe key only | review discipline; no automated secret scan yet |
| Hostile text spoofs UI (bidi, control chars, invisible chars) | `internal/untrusted.Text/Line` before display, audit and approval prompts | applies where adopted; **planned** for all integrations |
| Unapproved side effects by agents/MCP/automations | `internal/policy` (default read allow, write/destructive ask, unclassified deny; destructive never remembered; audit every decision) | **not yet wired behind daemon IPC**; no gated action exists in the UI today |
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

**Status:** the policy engine and sanitiser exist and are unit-tested; the agent runtime, MCP client and automations that
must call them are not yet exposed over IPC, so these controls are design + library today, not end-to-end behaviour.

## Plugin trust

There is no plugin system in Orole yet (**planned**). Shipyard's plugins are React code with the app's privileges and
Tailwind access (`docs/research/shipyard-analysis.md` section 15); copying that model would let a plugin do anything the UI
can. Requirements for any future plugin mechanism: separate process or sandbox, capabilities granted per plugin in
`internal/policy`, no access to the IPC token or credential store (the daemon brokers calls), signed or user-approved
installation, and audit entries naming the plugin as actor.

## Reporting

Open a private security advisory on the repository; do not post tokens or diagnostics JSON containing paths publicly
without reviewing them.

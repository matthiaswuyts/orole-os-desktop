# Terminals on Windows (ConPTY)

Source: `internal/terminals` (`terminals.go` manager, `backend_windows.go` ConPTY backend, tests in
`terminals_windows_test.go`). Library: `github.com/UserExistsError/conpty`.

## Model

The daemon's `terminals.Manager` owns every session; the UI is a view.

- `Backend` abstracts the platform (`Start(StartSpec)`, `Shells()`). Windows uses ConPTY;
  Linux uses a native PTY with opt-in [tmux persistence](linux-terminals.md). This does not
  make the entire desktop or credential stack portable to Linux/macOS.
- Shells detected on `PATH`/known locations: Windows PowerShell (`powershell`), PowerShell 7 (`pwsh`),
  Command Prompt (`cmd`), Git Bash (`git-bash`, from `%ProgramFiles%\Git`).
- Limits: 64 *running* sessions per manager; per-session scrollback of 1 MiB (`ScrollbackBytes`), a fixed
  ring of 64 KiB pages that is reused once full, so reading a flooding PTY allocates nothing.
- Exited sessions do not count against the 64. They stay attachable (final screen, exit code) for 30 minutes
  and at most 16 are kept; older ones are forgotten and the daemon publishes `terminal.removed` for them.
- Output is coalesced per session: the first bytes after an idle gap go out immediately (keystroke echo is
  not delayed), then at most one `terminal.output` event every ~12 ms, each at most 64 KiB, and at most
  ~2 MiB/s of event payload. `seq` is the stream offset (total bytes) at the end of the event, so an event
  covers `[seq-len(data), seq)`.
- Flood policy: a terminal cannot close the GUI's IPC connection. When more than 256 KiB is waiting to be
  delivered the oldest undelivered bytes are skipped (they stay in the scrollback), which a client sees as an
  event starting past the end of the previous one. The terminal view then calls `terminals.attach` again
  and resumes from the returned `seq`.
- `terminals.attach` returns the scrollback plus the current `seq`; live `terminal.output` events with
  `seq <=` that value are already included, so a reattaching window neither loses nor duplicates output.
  The session lock is held only to snapshot page headers; the ~1 MiB copy happens outside it.
- In-process consumers (agents) use `Manager.Wait(ctx, id)` to block until a session exits instead of
  polling `List`.
- `Close` kills the shell process and releases the pseudoconsole exactly once (`winProc.once`): closing a
  pseudoconsole twice corrupts the heap.
- `init()` calls `SetConsoleCtrlHandler(NULL, FALSE)` so Ctrl-C (`0x03`) works even if a parent started the
  daemon with Ctrl-C ignored (children inherit that flag).
- IPC methods: `terminals.shells|list|create|write|resize|attach|close`. `owner` and explicit `command` are
  stripped from UI requests; only trusted daemon code (including agents) may set them.

## Honest persistence

| Event | Terminals |
|---|---|
| Window closed / reopened / app crash | **Survive** (sessions live in the daemon; reattach with scrollback) |
| Daemon stopped (`orole daemon stop`, crash, log off) | **End** |
| Reboot | **End** (nothing is restored; there is no tmux equivalent on Windows) |

The UI and docs must not imply otherwise. Linux's optional tmux server can outlive a daemon,
but not a reboot; Windows has no equivalent persistent terminal mode.

## Measured echo latency

See `docs/performance.md` (ConPTY keystroke echo, inside the daemon, excluding IPC and xterm.js rendering).

## Known limits

- Scrollback is memory only, 1 MiB per session; it is not persisted to disk.
- Resizing and full-screen TUIs depend on ConPTY behaviour of the installed Windows build.

## Agent runtime and review

`internal/agents` owns coding-agent processes, sessions, permissions and transcripts in the daemon.
Migration `0020_agents.sql` adds `agent_sessions`, `agent_events`, `agent_journal` and `agent_runs`.
`internal/agents/orchestrate` implements send prompt, hand-off, review-with, loop-until and parallel
worktree attempts. The desktop's bound `Agents` service forwards requests; it owns no durable state.
Agent Dashboard and Review subscribe to the same persisted sessions through `stores/agents.ts`.

### Adapter capabilities

| Adapter | Launch and status | Resume / approval |
|---|---|---|
| Claude Code, structured | `claude -p --output-format stream-json --input-format stream-json --verbose --permission-prompt-tool stdio`; prompts and replies use stdin/stdout JSON, not terminal parsing. Per-session `--mcp-config` JSON registers the central `orole` gateway regardless of selected model; `--append-system-prompt-file` adds the enabled-skills index without putting JSON or large/multiline prompts on the command line. Native harness configuration is not modified. Completed thinking becomes reasoning; tool calls/results carry linked IDs, names, MCP server, status, input and bounded output | Saves CLI session ID; resumes with `--resume` and refreshed gateway/skills files. `--system-prompt-snapshot off` ensures current central skills replace the original prompt snapshot on resume. `can_use_tool` requests become Needs you; daemon validates the pending request, sends allow/deny and audits the decision |
| Claude Code, terminal | Interactive ConPTY with a per-session `--settings` file; SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Notification, Stop and SessionEnd hooks invoke `orole-daemon agent-hook` over authenticated IPC | Saves CLI session ID; resumes with `--resume`. Permission notifications say Needs you, but the user answers in the terminal, not a fabricated dashboard permission request |
| Oh My Pi (`omp`), structured | `omp --mode rpc --cwd <dir> --session-dir <session>/omp/sessions --approval-mode <mode> [--model m] [--thinking level]`; newline-delimited JSON on stdio (ready frame, protocol v2 `rpc_chunk` framing, `set_event_filter` delta projection). Completed thinking blocks, assistant text, tool calls/results and generated images become transcript items; `agent_end` closes the turn. A prompt sent mid-turn is queued as steering (`prompt` with `streamingBehavior: "steer"`). Model list comes from `omp models --json` (only models the user has credentials for), reasoning levels are `off`…`max` | Saves omp's session id; resumes with `--resume <id> --session-dir`. omp defaults to never asking, so Orole passes `--approval-mode`: default and `plan` → `always-ask`, `acceptEdits` → `write` (`plan` also narrows tools to `read,glob,grep,web_search,todo`; omp's `write` tool stays, behind approval). Approval `select` (Approve/Deny) and `confirm` dialogs become Needs you and are answered with `extension_ui_response`; any other dialog (free-form questions) is cancelled and noted in the transcript. The central `orole` MCP gateway and skills prompt are injected per session, never into the user's omp config or the repo: a generated extension package in the session directory (`package.json`, no-op `index.js`, `.mcp.json`) is loaded with `-e`, and `--append-system-prompt <file>` carries the skills index. Stop sends `abort`, closes stdin (omp drains and exits 0) and falls back to the job-object kill |
| Codex, structured (default) | `codex app-server` (JSON-RPC lines on stdio): `initialize`, then `thread/start` (`cwd`, `model`, `approvalPolicy`, `sandbox`, `developerInstructions` = enabled-skills index, and a per-thread `config` override that defines the stdio MCP server `orole` = the Orole gateway with `default_tools_approval_mode = "approve"`; `~/.codex/config.toml` is never read for this or modified) and `turn/start` per prompt (`effort` = chosen reasoning level, `summary: auto`). A prompt sent mid-turn is `turn/steer`; Stop sends `turn/interrupt`, closes stdin and kills the job. Completed items become transcript events: reasoning (summary + content), agent messages, `commandExecution` → tool `shell`, `fileChange` → tool `apply_patch` (changed paths and truncated diffs), `mcpToolCall` (server + tool), web search, and `imageGeneration` → an image event whose base64 result is validated by magic bytes and saved to `<data>/agents/<id>/images/<item id>.<ext>`. Streamed text only updates the activity line (throttled). Permission mode maps to Codex policy: default = `untrusted` + `workspace-write`, acceptEdits = `on-request` + `workspace-write`, plan = `on-request` + `read-only`. Model list and per-model reasoning levels come from `model/list` | Saves the Codex thread id; resumes with `thread/resume` (skills/gateway config is re-sent). Approvals are server requests: command, file-change (paths and diff shown from the started item; declined if Codex sent none), permissions, legacy `execCommandApproval`/`applyPatchApproval`, and MCP tool-call approvals (`mcpServer/elicitation/request` forms without fields) become Needs you with the exact request JSON; deny = `decline`. Forms that need typed input, URL elicitations and `item/tool/requestUserInput` questions are declined with a visible system note because the dashboard cannot collect answers yet |
| Codex, terminal | `codex [-m model] [prompt]` in ConPTY (opt-in second mode of the same kind) | Process running/exited only; approvals in terminal; no inferred CLI session ID |
| OpenCode | `opencode [--model model] [--prompt prompt]` in ConPTY | Process running/exited only; approvals in terminal |
| Gemini CLI | `gemini [-m model] [-i prompt]` in ConPTY | Process running/exited only; approvals in terminal |
| Custom CLI | User command via `cmd.exe` in ConPTY; `{prompt_file}` expands to a file containing the initial task | Process running/exited only; no automatic resume protocol. Include the placeholder if the command needs the initial task |

Terminal-mode sessions use `terminals.Manager` with owner `agent:<session-id>` and are inspectable in
the session's Terminal tab. Structured sessions deliberately have no terminal: their readable transcript
comes from the protocol. Terminal output is never regex-parsed to invent agent status. Long/multiline
initial terminal prompts use a task file; subsequent terminal replies are sent as one input line.
Detection reports missing binaries, version-probe failures and these capability differences honestly.
Only Claude's structured protocol/hook turns support loop-until's wait-for-turn behavior.

Each Windows agent gets a job object with `KILL_ON_JOB_CLOSE`. **Processes die with the daemon**, including
on a crash; GUI restarts do not affect them. A clean shutdown stops active work. After endpoint ownership
is established on the next start, persisted idle/working/needs-you sessions are marked **lost**, never
silently relaunched. Completed work stays done. Resume explicitly invokes the CLI session ID when
available; other adapters restart from the original task. The UI must not describe that as continuation
of a known OpenCode/Gemini/custom CLI conversation, or of a Codex terminal session (only structured Codex
sessions record a thread id). Image tasks (`images.start`) are structured Codex sessions in the `images` group
with a read-only sandbox in `<data>/images-workdir`; `agents.artifact` serves only image files inside the
session's own `<data>/agents/<id>` directory (symlinks resolved, traversal rejected) and downsizes images over
3 MiB to a JPEG preview because an IPC frame is 4 MiB.

### Durable orchestration and review

- Start requests insert a session's unique idempotency key before creating a worktree or process.
  Journaled operations insert pending before dispatch, then store their result. Pending rows from a
  previous boot become interrupted: an ambiguous outcome is not retried automatically. This is
  **at-most-once dispatch**, not exactly-once completion. Each parallel attempt has its own key/worktree.
- Loop-until runs user-selected checks under a bounded process-tree lifetime, passes failed output
  as explicitly untrusted data to the agent, waits for the next turn and respects the maximum rounds
  and cancellation. A daemon restart marks running orchestration interrupted.
- Automation provenance is persisted on sessions and propagated through `agent.finished` bus events,
  preserving cycle detection across agent turns and restart.
- Review compares against the starting commit, including untracked files, using a temporary index.
  It does not stage changes in the user's real index. Approve records a decision, **not a merge**;
  Send back delivers feedback; Discard removes only a worktree created for that session. Ordinary
  project-directory changes are not reverted by Discard. Run check records actual exit/output state.
  Worktree removal excludes the manager's manual/background Git readers, so an automatic
  diff cannot retain a Windows directory handle while Discard removes the worktree.
- Session transcripts are backed by bounded event history; CLI transcript paths are retained when
  provided by hooks. Structured permissions are transient: there is no live request to approve after
  the daemon process that owned it dies.
- Before honoring a structured Allow, the daemon evaluates central policy for `agent.tool.use`
  with actor `agent:<id>` and resource equal to the tool name. A Deny or policy lookup failure
  sends Deny to the CLI and is audited; the existing agent approval UI satisfies Ask without
  a second prompt. This covers CLI-relayed requests, not tools Claude's own rules already allow.
- Process launch, restart, prompts, check commands (including orchestration loops) and destructive
  worktree discard call the manager's required authorizer. The desktop daemon routes this through
  central policy with destructive effect: an explicit approval is required, stored denials are
  enforced, and a missing authorizer fails closed. Automation provenance selects the requesting
  automation actor; it is not authorization. Stopping an agent remains available without approval.

### Task branches and publishing

A task started with a new worktree gets branch `orole/<slug of the title, else of the prompt's first
line>`; when that name is taken a six-character suffix from the session id makes it unique (an
explicit branch is used as given and conflicts stay errors). `Session.BaseRef` is the commit the
worktree was created at and the worktree/branch are recorded before the process launches, so a crash
cannot orphan an unrecorded worktree.

`Agents.Publish(id, PublishSpec)` (`agents.publish`) turns the work into a commit, a pushed branch
and optionally a pull request, in the session directory, through `internal/git`:

1. **commit** — `git add --all` and commit with the user's configured `user.name`/`user.email`
   (an unconfigured identity fails with a message saying how to set it; nothing is staged). The message
   is `spec.message`, else the task title. A clean tree skips the step. Orole's Git wrapper disables
   `core.hooksPath`, so repository hooks do not run; `--no-verify` is never used or needed.
2. **push** — the checked-out branch to `origin` with upstream set, never forced. A detached HEAD, a
   missing `origin`, `main`/`master`/`develop`/`trunk`/... and the repository's default branch
   (`refs/remotes/origin/HEAD`) are refused before anything is committed.
3. **pull request** — against `spec.base`, else the remote branch the session's `BaseRef` sits on
   (the default branch when several do), else the default branch from `origin/HEAD` or the GitHub API.
   Title defaults to the task title, body to the task plus the last result. An existing PR (recorded
   on the session, or found by head branch after a lost response) is reused, so re-running publishes
   only new commits. `prUrl`/`prNumber` are stored in the session and in the `agent_sessions.pr_url`/
   `pr_number` columns (migration `0095`).

The first failing step stops the run and is named (`publish: push: ...`); earlier steps are not
rolled back and a re-run continues from them. Push and PR require `spec.confirmed` (the dialog's
explicit confirmation) and pass the GitHub gates described in `github.md` before the commit is made.
Each step appends a system event and an audit record, and the session is published as `agents.updated`.
Publishing is refused for remote-machine sessions, for sessions whose agent is mid-turn, and when
invoked by an automation.

Runtime IPC: `agents.detect/list/get/start/stop/restart/prompt/approve/events/transcript/remove/diff/review/check/hook`,
`agents.publish`, `agents.handoff/reviewWith/loopUntil/tryN/runs/cancelRun`. Live topics: `agents.updated`,
`agents.event`, `agents.removed`, `agents.run`.

### Verification and installed CLI evidence

`go test -race ./internal/agents` exercises a compiled scripted fake CLI: working → needs-you →
approve/deny → done, stop/process death, CLI-session resume, crash/error handling, lost recovery,
idempotency, real ConPTY ownership/hooks, temporary-index diff, hand-off, parallel worktrees and
bounded/cancelled loops. Fixtures live under `internal/agents/testdata`; production never uses them.

On 2026-10-08, this machine reported Claude Code **2.1.289** and Codex **0.156.1**. Claude's local
`--help` confirmed the structured flags and hooks/resume settings. The structured smoke invocation
produced an init event and a real authentication error: **OAuth session expired and could not be
refreshed**. No successful Claude model turn or real Claude permission round-trip is claimed.
OpenCode/Gemini were not found on PATH. Their terminal launch adapters remain capability-limited
as shown above.

A separate authenticated named-pipe smoke launched installed Codex through the Custom CLI adapter
in a daemon-owned ConPTY. The model reviewed an inline Go `Add` function with incorrect subtraction,
returned `return a + b`, and the session reached `done`. This proves real model execution through
the daemon, not file editing or the interactive Codex adapter. An earlier read-only attempt to
read the fixture file was blocked by Codex execution policy; no policy bypass was used. Both runs
used an isolated development home. After adding manager-level execution authorization, a fresh
daemon smoke observed the pending `agent.start` policy request, approved only that isolated launch
over IPC, and again received the correct real model result. Denied/missing-authorizer regression
tests passed under the Go race detector without creating a session or executing a check command.

The installed MyGo/WebView2 Home composer was also exercised with the built-in
Codex adapter: selected the registered smoke project, local box, new worktree,
custom model `gpt-6-astra`, and `low` reasoning; approved the daemon's
`agent.start` request. The real Codex terminal identified `GPT-6-Astra low` and
returned `return a + b`. Home's Running agents tab opened that session's terminal.
An earlier harness-default model was rejected by the account; model selection
does not imply account entitlement, and the application does not invent a model
catalog. This smoke proves prompt execution, not agent file editing.

After the structured Codex cutover, the opt-in `OROLE_LIVE_CODEX=1` smoke exercised
`codex app-server` directly: real file editing and command execution with approvals,
stop and `thread/resume`, and image generation producing a PNG read through the
artifact API. OMP also completed a real task through the daemon's central MCP
gateway, reading a registry-only skill. These headless checks supersede the older
terminal-only evidence above; the new React task surface has not been visually
checked. Claude, Codex and OMP model choices now come from their installed harnesses,
with per-model reasoning levels; discovery is not a guarantee of account entitlement.

Claude discovery sends only a stream-json `control_request` with subtype `initialize`;
it never sends a user message or starts a billable model turn. The matching
`control_response.response.response.models` catalog supplies each selector `value`,
`displayName`, optional `resolvedModel`, and optional `supportedEffortLevels`.
Versioned display names are retained; unversioned labels include the authoritative
resolved model ID when supplied. Selectors remain unchanged (including aliases),
and models without reported effort levels have no reasoning choices. Disabled
catalog entries are excluded.

Discovery uses a temporary working directory and `CLAUDE_CONFIG_DIR`, safe mode,
disabled hooks/tools/MCP configuration and session persistence, and disabled
auto-updates/nonessential traffic. It does not read or modify the user's native
Claude configuration or need their OAuth credentials. The CLI's built-in catalog
is therefore an installed-CLI inventory, not a signed-in account entitlement list.
Results are cached per executable for five minutes; initialization has a 12-second
deadline and its process group and temporary directory are cleaned up afterward.
Older CLIs without this protocol or its isolation flags leave the catalog empty
rather than using a guessed version list. `go test ./internal/agents -run 'TestClaudeModel'`
covers the metadata mapping, initialization-only request, cache, isolation,
process cleanup, failed initialization and cancellation.

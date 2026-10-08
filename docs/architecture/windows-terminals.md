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
- Limits: 64 sessions per manager; per-session scrollback ring of 1 MiB (`ScrollbackBytes`).
- Each output chunk gets a sequence number. `terminals.attach` returns the scrollback plus the current `seq`;
  live `terminal.output` events with `seq <=` that value are already included, so a reattaching window neither
  loses nor duplicates output.
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
| Claude Code, structured | `claude -p --output-format stream-json --input-format stream-json --verbose --permission-prompt-tool stdio`; prompts and replies use stdin/stdout JSON, not terminal parsing | Saves CLI session ID; resumes with `--resume`. `can_use_tool` requests become Needs you; daemon validates the pending request, sends allow/deny and audits the decision |
| Claude Code, terminal | Interactive ConPTY with a per-session `--settings` file; SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Notification, Stop and SessionEnd hooks invoke `orole-daemon agent-hook` over authenticated IPC | Saves CLI session ID; resumes with `--resume`. Permission notifications say Needs you, but the user answers in the terminal, not a fabricated dashboard permission request |
| Codex | `codex [-m model] [prompt]` in ConPTY | Process running/exited only; approvals in terminal; no inferred CLI session ID |
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
of a known Codex/OpenCode/Gemini/custom CLI conversation.

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

Runtime IPC: `agents.detect/list/get/start/stop/restart/prompt/approve/events/transcript/remove/diff/review/check/hook`,
`agents.handoff/reviewWith/loopUntil/tryN/runs/cancelRun`. Live topics: `agents.updated`, `agents.event`,
`agents.removed`, `agents.run`.

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

# Changelog

All notable changes to the Orole OS desktop app. Format follows [Keep a Changelog](https://keepachangelog.com).

Each release needs a `## X.Y.Z - YYYY-MM-DD` section matching `version` in `mygo.json`: the release
workflow refuses to build without it, and the in-app update window shows that section as the release
notes. See `docs/development/updates.md`.

## Unreleased

### Added
- Automations can start on app events: a task is created, changes status, waits for you, finishes or fails, a pull request is
  opened, a project is added, or a bot replies. Each event carries the task's title, project, branch, status and last message.
- Automation flows have condition lanes: a Condition block runs one lane or the other and the flow continues after it. New
  blocks: Ask a bot (the message lands in the bot's conversation; optionally wait for its reply), Start / Message / Wait for a
  code task, and Wait. Existing automations are upgraded automatically; a jump that has no lane equivalent is flagged and the
  automation will not run until it is rebuilt.
- Bots can run code tasks. Turn on "Runs code tasks" in a bot's panel and it can start a coding agent in one of your projects,
  check on any task, and message or stop the ones it started. You approve every start, bots never use YOLO, and the bot tells
  you in its chat when its task finishes, fails or needs you ("Code task update", with a link to the task).
- Automations can watch GitHub (new commits on a branch, pull requests opened or merged, failed workflow runs, new issues)
  and deployments of connected integrations such as Vercel or Railway. Orole checks every minute or two (it never opens a
  network port), starts from what already exists, and runs the flow once for each new item.

## 0.2.0 - 2026-10-12

### Added
- Right-click a task in the sidebar to rename it, pin it to the top of its project, or delete it. A name you give a task is final
  (automatic naming never replaces it); a running task is stopped before it is deleted.
- Code task conversations have a conversation map: a thin rail of ticks in the right gutter, one per turn. The turn being read is
  solid, turns on screen are darker, hovering shows the turn's title and the start of the reply, and clicking jumps there. It
  appears when the conversation is wide enough to leave a gutter and has more than one turn.
- New Code tasks get a real name. Oh My Pi and Codex name the task themselves (shown as soon as they do), and for Claude Code (and
  Codex until it names one) Orole makes one short request to the task's own Claude or ChatGPT account (Haiku, or Codex's smallest
  listed model) for a 3-6 word title, using the first prompts that say something. Until then the title is the first line of your
  prompt, a title you typed or changed is never replaced, and nothing is sent when the account is not installed and signed in. The
  request is audited without the prompt, and Settings > System > Task permissions > "Name tasks with AI" turns it off (names the
  harness chooses itself still apply).
- Starting a task opens its page instantly. Pressing Send in the Home or New task prompt switches to the task page at once, showing
  your message, the agent logo and a provisional title, and "Starting <agent>…" (or "Preparing workspace…" when a new worktree is
  being created), while the task launches in the background. The real task replaces it when ready; if the start fails, the page
  stays with the error, **Try again** and **Edit prompt** (which restores your prompt, files and choices). Leaving the page does
  not cancel the start, and the task appears in the sidebar as "Starting…".
- Code tasks have three permission modes, chosen before launch and switchable while the task runs: **Manual** asks before
  anything not known to be safe, **Auto** lets the agent decide which actions are safe, and **YOLO** never asks. Auto uses
  Claude Code's own auto mode and Codex's auto-review agent; Oh My Pi has no such mode, so Auto approves file edits there and
  still asks before commands (the picker says so). YOLO needs an explicit confirmation, can be turned off in Settings > System >
  Task permissions, is never available on remote machines, and Orole's MCP tool policy still applies in it. Switching applies
  immediately in Claude Code, from the next turn in Codex, and by resuming the conversation (while idle) in Oh My Pi; terminal and
  remote tasks cannot switch. Existing tasks keep working: stored `default` became Manual and `acceptEdits` became Auto.
- The Code composer is one rounded surface with a single toolbar: attach and permission mode on the left; model, dictation,
  context and send on the right. The folder and branch moved to a quiet line underneath.
- Dictation in the Code composer works in the desktop app. The WebView's Web Speech API had no speech service behind it
  ("Speech recognition could not reach its service"), so Dictate now records the microphone and the daemon transcribes the
  recording with OpenAI (`gpt-4o-mini-transcribe`, falling back to `whisper-1`) using the OpenAI API key you connected under
  Settings > AI providers. The consent text says the audio is sent to OpenAI; without a key, Dictate explains this and opens
  AI providers. Recordings stop at 2 minutes, the text lands in an editable draft and is never sent automatically, the key stays
  in Windows Credential Manager, and neither audio nor transcript is logged or saved. Each transcription is audited without its text.
- Settings > Integrations and AI providers open instantly: the last known connections, accounts, API-key providers and GitHub status
  show at once and refresh in the background, so the loading spinner no longer blocks the page. Listing the Claude, ChatGPT and
  Grok accounts also runs each tool's version, sign-in and update checks in parallel.
- App loading states use the Snake spinner from loading.dev. AI progress glyphs are unchanged.
- Bots can be pinned: "Pin to top" in a bot's menu in the sidebar keeps it above the others, marked with a pin; "Unpin" puts it back.
- Select text in a bot's reply and a small menu appears below it: Shorten, Improve or Explain. The bot gets the selected passage quoted
  (drawn as a quote in your message) with the request.
- Bots can use your Grok account: Grok models appear in the bot model picker once Grok is downloaded and signed in under Settings > AI
  providers > Accounts. Grok's sign-in state is read with `grok models`.
- Bots can use your Claude plan too: Claude models (Opus, Sonnet, Haiku) appear in the bot model picker once the Claude account is signed in.
  Text streams as it is written. Claude Code runs as a plain model with all of its own tools, settings and MCP servers switched off.
- Bots can use your ChatGPT plan: once the ChatGPT account is signed in under Settings > AI providers > Accounts, its models appear in the
  bot model picker. The Codex CLI runs as a plain model in an empty scratch folder; replies arrive whole rather than streaming.
- Settings > AI providers has an Accounts section for Claude, ChatGPT (Codex) and Grok: it shows which are installed, their version,
  whether a newer release exists and who is signed in, with Download, Update, Sign in and Sign out. Orole downloads the
  vendor's own CLI (hash-checked where published) and never sees the credentials. Providers now show their logos.
- Handing work to another bot no longer freezes the bot that handed it off: it says it has started and stays available
  (you can ask it for status), and the other bot's findings come back to it as a "reported back" card it then relays.
- Bots talk like coworkers: the system prompt now asks for short, direct, chat-sized replies, and a reply is drawn as
  several chat bubbles (one per paragraph) instead of one wall of text. Tables and charts stay components.
- Message queue: messages you send while a bot is busy wait in a queue above the composer. Cancel any of them, or pause
  the queue (the bot finishes its current reply, then holds) and resume it. Stop still cancels everything.
- Bot settings: Instructions and Title are editable, laid out as a contact card (Brain, Knows, Does). Memories open
  as a list you can prune; Skills, Tools and Integrations open as pages of their own.
- A reply that is still being produced always shows what the bot is doing: a shimmering line such as "Searching the web",
  "Reading a skill", "Thinking" or "Writing the reply", with the elapsed time after a few seconds. It replaces the bouncing dots, and
  covers the pauses between tool calls where nothing on screen used to move. The Code feed's activity line shimmers too.
- Skill proposals are now cards in the chat: the bot offers a skill, you read it and press Add. Adding saves the skill and
  gives it to that bot in one step, so there is nothing to approve or switch on in the bot settings.
- Built-in `skill-creator` skill for Chat bots: it teaches a bot to interview you, draft a skill and submit it with
  `propose_skill` for your approval. Built-in skills are for bots only and are never synced to coding agents.

### Changed
- CI also runs on pushes to `main` (so the Go, bun and NSIS caches are primed for pull requests) and is
  split into parallel `go`, `web` and `package` jobs; `release.yml` verifies Go and the frontend in
  parallel too. The frontend entry chunk size is checked against `scripts/bundle-budget.json`.
- Git-heavy tests in `internal/{git,agents,daemon,projects}` copy a repository built once per package
  (`internal/testrepo`) instead of running `git init` and `git commit` per test; the isolated ones run in parallel.
- Chat streaming no longer re-sends the whole reply every 50 ms: streamed text travels as small
  `chat.message.delta` events, with a full snapshot at tool boundaries, at least every 2 s and at the end,
  and the daemon builds the text in linear time. A 100 KB reply now publishes about 1 MB instead of 31 MB.
- Long conversations load only the text sent to the model, and the older backlog is summarised a page at a
  time. A failed summary is retried with a growing backoff instead of on every message.
- Sending, regenerating, clearing and delegating no longer hold the chat manager's global lock during
  database work.
- MCP gateway and bot tools no longer rebuild the tool index, re-query permissions or re-derive
  schemas on every call: the index is cached until a server is added, edited, enabled, disabled,
  removed, changes status or changes its tools. `gateway.tools` carries an ETag so the gateway
  process skips unchanged lists, and `mcp.status` is published only when a server's state changes.
- Automations: finished runs and their journal are now pruned (30 days and 500 runs per automation by
  default; `retentionDays` and `retentionMaxRuns` in the automations config, no settings UI yet).
  Queued, running and waiting-approval runs are never removed.
- Automations: run updates, queue scheduling and event dispatch no longer reload runs and definitions
  from SQLite for every step or event; the scheduler uses one timer that event bursts cannot postpone,
  and the webhook secret is read from the credential store once instead of per request.
- Agents: live sessions no longer write to SQLite on every event under the global lock. Hot-path
  changes are debounced per session and published as small `agents.delta` updates, events are
  inserted in batches, `agents.list` is paged, diff statistics use `git diff --numstat` coalesced
  per session, headless agent output lines are bounded and history pruning counts per session.
- Remote machine file operations reuse one SSH/SFTP session per machine (closed when idle, or when
  the machine's access is edited or the machine is removed) instead of reconnecting for every call,
  and remote file etags use size and modification time instead of downloading and hashing the file.
  Reading a remote file no longer downloads it twice.
- Fewer git processes per request: a status or diff now takes 4 to 5 `git` spawns instead of 16, and
  keeps git's stat cache by reviewing a copy of the real index. Ref-to-ref diffs skip the index.
- Git calls on a worktree look its directory up in the database instead of listing every worktree
  with a full status; worktree refreshes only write rows that changed.
- Directory listings in Git files run one status limited to the listed directory, shared between
  concurrent listings and reused for one second.
- `orole-cli` no longer links the daemon, sqlite, sftp/ssh or the MCP SDK: `daemon.Health` and the IPC
  token helpers moved to the leaf package `internal/daemon/api`, and `diagnostics.Collect` takes an
  injected offline schema reader. Offline `doctor` in the CLI reports the DB schema version as
  unavailable when the daemon is not running (the desktop app still reads it). CI guards this with
  `scripts/check-cli-linkage.sh`.
- SQLite: transactions take the write lock up front (`_txlock=immediate`), 4 idle connections,
  `temp_store`/`cache_size`/`journal_size_limit` pragmas, and `PRAGMA optimize` plus a WAL checkpoint on close.
  Migration 0101 adds a partial index on in-flight `messages.status` for startup recovery.
- Code task conversations read like a conversation: each assistant turn collapses into one line (`Working`, then
  `Worked for 2m 13s`) that opens into its thoughts and tool steps, while the final answer, pending approvals and questions,
  errors, files and images stay visible. Tool steps use plain words (`Ran npm test`, `Searched the web`, `Read main.go`,
  `Edited a.ts +12 -3`, `Used docs · search`) instead of raw tool names and JSON; the technical input/output is behind a
  disclosure in the row. Thoughts are no longer separate dashed boxes, and the per-row `Done` label and right-aligned
  timestamps are gone (hover a row for its time; day separators stay). Searching with `Ctrl+F` opens a run that holds a match.
- Tool permission requests are a compact card: what is being asked for in plain words (`Run a command`, `Edit files`,
  `Use <server>`), who is asking, and the full command, folder or file paths it covers, with `Deny` and `Allow once`. The
  review checkbox and the optional reason field are gone because the whole request is visible in the card; the raw JSON
  (control characters escaped) is behind `Details`, and a request without its complete input still cannot be approved.
  A decided request is a single `Allowed once` / `Denied` badge.
- Running image generations show a dot-grid placeholder with a shimmering `Generating` label and the prompt.
- Code task chrome is quieter: the titlebar now contains only the provider logo and task title. Stop, status,
  task-file and canvas-change controls no longer compete with the conversation; files still open from their feed cards.
- Conversation search stays hidden until `Ctrl+F`/`Cmd+F`, then opens as a compact panel in the top-right corner.
- Status bar: the MCP count opens Settings > Integrations (every "open settings" link in the app now opens the settings window), a connected
  daemon shows a green dot, and the host popover is a compact card with the OS logo, architecture, status and memory.
- Model picker: the company filter is a row of chips shown only for All and aggregators like OpenRouter (an account serves only its own
  models), the "Use default" entry is gone (a bot always shows and runs on a named model; new bots start on the default chat model), and the
  instructions box in bot settings stops growing at a maximum height. Choosing a provider shows all of its models, not only the handpicked
  ones (All still shows the handpicked ones, and a search reaches everything); the first row no longer carries a selection border.
- The model picker (bot settings and new bot) starts with a provider chooser: tiles for each connected provider, marked "Your account"
  (Claude, ChatGPT, Grok) or "API key" (OpenRouter, OpenAI), with logos. Models say who serves them ("via Claude") and the picker links to
  Settings > AI providers to connect more.
- The app window no longer shows the browser's right-click menu (except in text fields, for paste) and DevTools are off, including Ctrl+Shift+I
  and F12. Developers set `OROLE_DEVTOOLS=1` to get them back.
- Bot settings: the MCP tools and Skills dropdowns are plain switch lists. Integrations / MCP picks whole servers with their
  company logos (a bot can use every tool of a connected server except the ones you deny in Settings > Integrations);
  existing per-tool selections are migrated to their servers. Skills show only an icon and the name.
- Skills a bot proposes keep the name the bot chose (the bot-name prefix is gone), may have descriptions up to 1024
  characters, and a revised draft replaces the earlier one instead of being refused.
- The "Proposed skills" section of the bot settings panel is gone.
- Add now signs in right away (browser opens at the vendor's consent page). The connector details page is simpler: logo, Reauthenticate/Disconnect, tool chips and the server URL; OAuth client, permission levels and diagnostics moved under a closed Advanced section.
- Integrations and MCP servers are one Settings > Integrations page: company logos, one-click Add for Vercel,
  Cloudflare, Notion, Linear, Stripe, Netlify, Railway, Supabase, Neon, Prisma, Sentry, Zapier, Intercom, PayPal
  and Mixpanel, and a Custom connector button that needs only a name and a server URL.
- Faster startup: the Home view, home widgets, command palette and the settings window's pages now load
  on demand. The entry chunk drops from 929 kB to 592 kB (gzip 266 kB to 163 kB). Widgets show a
  per-widget loading card while their code loads.
- Only the Latin subsets of Inter and JetBrains Mono are bundled.
- Chat: a streaming reply no longer re-renders the whole conversation. The rail's preview of a reply
  updates at most once a second, the open conversation ignores preview and run-flag changes, bots and
  the run state are read once per thread, and composer drafts live in their own store.
- Window focus now triggers one shared refresh, at most every 30 seconds, instead of every store reloading
  on every focus. Chat messages refresh only the open conversation, and projects load their worktrees
  only while a view that shows them is open (cached for a minute), not on every project reload.
- Daemon health polling no longer notifies the UI when the status is unchanged, and the status bar,
  sidebar and shell subscribe to the fields they show, so the 5 second poll does not re-render them.
- Tasks: a streaming agent no longer re-renders the whole task feed. Events are grouped once for the feed and
  the canvas, unchanged messages and turns skip rendering, and conversations over 60 entries (messages and
  assistant turns) render only the entries near the viewport (search, "Load earlier" and following the newest
  message keep working).
- Tasks board: a session update re-renders only its own card, counts are computed in one pass, and only live
  sessions tick their elapsed time (a finished task now shows how long it ran, not time since it started).
- Task changes (diff) in Review and issue workflows refresh at most once every two seconds, never overlap, and
  keep showing the previous diff while refreshing.
- Startup no longer waits for the full task list; the Tasks view and Home activity show their own loading state.
- Home activity calendar no longer rebuilds or reformats its 365 days on hover or on every session update.
- Terminal: faster output decoding, and changing the font size no longer recreates the terminal.

### Removed
- Unused frontend dependencies: `@tanstack/react-query`, `cn`, `@fontsource-variable/dm-sans`,
  `@fontsource-variable/geist`.

### Fixed
- Hovering over a message, a run step or a note in a task conversation no longer pops up a native tooltip with its date. Dates
  stay on the day separators, and the system label on sidebar tasks lost its tooltip too.
- A model stream that goes silent for 90 seconds is cancelled with a clear error instead of hanging.
- Rate-limit (429) and server (5xx) answers are retried with jittered backoff before the reply starts,
  honouring `Retry-After`; a reply that already began is never replayed.
- Terminals: a program that floods its terminal (`cat` of a huge file, a runaway build log) no longer gets
  the desktop's daemon connection closed. Output is coalesced (about 12 ms, 64 KiB per event) and capped
  per session; when the window cannot keep up the oldest undelivered output is skipped and the terminal
  view re-syncs from the daemon's scrollback instead of the connection being dropped.
- Terminals: scrollback is a fixed ring, so a busy terminal no longer allocates and copies ~1 MiB per read,
  and attaching to a terminal no longer stalls its output while the scrollback is copied.
- Terminals: exited sessions no longer count toward the 64-session limit and are forgotten after 30 minutes
  (at most 16 are kept). Agents running in a terminal learn about exit through a notification instead of
  polling every 250 ms.
- IPC: a daemon reply larger than the 4 MiB frame limit now returns a `too_large` error instead of
  making the client drop the link, reconnect and re-send the request (which ran the call twice).
  Oversized events are dropped and logged, and `Client.Call` only retries a request that was never
  written to the connection.
- IPC: when a link is dropped (end of stream, or a frame the client rejects) the client now marks itself
  disconnected before it closes the socket, so nothing that sees the hang-up can still observe a live connection.
- Code projects load quickly again and no longer fail with "Could not load projects: context deadline exceeded". The sidebar
  used to wait for `git status` in every worktree of every project; now projects appear immediately, worktrees fill in per
  project, and one slow or broken repository cannot hide the rest. Worktree ahead/behind and dirty counts are read on the
  Worktrees page only (shown as "…" until ready).
- AI-generated text, errors and URLs can be selected and copied again in Bots and Code. The `selectable` class used in
  98 places was never defined, so only inputs and code blocks were selectable.
- Connecting services with large OAuth token sets (Cloudflare) no longer fails with "credential store: The stub
  received bad data": secrets over Windows Credential Manager's 2560-byte limit are now split across credentials.
- Startup no longer flickers: the window shows on first paint, the loading screen is part of
  `index.html` and appears before the JS bundle runs, and your theme is restored from a cache
  before first paint instead of flashing the default theme first.

## 0.1.0 - 2026-10-10

First tagged release. Windows only.

### Added
- Desktop shell (MyGo + React) in front of a separate headless daemon that owns durable state; the
  two talk over an authenticated per-user named pipe.
- Terminals (ConPTY: PowerShell, pwsh, cmd, Git Bash). Sessions survive closing the window but not
  the daemon exiting or a reboot.
- Projects with git status, diff, log and worktrees; GitHub repositories, issues, pull requests and
  checks through the existing GitHub CLI authentication.
- Local and Git files with an editor, notifications and search; command palette, themes, workspaces
  and layouts, guided first-run setup.
- MCP connections with a global approval queue, declarative MCP plugins, agent runtime with review,
  automations, machines and remote files over SSH, application launcher.
- `orole` command-line tool for the daemon, terminals and settings; optional start at logon.
- NSIS per-user installer and signed auto-update through GitHub releases.

### Known limitations
- Mail and Drive: setup UI is in place, but live authorization is blocked until a registered OAuth
  client ID is configured.
- Chat is not yet exercised against live provider APIs.
- External Windows and Linux SSH hosts are not yet verified.
- The installer and executables are not Authenticode-signed, so Windows SmartScreen may warn.
  Updates are signed with the MyGo update key and verified by the app.

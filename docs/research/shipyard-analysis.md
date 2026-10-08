# Shipyard source analysis (for Orole OS)

Source inspected: `C:/code/projects/_upstream/shipyard` (origin `https://github.com/cosscom/shipyard`),
HEAD `c809c5eccfc31a386be6bb82b8b0839eaf7ec89b`, MIT, © 2026 Sean Brydon. All paths below
are relative to that repo root unless prefixed `orole:`. Claims were checked against the
files cited; anything not directly read is tagged **[UNVERIFIED]** or **[INFERENCE]**.

Shipyard (formerly "Berth"; identifiers `berth`, `berthd`, `~/.berth`, `BERTH_*`,
`@berth/plugin` are kept — `AGENTS.md`) runs coding agents on remote "boxes" and shows them
in a desktop app. Go module `github.com/cosscom/shipyard`, `go 1.27.0`, one direct
dependency `tailscale.com v1.102.3` (`go.mod`).

## 1. Process model (three pieces + app)

| Piece | Where | What | Source |
|---|---|---|---|
| `berthd` | each box (Linux/macOS) | locations, worktrees, tmux sessions, services, hooks, flows, kits, event journal | `cmd/berthd`, `internal/box` |
| `berth` agent/CLI | the laptop | one pinned TLS connection per box; loopback UI API (:1378), `*.localhost` proxy (:1377), phone (:1379); offline prompt queue | `cmd/berth`, `internal/agent`, `internal/proxy` |
| Desktop app | the laptop | Tauri 2 shell + React UI; "only a view", holds no connection | `app/` |
| Plugins | app + sometimes box | React plugins on `@berth/plugin` | `plugins/`, `packages/plugin-sdk` |

`docs/concepts/architecture.mdx`: app → agent via loopback HTTP+WebSocket guarded by a token
(`app/src/lib/api.ts`: "The app is only a view: the laptop agent holds every box connection
and serves this API on loopback"); agent → box via TLS 1.3 HTTP/2. The local agent API is on
a **Unix socket** (`internal/agent/agent.go:346`, `net.Listen("unix", cfg.Socket)`), and
berthd's local API on the box is a Unix socket too (`cmd/berthd/main.go:448`).

**Implication for Orole:** Shipyard's architecture is "remote boxes + local agent". Orole OS
is a Windows-native *local* environment with a headless daemon (`orole-daemon`). The
agent/box split collapses into a single local daemon; the boxed-remote half becomes an
optional later "remote machine" feature (§11).

## 2. Frontend layout (`app/`)

- Stack: React 19 types, Vite, Tailwind 4 (`@import "tailwindcss"` in `app/src/index.css`),
  Zustand stores, `@base-ui/react ^1.8.0`, coss ui (shadcn-style registry), lucide,
  CodeMirror 6, `@pierre/diffs`, `ghostty-web ^0.4.0`, `@xterm/xterm ^5.5.0`,
  `@tanstack/react-virtual`, `@number-flow/react 0.6.2` (`app/package.json`).
- Entry: `app/src/main.tsx` → `app/src/App.tsx` (280 lines). `App.tsx` composes
  `AppSidebar`, `TabStrip`/`HomeTabs`, `PaneLayer`, `Launcher`, `CommandPalette`,
  `NotificationCenter`, `LoopsPanel`, `StatusBar`, `ToastProvider`, `TooltipProvider`,
  `PluginConsentDialog`, `WhatsNewDialog`, etc.
- Other `app/` dirs: `app/src-tauri` (Rust shell), `app/e2e` (Playwright on mock fixtures),
  `app/perf`, `app/scripts`, `app/public`.
- Browser/dev-mode mocks: `app/src/lib/mock-*.ts` and `app/src/demo/` let the UI run without
  Tauri or a box (`__BERTH_DEMO__` in `app/vite.config.ts:141`).

### 2.1 Component hierarchy (`app/src/components`)

| Dir / file | Role |
|---|---|
| `ui/` (52 files) | coss ui / base-ui wrappers: `button`, `dialog`, `sheet`, `menu`, `tabs`, `sidebar`, `toast`, `command`, `table`, `select`, `combobox`, `tooltip`, `scroll-area`, etc. The design-system layer; shadcn-style `components.json` |
| `workspace/` (20 files) | The tab/pane workspace: `tab-strip`, `tab-group`, `tab-drag`, `pane-layer` (flat keyed pane list so terminals never remount), `pane`, `pane-state`, `terminal-view`, `service-terminal`, `log-view`, `panel-pane`, `compare-view`, `compare-sync`, `launcher`, `new-tab-menu`, `run-menu`, `home-tabs`, `worktree-picker`, `worktree-sections`, `worktree-tone`, `zen` (zen mode + `FakeTrafficLights`) |
| `conversation/` (31 files) | Agent chat UI: `conversation-view`, `conversation-pane`, `agent-message` (+css), `chat-list`, `composer-dialog`, `composer-options`, `composer-pickers`, `command-menu`, `markdown`, `edit-diff`, `todo-card`, `question-form`, `subagent-view`, `helper-pane`, `live-screen`, `report-card`, `crew-card`, `chat-background`, `chat-search`, `attachments`, `artifacts` |
| `orchestrate/` | Only two files: `loops-panel.tsx`, `session-actions.tsx` (orchestration logic lives in `app/src/lib/orchestrate*.ts`, `loops.ts`) |
| `browser-pane.tsx` | **single file** (not a dir): native child webview over the pane; iframe fallback; URL bar, suggestions, picker, devtools toggle. Companions: `browser-devtools.tsx`, `browser-sandbox.tsx` |
| `preview-pane.tsx` | **single file**: same page at several device sizes at once (`PaneContent` kind `"preview"`, `app/src/lib/layout.ts`) |
| `sidebar/` | `nav.tsx` (customize sheet), `projects.tsx`, `rail.tsx` (folded rail), `row-layer.tsx`, `resize-handle.tsx`, `rename-worktree.tsx`, `add-to-box-dialog.tsx`, `actions.tsx`, `confirm.tsx`, `box-load.ts`, `autofix-items.tsx` |
| `art/`, `art/harbour/` | Artifacts board/viewer and the Harbour painting assets (`dawn/day/dusk/night.webp`) |
| `charts/` | ~100 files, a self-contained chart library (area/bar/line/pie/ring/gauge/funnel/sankey, visx-based) used by `box-monitor`/usage |
| `diff/`, `files/`, `editors/` | `@pierre/diffs` worker renderer; file tree + CodeMirror editor (`files/code-editor.tsx`); external-editor opener |
| `new-worktree/`, `add-project/` | worktree creation (smart input, PR/branch resolution) and project add/clone/create flows |
| `notifications/`, `queue/`, `prompts/`, `skills/`, `whats-new/` | notification centre, offline-queue UI, saved prompts, skills panel, release notes |
| top-level | `app-sidebar.tsx` (238 lines), `command-palette.tsx`, `status-bar.tsx`, `box-picker.tsx`, `agent-glyph.tsx`, `pixel-loader.*`, `error-boundary.tsx`, `shortcuts-sheet.tsx`, `repo-trust.tsx`, `plugin-consent-dialog.tsx`, `upgrade-box.tsx` |

### 2.2 Views (`app/src/views`)

`home/` (Home = Harbour widget grid), `dashboard/` (Agent Dashboard, Labs), `review/` (inbox
of finished agent work), `worktrees/` (table + commit graph + bulk actions + sync),
`automations/` (hooks, flows, events), `project/`, `kits/`, `team/`, `onboarding/` (add-box,
SSH setup, tailnet), `settings/` (appearance, terminal, notifications, plugins, phone,
shortcuts, developer, about…), `plugin-screen-view.tsx`, `view-header.tsx`.

## 3. Theme architecture

1. **Semantic CSS variables** (coss tokens) in `app/src/index.css` (395 lines): `:root` and
   `.dark` blocks define `--background --foreground --card --popover --primary --secondary
   --muted --accent --border --input --ring --success --warning --info --destructive` (+
   `-foreground`), `--sidebar*`, `--chart-1..5`, `--code*`, `--radius: 0.625rem`.
   A Tailwind-4 `@theme inline { --color-X: var(--X) … }` block maps them to utilities;
   radii derive from `--radius` (`--radius-sm … -4xl`). Fonts: Inter Variable +
   JetBrains Mono Variable via `@fontsource-variable`. `@custom-variant dark (&:is(.dark *))`.
2. **Runtime theme application**: `app/src/themes/apply.ts#applyTheme` writes the same
   variables as inline properties on `<html>` (beats the stylesheet), toggles `.dark`, sets
   `color-scheme`, `--terminal-background`, optional `--info/--link/--selection`, and per-theme
   **worktree tone** colours `--wt-<tone>` (`themes/tones.ts`, OKLab-based contrast-checked).
   Also exports `syntaxThemes` (Shiki pair for diffs) and `xtermTheme` (terminal palette).
3. **Theme catalogue**: `themes/builtin.ts` (4 own: `berth-dark` "Shipyard Dark",
   `berth-light`, `midnight`, `paper`) + `themes/vscode.ts` (1172 lines, 19 VS Code ports:
   Dracula, One Dark/Light, Catppuccin, Tokyo Night, Nord, GitHub ×3, Gruvbox ×2, Solarized ×2,
   Rosé Pine ×2 — credited in `THIRD_PARTY_THEMES.md`). Contrast gates: `themes/contrast.ts`,
   `contrast.test.ts`, `pnpm check:themes`. User themes: `~/.berth/themes/*.json` via agent,
   plugin themes via `addTheme`. Theme shape is `Theme` in `packages/plugin-sdk/src/types.ts`.
4. **Density tokens**: `:root { --row-h:2.25rem; --row-pad:.5rem; --row-gap:.5rem;
   --side-row:1.75rem }` and `:root[data-density="comfortable"]` (2.75rem/.75rem/.75rem/2rem),
   exposed as Tailwind spacing `h-row`, `py-row-pad`, `gap-row-gap`, `h-side-row`
   (`index.css`). TS side: `app/src/lib/density.ts` (graph row px: 28 compact / 36 comfortable);
   pref in `app/src/lib/prefs.ts` [UNVERIFIED contents — file referenced by `density.ts`].
5. **Motion**: `:root[data-berth-still]` kills spin/ping/pulse/shimmer (`index.css`,
   `app/src/lib/still.ts`).

**Orole reuse:** the whole token → `@theme inline` → `applyTheme` pipeline is pure CSS/TS and
platform independent. Rename `--wt-*`/`data-berth-*` identifiers; replace the `Theme` type
import from `@/lib/api` with an orole-local type.

## 4. Sidebar & navigation

- `app/src/components/app-sidebar.tsx`: "a short list of places at the top, then the
  repositories on every box, then Settings"; folded `Rail` (⌘\) at 76px to clear macOS traffic
  lights.
- Place arrangement is data: `app/src/lib/nav.ts` — `NavLayout {pinned, more, hidden}`,
  `DEFAULT_PINNED = ["home","review","worktrees","automations"]`, versioned `migrate`,
  persisted through the laptop agent at `GET/PUT /v1/app/sidebar` (stored in
  `~/.berth/app/<key>.json`, `internal/agent/appstate.go`). Plugin sidebar items land in More.
- Width: `lib/sidebar-width.ts` (+ test); `sidebar/resize-handle.tsx`; ui primitive
  `components/ui/sidebar.tsx`.
- Projects tree: `sidebar/projects.tsx`, grouping model `lib/groups.ts` (+ Go twin
  `internal/groups`).
- Command palette: `components/command-palette.tsx`, `lib/commands.ts`,
  `lib/palette-shortcuts.ts`, shortcuts table `lib/shortcuts.json` + `hooks/use-shortcuts.ts`
  (this hook imports `@tauri-apps/api/window`; Mac ⌘ vs Ctrl mapping in `lib/platform.ts`).

## 5. Dashboard / Harbour widget system

- Home = a widget grid: `app/src/views/home/home-view.tsx`, `views/home/widgets/*`
  (`grid.tsx`, `card.tsx`, `picker.tsx`, `registry.tsx`, `agents.tsx`, `fleet.tsx`, `git.tsx`,
  `env.tsx`, `parts.tsx`, `harbour.tsx`+`harbour.css`).
- **Data model** (pure, tested): `app/src/lib/home-layout.ts` — sizes `s(1×1) m(2×1) t(1×2)
  l(2×2) w(4×1)`; 4/2/1 columns by window width, fixed row height, dense packing;
  `HomeLayout {version, items: {id,size}[]}`, `DEFAULT_ITEMS` (needs-you, working, finished,
  `pull-request/prs`, git, boxes, services, areas, `pull-request/ci`, `notes/note`);
  stored in prefs (`Prefs.home`). Test: `lib/home-layout.test.ts`.
- **Registry**: `registry.tsx` `WidgetDef {id,title,description,icon,category,sizes,source,
  plugin?,bare?,useCount?,Component,wrap?}`; categories Agents/Work/Code/Fleet/You;
  `BUILTIN_WIDGETS` + plugin widgets (`berth.addHomeWidget`, ids `<plugin>/<id>`) merged via
  `useRegistry` (`plugins/registry.ts`). Card chrome (heading, menu, error boundary) is the
  grid's.
- **Harbour**: `harbour.tsx` draws a painting (`components/art/harbour/{dawn,day,dusk,night}.webp`,
  1672×941, lamp/horizon constants `PIC`) with a CSS-animated boat per working agent, waiting
  agents by the lighthouse, finished ones on the shore; time-of-day via `art/harbour-art.ts`
  `useHarbourLight`; reduced-motion aware.
- Separate "Agent Dashboard" (`views/dashboard/`, Labs): `agent-card.tsx`,
  `use-screen-tail.ts` (polls agent screens).
- Data feed: `lib/widget-data.ts`, `lib/state-model.ts`, `lib/derive.ts` over events from the
  agent (`lib/events.ts`).

**Art assets caveat:** the `.webp` paintings and `art/` scenes have no separate attribution in
`THIRD_PARTY_NOTICES.md`; they are the repo's own (MIT as part of repo) **[UNVERIFIED
authorship — confirm before shipping in Orole]**. Prefer generating Orole's own art.

## 6. Workspace layouts (tabs / splits / worktree-keyed state)

- `app/src/lib/layout.ts` (268 lines): a tab's panes are a **binary tree of splits**
  (`PaneNode = leaf | split{dir,ratio,a,b}`), pure functions. `PaneContent` kinds: `terminal`,
  `browser`, `preview`, `log`, `file`, `helper`, `panel` (plugin), `artifact`, `starting`,
  `empty`, `error`. Panes may belong to a worktree other than their tab's (`wt`, `adopt`,
  `mixed`).
- `app/src/lib/workspaces.ts`: "A workspace is one worktree's tabs, as in Orca"; keyed by
  `box:path`; persists layout on the laptop; actions `openTerminal`, `openBrowser`,
  `focusSession`, `selectWorktree`; tab groups (Labs); "box home" workspaces keyed `box:~`.
- `components/workspace/pane-layer.tsx`: one flat list keyed by pane id, positioned from the
  split trees — this is what keeps terminals/browser webviews alive across tab moves.
- Dragging: `tab-drag.tsx`; compare mode: `compare-view.tsx`, `compare-sync.tsx`,
  `lib/compare.ts`; zen: `zen.tsx`.

## 7. Terminal integration (the critical Unix coupling)

**Renderer (portable):** `app/src/lib/terminal.ts` — `ghostty-web` (Ghostty VT parser in WASM,
canvas) by default, `@xterm/xterm` (+fit, web-links) as fallback, both lazily imported;
custom `drawOnDemand` render loop, wheel batching (`lib/wheel.ts`), predictive echo
(`lib/predict-overlay.ts`), health (`lib/terminal-health.ts`). View:
`components/workspace/terminal-view.tsx` (480 lines; connection states
connecting/open/reconnecting/offline/ended; key hold during reattach 10 s / 4096 B).
CSP needs `'wasm-unsafe-eval'` for ghostty (`app/src-tauri/tauri.conf.json`).

**Transport:** app ↔ agent over WebSocket (`internal/agent/ui.go`, `github.com/coder/websocket`;
token goes in the query because a browser can't set WS headers, `ui.go:131`), agent → box
`/v1/sessions/{session}/attach?cols&rows` (`internal/agent/ui.go:325`). Input frames
`[type u8][len u16][payload]` with `FrameData=0`, `FrameResize=1` (`internal/terminal/frames.go`);
output is raw bytes.

**Process hosting (tmux + Unix PTY):**
- Every agent/terminal is a **tmux session** in a private server: socket name `berth`
  (`internal/box/sessions.go:144 const tmuxSocket = "berth"`), invoked as
  `tmux -u -L berth -f <config> …` (`internal/box/tmux.go:98`). tmux is searched on PATH then
  `/opt/homebrew/bin`, `/usr/local/bin`, `/home/linuxbrew/.linuxbrew/bin`, `~/.local/bin`
  (`tmux.go:19`). Doctor fails without it: "Shipyard runs every agent in tmux"
  (`tmux.go:237`).
- Commands used: `new-session -d -s -c -x 200 -y 50`, `set-option` (session metadata stored as
  tmux user options `@berth_title`, `@berth_agent`, `@berth_service`…), `list-sessions -F`
  (tab-separated), `kill-session -t =name`, `capture-pane -p -J -S -N` (screen scraping for
  dashboards), `send-keys -l`, `paste-buffer -p -d -b` (multi-line prompt injection),
  `attach-session` (`sessions.go:326-489`, `orchestrate.go:37-65`).
- Attach = run a tmux client under a pseudo-terminal and relay the master:
  `terminal.Start(cmd, cols, rows)` (`internal/terminal/pty.go`) using `syscall.SysProcAttr
  {Setsid, Setctty}`, `TIOCSWINSZ`, `syscall.Termios` raw mode; PTY opened by
  `pty_linux.go` (`/dev/ptmx`, `TIOCSPTLCK`, `TIOCGPTN`) and `pty_darwin.go`. **No
  `pty_windows.go` and no `*_windows.go` file exists anywhere in the repo** (`find . -name
  "*_windows.go"` → none), so `internal/terminal` and most of `internal/box` do not build on
  Windows **[INFERENCE from missing build files + use of `syscall.Setsid`, `syscall.Termios`,
  `syscall.Kill`, none of which exist in Windows `syscall`]**.
- Durability: sessions outlive the daemon because tmux owns them; per-session cleanup uses
  systemd transient scopes (`systemd-run --user --scope`) on Linux (`internal/box/scopes.go`).

## 8. Browser panes

- **Native child webview** laid over the React pane rectangle (`app/src-tauri/src/browser.rs`,
  367 lines: Tauri `Window::add_child`, labels `browser-<id>`, commands for
  create/place/navigate/reload/eval/inspect/pick/close, console watching; `linux.rs` for
  WebKitGTK placement; `menu.rs` macOS menu). React side: `components/browser-pane.tsx` invokes
  these through `@tauri-apps/api/core`; mode `"native"` when `isTauri()` else `"iframe"`.
  Capability file states browser panes get **no** Tauri permissions
  (`app/src-tauri/capabilities/*.json`).
- Page picker (`lib/picker.ts` `PICKER_SCRIPT`) → sends element context to an agent;
  devtools feed via proxy-injected `internal/proxy/devtools.js`/`preview.js`;
  reload-loop detector `lib/page-loads.ts`; URL resolution `lib/browser-url.ts`.
- Private URLs: the laptop agent serves `http://<worktree>.<location>.<box>.localhost:1377/`,
  streaming to box ports over the pinned connection (`internal/proxy/proxy.go`,
  `docs/concepts/laptop-agent.mdx`).
- **Agent browser**: headless Chromium on the *box*, one per worktree, driven over
  `--remote-debugging-pipe` (fd 3/4, NUL-framed JSON) with accessibility snapshots
  (`internal/box/browser.go`, `cdp.go`, `browsersandbox.go`, `axsnap.go`); Linux
  AppArmor/userns handling is Linux-only.
- Preview pane (`components/preview-pane.tsx`): multi-viewport grid reusing `BrowserPane`.

## 9. Agent conversation views

Not a protocol client: the box **reads agents' own transcripts** and serves structured turns.
- `internal/transcript/{claude,codex,locate,turn,questions,report,signals}.go` — locate and
  parse Claude Code (`CLAUDE_CONFIG_DIR` or `~/.claude`) and Codex (`CODEX_HOME`) JSONL;
  served by `internal/box/transcriptapi.go`, watched by `transcriptwatch.go`.
- Agent state (idle/running/waiting/finished) arrives from **hooks the agents call**
  (`internal/integrations`, `adapters/`; installed into Claude Code/Codex/Cursor configs),
  falling back to screen scraping (`Fidelity: hooks|partial|screen`, `sessions.go`).
- UI: `components/conversation/*`, `lib/transcript.ts`, `lib/transcript-feed.ts`,
  `lib/agent-messages.ts`, `lib/conversation-store.ts`; per-pane toggle `view: "terminal" |
  "conversation"` (Labs). Composer prompts are typed into the tmux pane (`orchestrate.go`
  `Send`), so conversation view is a **read-model over a terminal agent**, not a chat API.
- Questions/permission prompts: `internal/transcript/questions.go`, `box/answer.go`,
  `conversation/question-form.tsx`, `prompt-actions.tsx`.

## 10. Git worktree lifecycle (`internal/box`)

- Locations = named repo dirs on a box (`locations.go`, persisted JSON, `Locations.Add/List/
  Remove`). Worktree path convention: sibling dir `<repo>-<name>` (`locations.go:240`).
- `CreateWorktreeFrom` (`locations.go:224-285`): validates name (`trust.ValidName`,
  reserved artifact prefix), supports new branch / existing branch (fetch origin first) /
  `pull/<n>/head` PR fetch; `git -C <repo> worktree add [--track] -b …`; add runs under
  `context.WithoutCancel` to avoid half-initialised "initializing" locks; failure cleanup
  `cleanUpFailedAdd` (`worktree unlock`, `remove --force --force`, `prune`).
- `RemoveWorktree` (`:334`): unlock own lock then `git worktree remove [--force]`.
- `parseWorktrees` parses `git worktree list --porcelain` (`:403`).
- Setup/archive scripts: `scripts.go` — `setup`/`archive` commands with `BERTH_ROOT_PATH`,
  `BERTH_WORKTREE_PATH`, `BERTH_WORKTREE_NAME` (also Orca's `ORCA_*`). Repo config
  `.berth/config.json` (`repoconfig.go`): ports, env, services, hooks, flows, agent presets;
  per-worktree `BERTH_PORT` allocation (`ports.go`, `wtservices.go`).
- Status/sync/pause: `worktreeops.go` — base resolution (upstream → origin default → local
  default), commit log, sync (merge/rebase), **pause = SIGSTOP the pane's process tree,
  SIGCONT to resume** (`worktreeops.go:297-398`, uses `syscall.Kill`; Unix-only), persisted
  `PauseStore`.
- Resource guard (`guard.go`): under memory pressure stops services of idle worktrees, then
  pauses idle ones; reads `/proc` (Linux) / darwin variants (`procsnap_linux.go`,
  `procsnap_darwin.go`, `procsnap_other.go` = stub).
- App side: `views/worktrees/*`, `lib/worktrees.ts`, `lib/worktree-services.ts`,
  `components/new-worktree/*`.

## 11. Session handling

- Type `Session` (`internal/box/sessions.go:23-`): name, location, dir, command, created,
  attached count, exited, `Agent`, `AgentState`, `Turn`, `StateSeq`, `Fidelity`, `Title`,
  `Queued`, `Ask`, `Service`, etc.; all metadata in tmux options so it survives daemon restarts.
- Exit handling: `endSession` + `Ended` callback; scopes/process-tree kill (`scopes.go`).
- Images/files attach: `attachments.go` (box), `attachlocal.go` (agent uploads a laptop path).
- Memory ceiling per session (`sessionmemory.go`, systemd `MemoryHigh`).
- App: `lib/store.ts` (Zustand), `lib/session-open.ts`, `lib/session-title.ts`,
  `hooks/use-session-name.ts`, reconnect backoff `lib/reconnect.ts`, `lib/net.ts`.

## 12. Durable orchestration

- **Event journal** (`internal/events/journal.go`): append-only JSONL segments, monotonic
  `Seq`, group-commit fsync, torn-tail recovery, bounded memory with sparse offset index —
  lets a sleeping laptop catch up.
- **Turn ledger** (`internal/box/turns.go`): derives per-session turns (prompt → end of reply)
  from events in Seq order; waits, loops, flows, guard, Review and phone all read it.
- **Primitives** (`internal/box/orchestrate.go`): `Send` (paste + Enter), wait for turn/idle,
  run a check in a worktree; CLI mirror `internal/boxcmd`, TS mirror
  `app/src/lib/orchestrate-core.ts` ("same calls, same order, same messages").
- **Loops/runs** (`app/src/lib/loops.ts`, `internal/box/runs/`, `runsapi.go`): run-of-template
  on the box keeps going when the app quits.
- **Flows/automation** (`flows.go`, `flowtriggers.go`, `cron.go`, `triggers.go` (HMAC-signed
  webhooks, 5-min skew, delivery-id dedupe), `repohooks.go`; `before:` gates; netguard).
- **Offline queue** (`internal/agent/queue.go`): at-most-once delivery, `queued → sending →
  failed`, a prompt is never typed twice.
- **Handoff** (`box/handoff.go`): ≈2 KB packet (agent's note + diffstat + commits + last
  failing check tail) for the next agent.
- **MCP**: `internal/mcpserver/mcp.go` exposes box operations to agents.

## 13. Remote machine connectivity / auth / transport

- Identity: Ed25519 key per install; fingerprint = SHA-256(SPKI) (`internal/identity`).
- Wire (`internal/wire`): TLS 1.3 mutual, **pinned key only (no CA)**; HTTP/2 multiplexes API,
  TCP streams (`stream.go` presents a stream as `net.Conn`), terminals; health pings;
  `DialFunc` abstraction for non-system networks; `Unsent()` classifies retry-safe failures.
- Pairing (`internal/pairing`, `docs/concepts/security.mdx`): 256-bit single-use code,
  10-minute expiry, proof = HMAC over TLS exported keying material (RFC 5705) + laptop
  fingerprint; consumed under file lock; rate-limited per source + global, counting only
  failures (`wire/ratelimit.go`); join links (`berth://join?v=1&d=…`) bundle one code per box.
- Routes to a box: direct/tailnet; **embedded Tailscale nodes** for other tailnets
  (`internal/network`, uses `tailscale.com`); **via the user's own ssh** `ssh -W` in batch
  mode (`internal/sshroute`); `berth add ssh` bootstrap (`internal/sshsetup`,
  `cmd/berth/addssh.go`); `~/.ssh/config` parsing (`internal/sshconfig`).
- Trust stores: `internal/trust` (+ `statefile` atomic private files), `berthd clients/revoke`,
  live revoke cuts open connections (`wire/revoke.go`).
- Install/service: systemd user unit / launchd (`internal/service`, `cmd/berthd/install.go`,
  `internal/sshsetup/launchd.go`, `site/install.sh`).

## 14. Security & trust gates

(from `docs/concepts/security.mdx` + code)
- **Repo trust**: a repository's `.berth/config.json` is code; runs only after the user
  trusts the exact content hash on that location (`internal/box/repotrust.go`,
  `components/repo-trust.tsx`).
- **Kit review** pinned to a hash (`internal/box/kits.go`, `views/kits/review-sheet.tsx`).
- **Plugin consent**: a plugin runs in the app page with full app access, so it loads only after
  consent and only while `sha256("berth-plugin-v1\n"+len(manifest)+…)` equals the recorded hash
  (`app/src/plugins/consent-core.ts`, `consent.ts`, `hooks.PluginHash` in
  `internal/hooks/plugins.go`); `PLUGIN_POWERS` lists what is disclosed.
- **Outbound guard** (`internal/box/netguard.go`): box-originated requests only to public
  addresses, checked post-DNS on every dial incl. redirects; owner allowlist in
  `network.json`.
- **Flow data is text, never commands**; secrets via env refs (`lib/secret-ref.ts`,
  `internal/box/secrets.go`).
- **Phone token** scoped to read screens/type (`internal/box/phone.go`, `phoneui/`).
- **Agent browser** confined to the worktree's own pages through the proxy.
- UI-API origins whitelist and token (`internal/agent/ui.go:38`); release CSP
  (`tauri.conf.json`, `pnpm check:csp`).

## 15. Plugin architecture

- `packages/plugin-sdk` (`@berth/plugin` 0.1.0): **types only** (`src/index.ts` 425 lines,
  `types.ts` 707, `ui.ts` 169); runtime supplied by the app (`app/src/plugins/sdk-runtime.ts`);
  React, SDK, SDK/ui external.
- `activate(berth)` context: `api`, `orchestrate`, `prompts`, `storage`; `addScreen`,
  `addSidebarItem`, `addWorktreePanel`, `addWorktreeSection`, `addHomeWidget`, `addCommand`,
  `addStatusBarItem`, `addTheme`, `on(event)` (`docs/reference/plugin-sdk.mdx`).
- Manifest `berth-plugin.json {id,name,version,description,main,builtin}`.
- Built-ins in `plugins/` (git-changes, diff, pull-request, dev-servers, box-monitor, activity,
  notes, issues, prompts, usage; `hello-ports` example), bundled by esbuild into
  `app/public/builtin-plugins/<id>/` (`plugins/README.md`). User plugins in `~/.berth/plugins`
  are served by the agent.
- Host: `app/src/plugins/{host,registry,context,plugin-boundary,ui}.ts(x)`.
- Plugins styled with app Tailwind classes (`@source "../../plugins"` in `index.css`).

## 16. Tauri-specific coupling to replace with MyGo

Rust shell (`app/src-tauri`, 1379 lines of Rust in `src/`, Tauri `2` with features
`unstable`,`devtools`; plugins notification, shell, deep-link, updater, single-instance):

| Tauri feature | Used in | Needed replacement |
|---|---|---|
| `invoke`/commands `ui_endpoint`, `open_devtools`, `restart_app`, `open_terminal`, `agent_binary`, `browser_*` | `src-tauri/src/{lib,agent,browser}.rs`; TS: `lib/api.ts`, `lib/agent-start.ts`, `lib/cli-link.ts`, `components/browser-pane.tsx`, `browser-devtools.tsx`, `requirements-card.tsx`, `views/settings/developer-section.tsx`, `lib/updater.ts` | MyGo typed bindings (`mygo.Bind`, generated TS client; `mygo/README.md`). `ui_endpoint` reads a token file — in Orole the daemon endpoint is bound directly |
| Events `listen` | `components/browser-pane.tsx`, `lib/devtools.ts`, `hooks/use-shortcuts.ts` | MyGo events/channels |
| Window API (`getCurrentWindow`, titles, drag) | `hooks/use-shortcuts.ts`, `hooks/use-window-title.ts`; `titleBarStyle:"Overlay"` + `FakeTrafficLights` (`workspace/zen.tsx`) | MyGo `WindowOptions`; Windows caption/controls, drop macOS traffic-light offsets (76px rail) |
| Child webviews for browser panes | `browser.rs`, `linux.rs` | Biggest gap: MyGo public API shows one page per window; `mygo` has `internal/windows/webview2.go` but multi-webview/child-view embedding is **[UNVERIFIED]** (not found by grep of `*.go` for child/embedded webview API). Fallbacks: iframe mode (already exists in `browser-pane.tsx`, but `X-Frame-Options` pages fail), or separate MyGo window per browser pane, or WebView2 child via MyGo extension |
| `plugin-notification` | `lib/notifications.ts:608` | MyGo notifications API (README lists "notifications") |
| `plugin-shell` open | `lib/open-url.ts` | MyGo desktop API / `rundll32`/ShellExecute |
| `plugin-deep-link` (`berth://`) | `views/kits/deep-link.ts`, `views/team/team-entry.tsx` | MyGo `deeplink.go` |
| `plugin-updater` | `lib/updater.ts`, `views/settings/app-version.ts` (`getVersion`) | MyGo `updater.go` (signed delta updates per README) |
| Menu bar | `src-tauri/src/menu.rs` (macOS) | MyGo `menus.md`; Windows needs no app menu bar |
| CSP / IPC origin | `tauri.conf.json` (`connect-src ipc: http://ipc.localhost http://127.0.0.1:1378`) | New CSP for the MyGo origin and daemon port |
| Platform detection `__TAURI_INTERNALS__` | `lib/platform.ts`, `lib/api.ts` `isTauri()` | Replace with MyGo runtime detection; add `IS_WINDOWS`; `IS_MAC = !IS_LINUX` default must change |
| Sidecar CLI `berth` bundling, `cli_link.rs` (symlink to `~/.local/bin/berth`) | `agent.rs`, `lib/cli-link.ts` | Not needed (daemon is separate); Windows would use PATH entry/installer |
| Vite dev port 1420, `src-tauri` watch ignore | `app/vite.config.ts:157-175` | MyGo dev server config |

Package deps to drop: `@tauri-apps/api|plugin-deep-link|plugin-notification|plugin-shell|plugin-updater`
(+ devDep `@tauri-apps/cli`) (`app/package.json:49-53,88`). Only 16 TS files import
`@tauri-apps/*` (grep), so the surface is small.

## 17. tmux / Unix coupling to replace for Windows

| Coupling | Evidence | Windows replacement direction |
|---|---|---|
| tmux as session host, metadata store, screen scraper, input injector | `internal/box/tmux.go`, `sessions.go`, `orchestrate.go` | Own session manager in `orole-daemon`: ConPTY child + in-memory ring buffer/VT screen state (for `capture-pane` equivalent) + durable metadata file; reattach = daemon-held PTY (daemon is the persistence point rather than tmux) |
| Unix PTY (`/dev/ptmx`, `Setsid`, `Termios`, `TIOCSWINSZ`) | `internal/terminal/pty*.go` | ConPTY (`CreatePseudoConsole`); keep `frames.go` wire format |
| SIGSTOP/SIGCONT pause, `syscall.Kill` | `internal/box/worktreeops.go:297-398` | Job objects + `NtSuspendProcess` or drop the feature |
| systemd scopes / cgroups, `/proc` | `scopes.go`, `guard.go`, `procsnap_linux.go`, `stats.go` | Windows Job Objects for tree-kill and memory limits; `GetProcessMemoryInfo`; `procsnap_other.go` shows the stub shape |
| systemd/launchd service install | `internal/service`, `cmd/berthd/install.go` | Task Scheduler / Windows Service for `orole-daemon` |
| Unix-domain sockets for local API | `agent.go:346`, `cmd/berthd/main.go:448` | Go supports AF_UNIX on Win10+ [INFERENCE]; or loopback TCP w/ token, or named pipe |
| POSIX paths/shell (`sh -c`, `~`, `0o111` exec bit check) | `tmux.go:findTmux`, `scripts.go`, `sshsetup/agents.go` | `cmd.exe`/PowerShell/Git-Bash selection, `.exe` lookup, `%USERPROFILE%` |
| Chromium userns/AppArmor sandbox probing | `browsersandbox.go` | N/A on Windows |
| `install.sh`, `scripts/*.sh`, `ci/runner` | repo root | PowerShell/MSI equivalents; discard |
| `ssh -W`, `sshsetup` assume POSIX remote | `internal/sshroute`, `sshsetup` | Windows OpenSSH client works for `-W` [INFERENCE]; remote bootstrap stays POSIX-only |
| Mac key idioms (⌘) | `lib/platform.ts`, `lib/shortcuts.json` | Ctrl-based table already exists for Linux (`platformKeys`) |

## 18. Summary verdict

- **Highly reusable**: UI kit (`components/ui`), theme pipeline, home widget model, layout
  tree, conversation components, diff/files/editor components, chart lib, plugin SDK types and
  host, wire/pairing/trust Go packages (portable Go, only remote-machine relevant).
- **Needs re-seating**: everything that imports `@tauri-apps/*`, `lib/api.ts` (endpoint/token
  acquisition), `lib/platform.ts`, browser pane native layer.
- **Must be rewritten for Windows**: session hosting (tmux), PTY, pause/guard, service install,
  process-tree handling, shell/pathing in scripts.
- **Not applicable to a local-first OS**: boxes onboarding, tailnet/ssh setup, site/docs-site,
  release scripts for mac/linux, phone companion (unless wanted later).

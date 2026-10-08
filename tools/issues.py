import subprocess, json, sys
R = "rayorole/orole-os-desktop"
M = {0:"M0 Research and architecture",1:"M1 Native foundation",2:"M2 Orole OS design",3:"M3 Integrations",4:"M4 Development environment",5:"M5 Agent orchestration",6:"M6 Automations and extensibility",7:"M7 Hardening and release"}
I = [
# M0
(0,"area:docs,type:research","Shipyard source analysis, reuse inventory and attribution","docs/research/{shipyard-analysis,reuse-inventory,upstream-attribution}.md with commit hashes and per-file verdicts."),
(0,"area:docs,type:research","MyGo compatibility research on Windows","docs/research/mygo-compatibility.md: verified capability table (frameless, tray, deep links, updates, NSIS, devtools) with evidence."),
(0,"area:docs,type:research","ascii.rest library research","docs/research/ascii-library.md: piece contract, scenes, fps/reduced-motion behaviour, licence, integration plan."),
(0,"area:docs,area:mcp,type:research","MCP connectivity research (Orole Mail and Drive)","docs/research/mcp-connectivity.md: live probe evidence, OAuth metadata, Go SDK choice, verified vs blocked list."),
(0,"area:docs","Architecture specification, ADRs and Windows runtime plan","docs/architecture/*, docs/decisions/*: process model, IPC, daemon lifecycle (Task Scheduler vs Session-0 service), terminal lifecycle honesty."),
(0,"area:docs,area:security","Threat model and permission policy design","docs/security/threat-model.md: assets, trust boundaries, prompt-injection handling, plugin trust."),
(0,"area:docs","Design-system specification and dependency/integration matrix","docs/architecture/design-system.md and integration-matrix.md."),
# M1
(1,"area:desktop,area:frontend","MyGo bootstrap: Windows app shows the React frontend","bun run dev launches the MyGo window with Vite HMR; go tool mygo generate emits typed bindings."),
(1,"area:daemon","SQLite store with embedded migrations","modernc sqlite, WAL, foreign keys, idempotent migrations, tested."),
(1,"area:daemon,area:security","Authenticated versioned IPC over user-restricted named pipe","Per-user DACL pipe, hello+token, protocol negotiation, timeouts, cancel, bounded queues, reconnect; tests."),
(1,"area:daemon,area:windows","Daemon lifecycle: detached start, stop, status, single instance","orole CLI start/stop/status; GUI restart does not stop the daemon; second daemon refused."),
(1,"area:daemon,area:windows","Per-user autostart via Task Scheduler (install/uninstall)","orole daemon install registers a logon task with no admin rights; uninstall removes it; documented Session-0 distinction."),
(1,"area:daemon,area:security","Credential Manager secret store","Set/Get/Delete via Windows Credential Manager; frontend never receives secrets."),
(1,"area:daemon","Structured logging and diagnostics bundle","slog JSON to logs/daemon.log with rotation; diagnostics command output."),
(1,"area:desktop,area:daemon","Desktop-to-daemon bridge as typed MyGo services and events","React calls Daemon/Settings/Workspaces/Terminals services; daemon events forwarded; link state surfaced."),
(1,"area:desktop,area:windows","Windows packaging prototype (NSIS installer) containing desktop and daemon","mygo build produces installer; installed app launches and starts bundled daemon."),
(1,"area:ci","CI: Go vet/test/race, frontend typecheck/lint/test/build, Windows build","GitHub Actions on PRs without secrets."),
# M2
(2,"area:frontend","Design tokens, three themes and compact density","Midnight/Obsidian/Light + validated custom tokens; density switch."),
(2,"area:frontend","AsciiBackground with pause, fps, opacity, intensity, static, adaptive throttling","Single canvas; honours reduced motion; unfocused/low-power throttling; unit-tested policy."),
(2,"area:frontend,area:desktop","AppShell: custom title bar, sidebar, status bar","MyGo hidden title bar insets respected; keyboard accessible."),
(2,"area:frontend","Command palette and command registry","Ctrl+K, fuzzy, recents, provider filter, async providers, plugin contribution API."),
(2,"area:frontend","Dashboard widget grid: drag, resize, add, remove, persist","Layout persists in daemon DB per workspace; loading/error/offline states."),
(2,"area:frontend","Multi-workspace switcher","Create, switch, delete workspaces; layout per workspace persisted."),
(2,"area:frontend","Settings app: appearance, ASCII, accessibility, terminal, diagnostics","Real persisted settings; unbuilt sections say so."),
(2,"area:frontend","First-run onboarding flow","Skippable sequence: theme/scene, daemon, GitHub, Mail, Drive, agents, machine; incomplete tasks remain visible."),
(2,"area:frontend,area:docs","Core component library and design-system docs","AppShell/Panel/Widget/DataTable/TreeView/SplitPane/Dialogs/ContextMenu/Empty/Loading/Error states documented."),
(2,"area:frontend,type:test","Playwright E2E, accessibility (axe) and visual regression baseline","Browser-preview suite for shell, themes, palette, widgets."),
# M3
(3,"area:mcp,area:daemon","Generic MCP client manager (Streamable HTTP)","Add/remove/enable servers, status, tool/resource/prompt discovery, invoke with timeout/cancel/retry, diagnostics."),
(3,"area:mcp,area:security","MCP OAuth 2.1 (PKCE, DCR, resource indicators) with loopback redirect","Tokens in Credential Manager; refresh; no secrets in SQLite."),
(3,"area:mcp,area:security","Permission policy engine and audit log","Central server-side decision for every sensitive op; allow/ask/deny; audit events; approval UI."),
(3,"area:mcp","Orole Mail adapter and typed models","Capabilities derived from tools/list; no invented tool names."),
(3,"area:frontend,area:mcp","Mail application UI","Inbox, threads, search, actions only if server supports; send requires confirmation."),
(3,"area:frontend,area:mcp","Mail dashboard widget","Real unread/recent/important."),
(3,"area:mcp","Orole Drive adapter and typed models","Capabilities derived from tools/list."),
(3,"area:frontend,area:mcp","Drive application UI","Browse, search, preview, metadata, actions per server capability."),
(3,"area:frontend,area:mcp","Drive recent-files widget and scoped hand-off to agents","Temporary scoped access only."),
(3,"area:github,area:daemon","GitHub auth via gh CLI token and API adapter","Token never plaintext on disk; adapter interface ready for OAuth/App flow."),
(3,"area:github,area:frontend","GitHub app: repos, issues, PRs, notifications","Real authenticated reads; create/edit issue."),
(3,"area:github,area:frontend","GitHub Actions runs, failed checks and logs","Workflow runs and CI logs."),
(3,"area:frontend,area:mcp","MCP servers settings and tool inspector","Add/remove/enable, status, schemas, diagnostics."),
# M4
(4,"area:terminal,area:windows","ConPTY terminal backend and manager","PowerShell + cmd/pwsh/Git Bash, resize, Unicode, Ctrl-C, scrollback, exit status; tests."),
(4,"area:terminal,area:frontend","Terminals UI: tabs, splits, search, copy/paste, reconnect","xterm.js on WebView2 verified."),
(4,"area:terminal","Linux PTY backend and tmux persistence","TerminalBackend implementation for Linux."),
(4,"area:daemon,area:frontend","Local file service and file manager (provider adapters)","Browse/read/write local files; explicit providers; path policy."),
(4,"area:frontend","CodeMirror 6 editor with tabs, dirty state, conflict detection","Save, read-only, search."),
(4,"area:github,area:daemon","Git service: repos, branches, status, diffs, worktrees","Uses git binary; tested on temp repos."),
(4,"area:daemon","Machine abstraction: transport vs execution backend, identity pinning","Local machine + interface for remote Windows/Linux."),
(4,"area:daemon,area:security","Remote machine pairing and SSH transport","Host key pinning; no Tailscale dependency."),
(4,"area:frontend","Browser preview panes","Dev-server URLs, viewport sizes, open externally; console/network only if verified."),
(4,"area:agents,area:daemon","Agent runtime: session model, lifecycle, persistence","Start/stop/restart/prompt/inspect; metadata in SQLite."),
(4,"area:agents","Claude Code adapter end-to-end","Structured events/hooks preferred over output regexes."),
(4,"area:agents","Codex, OpenCode, Gemini CLI and custom CLI adapters","Capability-detected; clearly reports missing binaries."),
# M5
(5,"area:agents,area:frontend","Agents dashboard and conversation view","Running agents, approvals, transcript, changed files."),
(5,"area:agents,area:github","Issue-to-agent flow with worktree, traceability and PR confirmation","No publish without explicit confirmation."),
(5,"area:agents","Orchestration: handoff, review, parallel attempts, test-and-fix loops","Durable journals, idempotent dispatch, cancel, recovery."),
(5,"area:agents,area:security","Untrusted-content handling and agent sandbox options","Prompt-injection boundaries; optional Windows Sandbox/WSL2/container backends."),
# M6
(6,"area:automation,area:daemon","Automation runtime: triggers, steps, journals, retries, idempotency","Go engine tested first; no duplicate runs after restart; recursion guard."),
(6,"area:automation,area:security","Automation approvals, HTTP outbound restrictions, secret redaction","Human approval step; allowlist."),
(6,"area:automation,area:frontend","Automation UI/editor","Create/run/inspect workflows from supported steps."),
(6,"area:frontend","Notification center and native notifications","Aggregation, read/dismiss, preferences."),
(6,"area:frontend,area:daemon","Unified search providers","Server-side provider search for mail, Drive, GitHub, files, agents."),
(6,"area:security,area:frontend","Plugin API with capability manifests and trust UI","Capability-limited extensions; trusted-code disclosure."),
(6,"area:desktop","System tray, deep links (orole://), single instance, global shortcut","Verified on Windows."),
# M7
(7,"area:security,area:docs","Security review and hardening pass","Findings fixed or documented."),
(7,"area:desktop,type:test","Packaged-app E2E over WebView2 CDP","Playwright connects to installed app."),
(7,"area:docs,type:test","Performance benchmarks committed","Startup, idle mem/CPU, terminal latency, SQLite, MCP latency."),
(7,"area:desktop,area:windows","Signed NSIS installer and update process","Tested install/upgrade/uninstall; updater with signed releases."),
(7,"area:docs","Documentation set and troubleshooting","All docs listed in handoff section 25."),
]
out=[]
for m,lab,title,body in I:
    cmd=["gh","issue","create","-R",R,"-m",M[m],"-l",lab,"-t",title,"-b",body+"\n\nPart of "+M[m]+"."]
    r=subprocess.run(cmd,capture_output=True,text=True)
    if r.returncode: print("ERR",title,r.stderr); continue
    n=int(r.stdout.strip().rsplit("/",1)[1]); out.append((n,title))
    print(n,title)
json.dump(out,open("tools/issues.json","w"),indent=1)

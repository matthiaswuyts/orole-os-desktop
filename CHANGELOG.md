# Changelog

All notable changes to the Orole OS desktop app. Format follows [Keep a Changelog](https://keepachangelog.com).

Each release needs a `## X.Y.Z - YYYY-MM-DD` section matching `version` in `mygo.json`: the release
workflow refuses to build without it, and the in-app update window shows that section as the release
notes. See `docs/development/updates.md`.

## Unreleased

### Added
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

### Fixed
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

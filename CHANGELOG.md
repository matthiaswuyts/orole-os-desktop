# Changelog

All notable changes to the Orole OS desktop app. Format follows [Keep a Changelog](https://keepachangelog.com).

Each release needs a `## X.Y.Z - YYYY-MM-DD` section matching `version` in `mygo.json`: the release
workflow refuses to build without it, and the in-app update window shows that section as the release
notes. See `docs/development/updates.md`.

## Unreleased

### Changed
- Chat streaming no longer re-sends the whole reply every 50 ms: streamed text travels as small
  `chat.message.delta` events, with a full snapshot at tool boundaries, at least every 2 s and at the end,
  and the daemon builds the text in linear time. A 100 KB reply now publishes about 1 MB instead of 31 MB.
- Long conversations load only the text sent to the model, and the older backlog is summarised a page at a
  time. A failed summary is retried with a growing backoff instead of on every message.
- Sending, regenerating, clearing and delegating no longer hold the chat manager's global lock during
  database work.

### Fixed
- A model stream that goes silent for 90 seconds is cancelled with a clear error instead of hanging.
- Rate-limit (429) and server (5xx) answers are retried with jittered backoff before the reply starts,
  honouring `Retry-After`; a reply that already began is never replayed.

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

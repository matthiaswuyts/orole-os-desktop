# Changelog

All notable changes to the Orole OS desktop app. Format follows [Keep a Changelog](https://keepachangelog.com).

Each release needs a `## X.Y.Z - YYYY-MM-DD` section matching `version` in `mygo.json`: the release
workflow refuses to build without it, and the in-app update window shows that section as the release
notes. See `docs/development/updates.md`.

## Unreleased

### Changed
- Tasks: a streaming agent no longer re-renders the whole task feed. Events are grouped once for the feed and
  the canvas, unchanged messages and tool calls skip rendering, and conversations over 60 rows render only the
  rows near the viewport (search, "Load earlier" and following the newest message keep working).
- Tasks board: a session update re-renders only its own card, counts are computed in one pass, and only live
  sessions tick their elapsed time (a finished task now shows how long it ran, not time since it started).
- Task changes (diff) refresh at most once every two seconds, never overlap, and keep showing the previous
  diff while refreshing.
- Startup no longer waits for the full task list; the Tasks view and Home activity show their own loading state.
- Home activity calendar no longer rebuilds or reformats its 365 days on hover or on every session update.
- Terminal: faster output decoding, and changing the font size no longer recreates the terminal.

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

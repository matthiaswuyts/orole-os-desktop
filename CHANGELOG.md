# Changelog

All notable changes to the Orole OS desktop app. Format follows [Keep a Changelog](https://keepachangelog.com).

Each release needs a `## X.Y.Z - YYYY-MM-DD` section matching `version` in `mygo.json`: the release
workflow refuses to build without it, and the in-app update window shows that section as the release
notes. See `docs/development/updates.md`.

## Unreleased

### Fixed
- Terminals: a program that floods its terminal (`cat` of a huge file, a runaway build log) no longer gets
  the desktop's daemon connection closed. Output is coalesced (about 12 ms, 64 KiB per event) and capped
  per session; when the window cannot keep up the oldest undelivered output is skipped and the terminal
  view re-syncs from the daemon's scrollback instead of the connection being dropped.
- Terminals: scrollback is a fixed ring, so a busy terminal no longer allocates and copies ~1 MiB per read,
  and attaching to a terminal no longer stalls its output while the scrollback is copied.
- Terminals: exited sessions no longer count toward the 64-session limit and are forgotten after 30 minutes
  (at most 16 are kept). Agents running in a terminal learn about exit through a notification instead of
  polling every 250 ms.

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

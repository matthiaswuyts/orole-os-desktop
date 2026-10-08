# 0002: The daemon is a separate per-user process

Status: accepted

## Context
Terminals, and later agents, MCP connections and automations, must keep running when the window closes or the UI
process crashes or is updated. A GUI process that owns them couples their lifetime to a webview.

## Decision
All durable state and long-running work live in `orole-daemon` (`cmd/daemon`). The desktop process
(`internal/app`) is stateless and forwards calls; the CLI is a second client. Permissions are decided in the daemon.

## Consequences
- Closing/reopening the window keeps terminal sessions (reattach restores scrollback).
- A crash of the UI cannot corrupt state or leak permission decisions made in the UI.
- Updating the app requires stopping the daemon (a running exe cannot be replaced on Windows) [INFERENCE for the updater path].
- Extra process and IPC hop: measured round trip ~59 us for a no-op, ~128 us for `daemon.health` (`docs/performance.md`).
- Sessions still end when the daemon exits or at reboot; that is stated in UI and docs (ADR 0006).

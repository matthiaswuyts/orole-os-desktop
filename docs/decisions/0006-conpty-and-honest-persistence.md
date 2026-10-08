# 0006: ConPTY terminals and honest persistence

Status: accepted

## Context
Shipyard keeps terminals alive across everything with tmux on Linux/macOS boxes. Windows has ConPTY (pseudoconsole)
but no tmux equivalent, and a Windows daemon cannot make shells survive its own exit or a reboot.

## Decision
Use ConPTY (`github.com/UserExistsError/conpty`) behind a `Backend` interface; sessions are owned by the daemon with
a 1 MiB in-memory scrollback ring and sequence numbers for lossless reattach. Do not fake persistence: sessions
survive window restarts, not daemon exit or reboot, and the UI and docs say so.

## Consequences
- Reattach after closing the window works; nothing is restored after daemon exit (no half-restored shells).
- Scrollback is not written to disk (no secret leakage into files; also no history after daemon exit).
- Backend interface leaves room for another platform; today only Windows exists, which makes the daemon Windows-only.
- Measured keystroke echo through ConPTY: ~8.6-9.9 ms average in the daemon (`docs/performance.md`).

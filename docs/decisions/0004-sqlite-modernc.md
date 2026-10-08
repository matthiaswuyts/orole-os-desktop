# 0004: SQLite through modernc.org/sqlite

Status: accepted

## Context
Local metadata store (settings, workspaces, projects, audit log, agent sessions, policy rules). Needs transactions,
cheap migrations, and a Windows build without a C toolchain. MyGo also ships a purego-based SQLite plugin, aimed at
web pages and apps that do not run their own daemon.

## Decision
`modernc.org/sqlite` (pure Go, no cgo) in the daemon only, WAL mode, forward-only embedded SQL migrations
(`migrations/NNNN_*.sql`, one transaction each, unique number prefix per work slice).

## Consequences
- `go build` works on any machine with Go; CI needs no gcc.
- Slower than cgo SQLite and a larger binary [INFERENCE: not measured here]. Measured on this machine:
  ~70-78 us per autocommit audit insert, ~1.2 ms per 100-row transaction, ~96 us for a 50-row indexed query over
  100k rows (`docs/performance.md`).
- Only the daemon opens the DB; diagnostics opens it read-only (`mode=ro`) when the daemon is down.
- No down migrations; schema changes are additive or rebuild-and-copy inside one migration.

-- Agent runtime (slice 0020-0029). Sessions are stored as one JSON document (data) with the
-- columns the daemon queries on denormalised next to it; the JSON is the source of truth.
CREATE TABLE agent_sessions (
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  status      TEXT NOT NULL,
  project     TEXT NOT NULL DEFAULT '',
  dir         TEXT NOT NULL DEFAULT '',
  parent_id   TEXT NOT NULL DEFAULT '',
  grp         TEXT NOT NULL DEFAULT '',
  journal_key TEXT,                      -- idempotency key of the start request (NULL = none)
  started_at  INTEGER NOT NULL,
  ended_at    INTEGER,
  updated_at  INTEGER NOT NULL,
  data        TEXT NOT NULL              -- JSON Session
);
CREATE UNIQUE INDEX agent_sessions_journal_key ON agent_sessions(journal_key) WHERE journal_key IS NOT NULL;
CREATE INDEX agent_sessions_status ON agent_sessions(status);
CREATE INDEX agent_sessions_started ON agent_sessions(started_at);

CREATE TABLE agent_events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
  at         INTEGER NOT NULL,           -- unix milliseconds
  kind       TEXT NOT NULL,
  text       TEXT NOT NULL DEFAULT '',
  data       TEXT NOT NULL DEFAULT 'null' -- JSON
);
CREATE INDEX agent_events_session ON agent_events(session_id, id);

-- Orchestration journal: at-most-once dispatch. A row is inserted (pending) before the side
-- effect and finished after; a restart marks leftover pending rows 'interrupted' instead of
-- repeating them.
CREATE TABLE agent_journal (
  key        TEXT PRIMARY KEY,
  op         TEXT NOT NULL,
  params     TEXT NOT NULL DEFAULT 'null',
  status     TEXT NOT NULL,              -- pending | done | failed | interrupted
  boot       TEXT NOT NULL,              -- id of the daemon run that began the op
  result     TEXT NOT NULL DEFAULT 'null',
  error      TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

-- Long-running orchestration runs (loop-until, try-N-ways, hand-off, review-with).
CREATE TABLE agent_runs (
  id         TEXT PRIMARY KEY,
  kind       TEXT NOT NULL,
  key        TEXT NOT NULL DEFAULT '',
  status     TEXT NOT NULL,              -- running | succeeded | failed | cancelled | interrupted
  params     TEXT NOT NULL DEFAULT 'null',
  state      TEXT NOT NULL DEFAULT 'null',
  error      TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX agent_runs_status ON agent_runs(status);

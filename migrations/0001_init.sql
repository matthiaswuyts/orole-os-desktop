CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,           -- JSON
  updated_at INTEGER NOT NULL
);

CREATE TABLE workspaces (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  position   INTEGER NOT NULL DEFAULT 0,
  layout     TEXT NOT NULL DEFAULT '{}', -- JSON: widgets, tabs, panes
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE machines (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  kind        TEXT NOT NULL,          -- local | remote
  os          TEXT NOT NULL,
  transport   TEXT NOT NULL,
  address     TEXT NOT NULL DEFAULT '',
  fingerprint TEXT NOT NULL DEFAULT '', -- pinned machine identity
  created_at  INTEGER NOT NULL
);

CREATE TABLE projects (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  machine_id TEXT NOT NULL DEFAULT 'local',
  path       TEXT NOT NULL,
  remote_url TEXT NOT NULL DEFAULT '',
  last_opened INTEGER NOT NULL DEFAULT 0,
  UNIQUE (machine_id, path)
);

CREATE TABLE mcp_servers (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  url        TEXT NOT NULL,
  transport  TEXT NOT NULL DEFAULT 'streamable-http',
  adapter    TEXT NOT NULL DEFAULT '',  -- '', mail, drive
  enabled    INTEGER NOT NULL DEFAULT 1,
  auth_kind  TEXT NOT NULL DEFAULT 'none', -- none | bearer | oauth ; secrets live in the credential store
  created_at INTEGER NOT NULL
);

CREATE TABLE mcp_permissions (
  server_id TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
  tool      TEXT NOT NULL,
  decision  TEXT NOT NULL,             -- allow | ask | deny
  PRIMARY KEY (server_id, tool)
);

CREATE TABLE audit_events (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  at      INTEGER NOT NULL,
  actor   TEXT NOT NULL,
  action  TEXT NOT NULL,
  target  TEXT NOT NULL DEFAULT '',
  outcome TEXT NOT NULL,               -- allowed | denied | error
  detail  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_events_at ON audit_events(at);

CREATE TABLE notifications (
  id        TEXT PRIMARY KEY,
  at        INTEGER NOT NULL,
  source    TEXT NOT NULL,
  title     TEXT NOT NULL,
  body      TEXT NOT NULL DEFAULT '',
  read      INTEGER NOT NULL DEFAULT 0,
  dismissed INTEGER NOT NULL DEFAULT 0
);

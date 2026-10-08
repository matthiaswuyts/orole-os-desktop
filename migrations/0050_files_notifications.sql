-- Approved file roots: the only places the files providers may touch.
-- `path` is stored fully resolved (symlinks/junctions/short names expanded).
CREATE TABLE file_roots (
  id         TEXT PRIMARY KEY,
  provider   TEXT NOT NULL DEFAULT 'local',
  path       TEXT NOT NULL,
  label      TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  UNIQUE (provider, path)
);

-- Notifications gain a severity level; preferences per source live in settings
-- under the key "notifications.prefs".
ALTER TABLE notifications ADD COLUMN level TEXT NOT NULL DEFAULT 'info';
CREATE INDEX notifications_at ON notifications(dismissed, at DESC);
CREATE INDEX notifications_source ON notifications(source);

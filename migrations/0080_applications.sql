CREATE TABLE application_shortcuts (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  executable TEXT NOT NULL,
  arguments TEXT NOT NULL DEFAULT '[]',
  working_directory TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX application_shortcuts_name ON application_shortcuts(name COLLATE NOCASE);

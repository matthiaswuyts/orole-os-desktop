-- Projects registry extensions and the worktree index (issue #46).
ALTER TABLE projects ADD COLUMN worktree_root TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;

-- One row per git worktree of a registered project (the main checkout included).
-- Git is the source of truth for existence; this table carries Orole metadata and stable ids.
CREATE TABLE worktrees (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  path       TEXT NOT NULL,
  branch     TEXT NOT NULL DEFAULT '',
  base_ref   TEXT NOT NULL DEFAULT '',
  is_main    INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE (project_id, path)
);
CREATE INDEX worktrees_project ON worktrees(project_id);

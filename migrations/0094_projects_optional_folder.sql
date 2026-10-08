-- Projects may have no folder (path = ''). The original UNIQUE (machine_id, path) would allow only one
-- such project per machine, and SQLite cannot drop a table constraint, so the table is rebuilt with a
-- partial unique index instead. worktrees is the only table referencing projects; it is parked and
-- recreated around the swap so dropping projects cannot cascade-delete its rows.
CREATE TABLE worktrees_parked AS SELECT * FROM worktrees;
DROP TABLE worktrees;

CREATE TABLE projects_new (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  machine_id    TEXT NOT NULL DEFAULT 'local',
  path          TEXT NOT NULL DEFAULT '',
  remote_url    TEXT NOT NULL DEFAULT '',
  last_opened   INTEGER NOT NULL DEFAULT 0,
  worktree_root TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL DEFAULT 0
);
INSERT INTO projects_new(id,name,machine_id,path,remote_url,last_opened,worktree_root,created_at)
  SELECT id,name,machine_id,path,remote_url,last_opened,worktree_root,created_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;
CREATE UNIQUE INDEX projects_machine_path ON projects(machine_id, path) WHERE path <> '';

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
INSERT INTO worktrees(id,project_id,name,path,branch,base_ref,is_main,created_at)
  SELECT id,project_id,name,path,branch,base_ref,is_main,created_at FROM worktrees_parked;
DROP TABLE worktrees_parked;

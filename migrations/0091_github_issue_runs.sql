CREATE TABLE github_issue_runs (
  id TEXT PRIMARY KEY,
  request_key TEXT NOT NULL UNIQUE,
  repo TEXT NOT NULL,
  issue_number INTEGER NOT NULL CHECK(issue_number > 0),
  data TEXT NOT NULL,
  preparation TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX github_issue_runs_issue ON github_issue_runs(repo, issue_number, created_at);

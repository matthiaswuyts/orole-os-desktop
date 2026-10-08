-- A session's branch can be published (commit, push, pull request). The Session JSON stays the
-- source of truth; the pull request is denormalised next to it so it can be queried and indexed
-- without parsing documents. Existing rows predate publishing and have no pull request.
ALTER TABLE agent_sessions ADD COLUMN pr_url    TEXT    NOT NULL DEFAULT '';
ALTER TABLE agent_sessions ADD COLUMN pr_number INTEGER NOT NULL DEFAULT 0;
CREATE INDEX agent_sessions_pr ON agent_sessions(pr_url) WHERE pr_url <> '';

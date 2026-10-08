-- Automation engine. Times are unix milliseconds unless noted.
--
-- automations: one row per saved definition. `definition` is the JSON document
-- (carries its own schemaVersion); trigger_type/enabled are denormalised for queries.
-- last_fire_at is the last scheduled slot consumed (unix seconds); it lets the
-- scheduler apply the catch-up policy after the daemon was down or the PC slept.
CREATE TABLE automations (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  enabled        INTEGER NOT NULL DEFAULT 0,
  schema_version INTEGER NOT NULL,
  trigger_type   TEXT NOT NULL,
  definition     TEXT NOT NULL,
  last_fire_at   INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL
);

-- automation_runs: one row per execution. `definition` is a snapshot taken when the
-- run was created, so editing an automation never changes a run in flight or the
-- way a crashed run resumes. trigger_key (when set) is unique: a scheduled slot or a
-- webhook delivery id can create at most one run, even across restarts.
CREATE TABLE automation_runs (
  id            TEXT PRIMARY KEY,
  automation_id TEXT NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
  status        TEXT NOT NULL, -- queued | running | waiting-approval | succeeded | failed | canceled | uncertain
  trigger_type  TEXT NOT NULL,
  trigger_key   TEXT,
  trigger_data  TEXT NOT NULL DEFAULT '{}', -- JSON, redacted
  chain         TEXT NOT NULL DEFAULT '[]', -- JSON array of automation ids (provenance)
  definition    TEXT NOT NULL,
  error         TEXT NOT NULL DEFAULT '',
  created_at    INTEGER NOT NULL,
  started_at    INTEGER NOT NULL DEFAULT 0,
  finished_at   INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX automation_runs_trigger_key ON automation_runs(trigger_key) WHERE trigger_key IS NOT NULL;
CREATE INDEX automation_runs_automation ON automation_runs(automation_id, created_at DESC);
CREATE INDEX automation_runs_status ON automation_runs(status);

-- automation_steps_journal: the write-ahead journal. A row is written with status
-- 'running' BEFORE a step executes and updated AFTER it finishes. On restart the
-- first step without a finished row is where the run resumes; a row still 'running'
-- means the daemon died mid-step (see idempotency_key and the step's strategy).
-- input/output/error are redacted before they are stored.
CREATE TABLE automation_steps_journal (
  run_id          TEXT NOT NULL REFERENCES automation_runs(id) ON DELETE CASCADE,
  step_id         TEXT NOT NULL,
  seq             INTEGER NOT NULL, -- position in the definition
  step_type       TEXT NOT NULL,
  status          TEXT NOT NULL, -- running | succeeded | failed | skipped | canceled | uncertain | waiting-approval
  attempt         INTEGER NOT NULL DEFAULT 0,
  idempotency_key TEXT NOT NULL,
  input           TEXT NOT NULL DEFAULT 'null',
  output          TEXT NOT NULL DEFAULT 'null',
  error           TEXT NOT NULL DEFAULT '',
  started_at      INTEGER NOT NULL DEFAULT 0,
  finished_at     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (run_id, step_id)
);
CREATE INDEX automation_steps_status ON automation_steps_journal(status);

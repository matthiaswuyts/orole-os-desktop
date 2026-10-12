-- Agent token usage ledger: billed tokens per session, model and 15-minute UTC bucket, summed from
-- the harnesses' per-request or per-turn reports (never from context-occupancy readings). Quarter
-- hours keep every bucket inside one local day in any time zone (UTC offsets are whole quarter
-- hours). Cost is priced at query time from the model price catalogue unless every report folded
-- into the bucket carried the harness's own cost (has_reported_cost = 1), which then wins.
CREATE TABLE agent_usage (
  session_id        TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
  provider          TEXT NOT NULL DEFAULT '', -- models.dev provider id when known
  model             TEXT NOT NULL DEFAULT '',
  bucket_start      INTEGER NOT NULL,         -- Unix seconds of the UTC quarter hour (multiple of 900) the tokens were reported in
  input             INTEGER NOT NULL DEFAULT 0, -- uncached input
  output            INTEGER NOT NULL DEFAULT 0, -- includes reasoning
  cache_read        INTEGER NOT NULL DEFAULT 0,
  cache_write       INTEGER NOT NULL DEFAULT 0,
  reasoning         INTEGER NOT NULL DEFAULT 0, -- informational subset of output
  reported_cost_usd REAL NOT NULL DEFAULT 0,
  has_reported_cost INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (session_id, provider, model, bucket_start)
) WITHOUT ROWID;
CREATE INDEX agent_usage_bucket ON agent_usage(bucket_start);
CREATE INDEX agent_sessions_updated ON agent_sessions(updated_at);

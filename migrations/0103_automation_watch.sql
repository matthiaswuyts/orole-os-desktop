-- Watch triggers poll an integration and run their automation once per new item.
-- One row per watching automation: the watch configuration the cursor belongs to
-- (a changed configuration starts a new baseline), the item keys already seen,
-- and the last poll's outcome.
CREATE TABLE automation_watch_state (
  automation_id TEXT PRIMARY KEY REFERENCES automations(id) ON DELETE CASCADE,
  watch         TEXT NOT NULL,            -- canonical JSON of the Watch the cursor belongs to
  seen          TEXT NOT NULL DEFAULT '[]', -- JSON array of item keys, newest last, bounded
  error         TEXT NOT NULL DEFAULT '',
  polled_at     INTEGER NOT NULL DEFAULT 0 -- Unix milliseconds
);

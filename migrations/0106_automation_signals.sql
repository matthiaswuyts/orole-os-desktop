-- Requests a bot keeps getting from the user that an automation could take over. One row per
-- bot and repeated tool call (key = tool name + hash of its canonical arguments), recorded only
-- in turns the user started. Times are Unix milliseconds; last_day is the local date (YYYY-MM-DD)
-- of the last call, so `days` counts distinct days. offered_at/offers rate-limit suggestions;
-- dismissed_until silences one after the user declined (or accepted) it.
CREATE TABLE automation_signals (
  bot_id          TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  key             TEXT NOT NULL,
  label           TEXT NOT NULL,
  count           INTEGER NOT NULL DEFAULT 0,
  days            INTEGER NOT NULL DEFAULT 0,
  last_day        TEXT NOT NULL DEFAULT '',
  first_at        INTEGER NOT NULL,
  last_at         INTEGER NOT NULL,
  offered_at      INTEGER NOT NULL DEFAULT 0,
  offers          INTEGER NOT NULL DEFAULT 0,
  dismissed_until INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bot_id, key)
);

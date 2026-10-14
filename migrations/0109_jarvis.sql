-- Jarvis mode: rules tell the daemon when a bot phones the owner; calls record each phone call.
-- Times are unix milliseconds. target is a task or automation id, "any", a notification
-- kind[:source], or HH:MM for at_time. Once rules (repeat=0) are deleted after they fire.
CREATE TABLE jarvis_rules (
  id             TEXT PRIMARY KEY,
  bot_id         TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  trigger        TEXT NOT NULL CHECK (trigger IN ('task_finished','task_failed','automation_finished','automation_failed','notification','at_time')),
  target         TEXT NOT NULL DEFAULT '' CHECK (length(target) <= 200),
  repeat         INTEGER NOT NULL DEFAULT 0,
  urgent         INTEGER NOT NULL DEFAULT 0,
  quiet_override INTEGER NOT NULL DEFAULT 0,
  note           TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 280),
  expires_at     INTEGER,
  created_at     INTEGER NOT NULL
);
CREATE INDEX jarvis_rules_trigger ON jarvis_rules(trigger);
CREATE TABLE jarvis_calls (
  id               TEXT PRIMARY KEY,
  bot_id           TEXT,
  direction        TEXT NOT NULL CHECK (direction IN ('outbound','inbound')),
  rule_id          TEXT,
  reason           TEXT NOT NULL DEFAULT '',
  status           TEXT NOT NULL,
  provider_call_id TEXT NOT NULL DEFAULT '',
  conversation_id  TEXT NOT NULL DEFAULT '',
  message_id       TEXT NOT NULL DEFAULT '',
  started_at       INTEGER,
  answered_at      INTEGER,
  ended_at         INTEGER,
  est_cost_micros  INTEGER NOT NULL DEFAULT 0,
  created_at       INTEGER NOT NULL
);
CREATE INDEX jarvis_calls_created ON jarvis_calls(created_at);
CREATE INDEX jarvis_calls_bot ON jarvis_calls(bot_id, created_at);

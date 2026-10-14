-- Jarvis rules gain the bot_reported trigger: a bot the rule's bot handed work to reported back. target is that
-- bot's name or "any". SQLite cannot change a CHECK constraint, so jarvis_rules is rebuilt with the wider list;
-- no other table references it (jarvis_calls.rule_id is a plain column), so the swap cannot cascade anywhere.
CREATE TABLE jarvis_rules_new (
  id             TEXT PRIMARY KEY,
  bot_id         TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  trigger        TEXT NOT NULL CHECK (trigger IN ('task_finished','task_failed','automation_finished','automation_failed','notification','at_time','bot_reported')),
  target         TEXT NOT NULL DEFAULT '' CHECK (length(target) <= 200),
  repeat         INTEGER NOT NULL DEFAULT 0,
  urgent         INTEGER NOT NULL DEFAULT 0,
  quiet_override INTEGER NOT NULL DEFAULT 0,
  note           TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 280),
  expires_at     INTEGER,
  created_at     INTEGER NOT NULL
);
INSERT INTO jarvis_rules_new(id,bot_id,trigger,target,repeat,urgent,quiet_override,note,expires_at,created_at)
  SELECT id,bot_id,trigger,target,repeat,urgent,quiet_override,note,expires_at,created_at FROM jarvis_rules;
DROP TABLE jarvis_rules;
ALTER TABLE jarvis_rules_new RENAME TO jarvis_rules;
CREATE INDEX jarvis_rules_trigger ON jarvis_rules(trigger);

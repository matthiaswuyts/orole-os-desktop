-- Who created an automation: 'user' (the builder) or 'bot:<id>' (a bot's draft the user
-- approved in chat). Set on insert only; later saves never change it.
ALTER TABLE automations ADD COLUMN created_by TEXT NOT NULL DEFAULT 'user';

-- Automations a bot drafted in chat; inert until the user approves the card. A row's
-- definition never changes after insert, so what the card shows is what gets saved.
-- replaces/base_version: the automation an edit applies to and a hash of its definition
-- when the bot drafted it (approval refuses a stale base). automation_id: what approval saved.
CREATE TABLE automation_proposals (
  id              TEXT PRIMARY KEY,
  bot_id          TEXT NOT NULL,
  name            TEXT NOT NULL,
  replaces        TEXT NOT NULL DEFAULT '',
  base_version    TEXT NOT NULL DEFAULT '',
  rationale       TEXT NOT NULL DEFAULT '',
  definition      TEXT NOT NULL,
  status          TEXT NOT NULL, -- pending | approved | rejected | replaced
  automation_id   TEXT NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  decided_at      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX automation_proposals_bot ON automation_proposals(bot_id, status);

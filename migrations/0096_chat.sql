-- Chat: bots are contacts with one continuous conversation each (no threads).
-- Provider credentials never live here (internal/credentials); config JSON holds no secrets.
CREATE TABLE bots (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  avatar       TEXT NOT NULL DEFAULT '',
  color        TEXT NOT NULL DEFAULT '',
  description  TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  instructions TEXT NOT NULL DEFAULT '' CHECK (length(instructions) <= 32768),
  provider     TEXT NOT NULL DEFAULT '',
  model        TEXT NOT NULL DEFAULT '',
  reasoning    TEXT NOT NULL DEFAULT '',
  config       TEXT NOT NULL DEFAULT '{}',   -- tools, mcp tools, skills, delegation, memory access, limits
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  archived_at  INTEGER
);
CREATE INDEX bots_active ON bots(archived_at, name);

CREATE TABLE messages (
  id         TEXT PRIMARY KEY,
  bot_id     TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  seq        INTEGER NOT NULL,
  role       TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
  author     TEXT NOT NULL DEFAULT '',        -- 'user', 'bot:<id>' (delegated runs), '' for system
  parts      TEXT NOT NULL DEFAULT '[]',      -- JSON []chat.MsgPart
  text       TEXT NOT NULL DEFAULT '',        -- plain-text projection for FTS
  status     TEXT NOT NULL DEFAULT 'complete' CHECK (status IN ('pending', 'running', 'complete', 'incomplete', 'error', 'canceled')),
  usage      TEXT NOT NULL DEFAULT '',        -- JSON chat.MessageUsage, provider-reported only
  error      TEXT NOT NULL DEFAULT '',
  model      TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  UNIQUE (bot_id, seq)
);

CREATE VIRTUAL TABLE messages_fts USING fts5(text, content='messages', content_rowid='rowid', tokenize='porter unicode61');
CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
END;
CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
END;
CREATE TRIGGER messages_au AFTER UPDATE OF text ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
  INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
END;

-- Content-addressed blobs live at <DataDir>/chat/attachments/<sha256>; rows are references.
CREATE TABLE chat_attachments (
  id         TEXT PRIMARY KEY,
  sha256     TEXT NOT NULL CHECK (length(sha256) = 64),
  name       TEXT NOT NULL,
  mime       TEXT NOT NULL,
  size       INTEGER NOT NULL CHECK (size >= 0),
  bot_id     TEXT REFERENCES bots(id) ON DELETE CASCADE,  -- set once sent or attached as knowledge
  created_at INTEGER NOT NULL
);
CREATE INDEX chat_attachments_sha ON chat_attachments(sha256);
CREATE INDEX chat_attachments_staged ON chat_attachments(bot_id, created_at);

-- Rolling summary of messages with seq <= upto_seq (used once the window fills).
CREATE TABLE conversation_summaries (
  bot_id     TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
  upto_seq   INTEGER NOT NULL,
  text       TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

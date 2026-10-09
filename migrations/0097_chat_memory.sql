-- Bot memory. Scopes: 'bot:<id>' (private), 'user' (shared profile), 'shared:<name>'.
-- kind 'core' entries are the small curated always-in-prompt notes; 'fact' entries are archival.
-- Facts are never deleted on contradiction; they are invalidated (validity window).
CREATE TABLE memory_scopes (
  id         TEXT PRIMARY KEY CHECK (id = 'user' OR id LIKE 'shared:%'),
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
INSERT INTO memory_scopes(id, name, created_at) VALUES ('user', 'About you', CAST(strftime('%s','now') AS INTEGER) * 1000);

CREATE TABLE memory_entries (
  id             TEXT PRIMARY KEY,
  scope          TEXT NOT NULL,
  kind           TEXT NOT NULL CHECK (kind IN ('core', 'fact')),
  text           TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 4000),
  trust          TEXT NOT NULL CHECK (trust IN ('user', 'bot', 'untrusted')),
  author         TEXT NOT NULL DEFAULT '',      -- 'user' or 'bot:<id>'
  source_msg_id  TEXT NOT NULL DEFAULT '',
  pending        INTEGER NOT NULL DEFAULT 0 CHECK (pending IN (0, 1)),  -- awaiting user approval
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL,
  invalidated_at INTEGER,
  invalidated_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX memory_entries_scope ON memory_entries(scope, kind, invalidated_at, created_at);

CREATE VIRTUAL TABLE memory_fts USING fts5(text, content='memory_entries', content_rowid='rowid', tokenize='porter unicode61');
CREATE TRIGGER memory_ai AFTER INSERT ON memory_entries BEGIN
  INSERT INTO memory_fts(rowid, text) VALUES (new.rowid, new.text);
END;
CREATE TRIGGER memory_ad AFTER DELETE ON memory_entries BEGIN
  INSERT INTO memory_fts(memory_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
END;
CREATE TRIGGER memory_au AFTER UPDATE OF text ON memory_entries BEGIN
  INSERT INTO memory_fts(memory_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
  INSERT INTO memory_fts(rowid, text) VALUES (new.rowid, new.text);
END;

-- A bot always reads and writes its own 'bot:<id>' scope; rows grant anything else.
CREATE TABLE bot_memory_access (
  bot_id    TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  scope     TEXT NOT NULL,
  can_read  INTEGER NOT NULL DEFAULT 1 CHECK (can_read IN (0, 1)),
  can_write INTEGER NOT NULL DEFAULT 0 CHECK (can_write IN (0, 1)),
  PRIMARY KEY (bot_id, scope)
);

-- How far idle consolidation has processed each bot's conversation.
CREATE TABLE memory_consolidation (
  bot_id   TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
  upto_seq INTEGER NOT NULL,
  ran_at   INTEGER NOT NULL
);

-- Skills a bot proposed; inert until approved in the UI.
CREATE TABLE skill_proposals (
  id          TEXT PRIMARY KEY,
  bot_id      TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL,
  body        TEXT NOT NULL CHECK (length(body) <= 262144),
  status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
  created_at  INTEGER NOT NULL,
  decided_at  INTEGER
);

-- Bot knowledge files: user-attached documents, chunked and searchable through knowledge_search.
CREATE TABLE knowledge_files (
  attachment_id TEXT NOT NULL REFERENCES chat_attachments(id) ON DELETE CASCADE,
  bot_id        TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  status        TEXT NOT NULL DEFAULT 'indexing' CHECK (status IN ('indexing', 'ready', 'error')),
  error         TEXT NOT NULL DEFAULT '',
  chunks        INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  PRIMARY KEY (bot_id, attachment_id)
);
CREATE TABLE knowledge_chunks (
  id            INTEGER PRIMARY KEY,
  bot_id        TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  attachment_id TEXT NOT NULL REFERENCES chat_attachments(id) ON DELETE CASCADE,
  ord           INTEGER NOT NULL,
  text          TEXT NOT NULL
);
CREATE VIRTUAL TABLE knowledge_fts USING fts5(text, content='knowledge_chunks', content_rowid='id', tokenize='porter unicode61');
CREATE TRIGGER knowledge_ai AFTER INSERT ON knowledge_chunks BEGIN
  INSERT INTO knowledge_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER knowledge_ad AFTER DELETE ON knowledge_chunks BEGIN
  INSERT INTO knowledge_fts(knowledge_fts, rowid, text) VALUES ('delete', old.id, old.text);
END;

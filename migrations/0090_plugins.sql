-- Declarative MCP-backed plugins contain configuration, never executable code.
-- Arguments are plaintext non-secret configuration; credentials remain in the OS store.
CREATE TABLE plugins (
  id              TEXT PRIMARY KEY,
  manifest        TEXT NOT NULL CHECK (length(manifest) <= 65536),
  digest          TEXT NOT NULL CHECK (length(digest) = 64),
  enabled         INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
  approved_digest TEXT NOT NULL DEFAULT '',
  updated_at      INTEGER NOT NULL,
  CHECK ((enabled = 0 AND approved_digest = '') OR
         (enabled = 1 AND approved_digest = digest))
);

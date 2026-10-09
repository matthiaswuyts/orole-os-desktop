-- Declarative OpenAI-compatible model providers contain configuration, never executable code.
-- The API key is never stored here: it lives in the OS credential store under llm/<id>/key.
CREATE TABLE llm_manifests (
  id              TEXT PRIMARY KEY,
  manifest        TEXT NOT NULL CHECK (length(manifest) <= 32768),
  digest          TEXT NOT NULL CHECK (length(digest) = 64),
  enabled         INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
  approved_digest TEXT NOT NULL DEFAULT '',
  updated_at      INTEGER NOT NULL,
  CHECK ((enabled = 0 AND approved_digest = '') OR
         (enabled = 1 AND approved_digest = digest))
);

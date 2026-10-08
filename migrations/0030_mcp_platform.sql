-- MCP platform + central permission engine.
-- NOTE: no secret ever lives here. Access/refresh tokens, bearer tokens and client
-- secrets are stored only in the OS credential store under key mcp/<server id>.

ALTER TABLE mcp_servers ADD COLUMN oauth_client_id TEXT NOT NULL DEFAULT ''; -- public client id or https client-metadata URL
ALTER TABLE mcp_servers ADD COLUMN scopes TEXT NOT NULL DEFAULT '';          -- space separated override; '' = use server challenge

-- Non-secret record of the last OAuth discovery/authorization for a server (diagnostics, refresh routing).
CREATE TABLE mcp_auth_state (
  server_id              TEXT PRIMARY KEY REFERENCES mcp_servers(id) ON DELETE CASCADE,
  issuer                 TEXT NOT NULL DEFAULT '',
  authorization_endpoint TEXT NOT NULL DEFAULT '',
  token_endpoint         TEXT NOT NULL DEFAULT '',
  registration_endpoint  TEXT NOT NULL DEFAULT '',
  resource               TEXT NOT NULL DEFAULT '',
  registration_kind      TEXT NOT NULL DEFAULT '',  -- preregistered | cimd | dcr
  client_id              TEXT NOT NULL DEFAULT '',
  scopes                 TEXT NOT NULL DEFAULT '',
  authorized_at          INTEGER NOT NULL DEFAULT 0,
  access_expires_at      INTEGER NOT NULL DEFAULT 0
);

-- Last discovered tools/resources/prompts (server-controlled, untrusted display data).
CREATE TABLE mcp_cache (
  server_id          TEXT PRIMARY KEY REFERENCES mcp_servers(id) ON DELETE CASCADE,
  fetched_at         INTEGER NOT NULL,
  protocol_version   TEXT NOT NULL DEFAULT '',
  server_info        TEXT NOT NULL DEFAULT '{}',
  capabilities       TEXT NOT NULL DEFAULT '{}',
  tools              TEXT NOT NULL DEFAULT '[]',
  resources          TEXT NOT NULL DEFAULT '[]',
  resource_templates TEXT NOT NULL DEFAULT '[]',
  prompts            TEXT NOT NULL DEFAULT '[]'
);

-- Generic policy rules. Patterns use '*' wildcards. Most specific rule wins; at equal
-- specificity deny beats ask beats allow. max_effect (read|write|destructive) limits
-- the rule to requests whose effect is not stronger than it ('' = any).
CREATE TABLE policy_rules (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  actor      TEXT NOT NULL DEFAULT '*',
  action     TEXT NOT NULL,
  resource   TEXT NOT NULL DEFAULT '*',
  max_effect TEXT NOT NULL DEFAULT '' CHECK (max_effect IN ('', 'read', 'write', 'destructive')),
  decision   TEXT NOT NULL CHECK (decision IN ('allow', 'ask', 'deny')),
  note       TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  UNIQUE (actor, action, resource, max_effect)
);

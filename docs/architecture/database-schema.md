# Database schema

SQLite via `modernc.org/sqlite` (pure Go, no cgo). File: `<data dir>/orole.db`. The SQL in `migrations/*.sql`
is authoritative; this page summarises it. Migrations present in this tree: `0001`, `0010`, `0020`, `0030`, `0050`.

## Connection and migrations

`internal/database`: DSN pragmas `busy_timeout(5000)`, `journal_mode(WAL)`, `foreign_keys(1)`,
`synchronous(NORMAL)`; up to 4 open connections. `Migrate` creates `schema_migrations(version, applied_at)`, then
applies every embedded `NNNN_name.sql` (`migrations/embed.go`) whose numeric prefix is not recorded, in order,
**each in its own transaction**. Forward-only: there are no down migrations. Different slices use different number
prefixes (0010, 0020, ...) so concurrent work does not collide; `DB.Version()` is `MAX(version)`.

## Tables

| Table | Migration | Purpose |
|---|---|---|
| `settings(key PK, value JSON, updated_at)` | 0001 | preferences |
| `workspaces(id, name, position, layout JSON, created_at, updated_at)` | 0001 | saved layouts (widgets, tabs, panes) |
| `machines(id, name, kind local\|remote, os, transport, address, fingerprint, created_at)` | 0001 | machine registry (remote use is planned) |
| `projects(id, name, machine_id, path, remote_url, last_opened, UNIQUE(machine_id,path))` + `worktree_root`, `created_at` | 0001, 0010 | registered repositories |
| `worktrees(id, project_id FK, name, path, branch, base_ref, is_main, created_at)` | 0010 | Orole metadata per git worktree; git is the source of truth for existence |
| `mcp_servers(id, name, url, transport, adapter, enabled, auth_kind none\|bearer\|oauth, created_at)` + `oauth_client_id`, `scopes` | 0001, 0030 | configured MCP servers (**no secrets**) |
| `mcp_permissions(server_id FK, tool, decision allow\|ask\|deny)` | 0001 | per-tool decisions |
| `mcp_auth_state(server_id PK/FK, issuer, endpoints, registration_kind, client_id, scopes, authorized_at, access_expires_at)` | 0030 | non-secret record of the last OAuth discovery |
| `mcp_cache(server_id PK/FK, fetched_at, tools, resources, prompts, ...)` | 0030 | last discovered capabilities; server-controlled, untrusted display data |
| `policy_rules(id, actor, action, resource, max_effect, decision, note, created_at, UNIQUE(...))` | 0030 | generic permission rules; most specific wins, deny > ask > allow |
| `audit_events(id, at, actor, action, target, outcome allowed\|denied\|error, detail)` + index on `at` | 0001 | audit log |
| `notifications(id, at, source, title, body, read, dismissed)` + `level` | 0001, 0050 | notification inbox |
| `file_roots(id, provider, path, label, created_at, UNIQUE(provider,path))` | 0050 | approved roots for the files providers; paths stored fully resolved |
| `agent_sessions(id, kind, status, ..., data JSON)`, `agent_events`, `agent_journal`, `agent_runs` | 0020 | agent runtime state and an at-most-once orchestration journal |

Tables for agents, MCP, policy and notifications exist ahead of the daemon services that use them; see
`daemon.md` for what is actually registered.

## Secrets

Never stored here. OAuth tokens, bearer tokens and PATs live in Windows Credential Manager
(`docs/architecture/credentials.md`); `mcp_servers.auth_kind` only says which kind applies.

## Adding a migration

New file `migrations/NNNN_<slice>.sql` with a unique, higher prefix; never edit an applied file; test through
`database.Open` on a temp dir (`internal/database/database_test.go` shows the pattern). Update this page.

## Chat tables (migrations 0096-0098)

| Table | Purpose |
|---|---|
| `bots`, `messages` (+ `messages_fts`), `chat_attachments`, `conversation_summaries` | 0096: bots as contacts, one continuous conversation each (`UNIQUE(bot_id, seq)`), FTS5 over message text, content-addressed attachment references, rolling summaries |
| `memory_scopes`, `memory_entries` (+ `memory_fts`), `bot_memory_access`, `memory_consolidation`, `skill_proposals`, `knowledge_files`, `knowledge_chunks` (+ `knowledge_fts`) | 0097: scoped core/fact memory with validity windows, per-bot scope grants, consolidation cursor, inert skill proposals, knowledge indexing |
| `llm_manifests` | 0098: declarative provider manifests with digest-based trust (same shape as `plugins`) |

Provider API keys are never stored here (`llm/<provider>/key` in Credential Manager). See `chat.md`.

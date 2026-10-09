# Chat: bots, providers, tools, memory

Chat mode (sidebar tab **Chat**) lists the user's bots as contacts. Each bot has one continuous conversation.
Decisions: `docs/decisions/0007-direct-api-chat.md`.

```
React (features/chat, stores/chat*.ts)  --MyGo Chat / LLM--> internal/app (stateless) --IPC--> daemon
                                                                                               |
        internal/llm  Provider registry: openrouter, openai  <----  internal/chat Manager + engine
        (credentials just-in-time)                                    |- store (SQLite: bots, messages, FTS5)
                                                                      |- tools: web_search/web_fetch/datetime,
                                                                      |   research, MCP, skills (internal/chat/tools)
                                                                      '- memory (internal/chat/memory)
```

## Packages
| Package | Owns |
|---|---|
| `internal/llm` | `Provider`, `Request`/`Delta`, `Caps`, errors, SSE helper, `Manager` (connect, test, defaults, OpenRouter PKCE, dynamic manifest providers), `openrouter/`, `openai/`, `manifest/` + `openaicompat/` (declarative providers), `llmtest/` (tests only) |
| `internal/chat` | bots, messages (seq per bot), run engine and queue, prompt assembly, compaction, attachments, draft runs; contracts in `api.go`, `memory_api.go`, `tool.go` |
| `internal/chat/tools` | hosted web tools mapping, `fetch_url` SSRF-safe fallback, research loop, MCP adapter (`InvokeChecked`, actor `bot:<id>`), skills tool, `chat.tools.list` info |
| `internal/chat/memory` | core memory, facts + FTS5 recall, consolidation, `ask_bot`, `propose_skill`, knowledge files, memory IPC |
| `migrations/0096_chat.sql`, `0097_chat_memory.sql`, `0098_llm_manifests.sql` | all tables |

## Run lifecycle
`chat.send` stores the user message, then the engine (one active run per bot; further sends are queued) assembles
the prompt (bot instructions, standing rules, memory snapshot taken once per run, recalled facts, rolling summary,
recent messages, current attachments), streams from the provider, runs client tools, and publishes `chat.message`
snapshots (at most one per 50 ms plus the final). Statuses: `pending`, `running`, `complete`, `incomplete`
(daemon restarted mid-run), `error`, `canceled`. Budgets: steps (default 8) and tool calls (default 20) per turn,
doom-loop detection, cancellation through `chat.cancel`.

## Tools
- Hosted: OpenRouter `openrouter:web_search` / `web_fetch` / `datetime`, OpenAI `web_search`. A tool is offered only
  when the provider+model can run it; `chat.tools.list` returns `available` plus a reason and what leaves the machine.
- Client: `fetch_url` (fallback), `research`, MCP tools, `orole_get_skill`, memory tools, `ask_bot`.
- MCP and policy: bot calls use actor `bot:<id>` and the existing approval flow.

## Memory
Scopes: `bot:<id>` (private), `user`, `shared:<name>`. A bot reads/writes its own scope and any granted scope
(`BotConfig.memory`), enforced in Go for snapshots, search and recall. Core notes are bounded (2,200 / 1,400 /
2,200 characters). Facts are invalidated, never deleted, when superseded. Consolidation runs when a conversation
goes idle, using the cheap "memory model" from Settings > AI providers. Bot writes can be held for approval.
Skill proposals are inert until approved in the UI. Knowledge files accept text-like files only (PDF indexing is not
supported yet).

## Attachments
Chunked upload (1 MiB chunks, 20 MiB per file, 10 per message), type sniffed from content, stored
content-addressed at `<DataDir>/chat/attachments/<sha256>`; staged uploads expire after 24 h.

## Security
Keys: Credential Manager only, never returned to the UI. All external content is fenced as untrusted. Policy decisions
are made in Go; the UI only renders. Persistence: bot data survives GUI restarts; runs live in the daemon and end
(`incomplete`) if the daemon exits.

## Declarative provider manifests (issue #97)
Settings > AI providers can add OpenAI-compatible providers (Ollama or LM Studio on loopback, hosted gateways) without
code. A manifest (`internal/llm/manifest`, schema v1) is strict JSON of at most 32 KiB: duplicate keys, unknown or
differently-cased fields and deep nesting are rejected. It declares `id` (lowercase slug, never a built-in id), `name`,
`baseUrl` (https; http only for `localhost`, `127.0.0.1`, `::1`; no credentials, query or fragment), `auth`
(`bearer`, `header` with a header name, or `none`), `modelsPath` (default `/models`), `chatPath` (default
`/chat/completions`), optional static `models` (`vision`, `pdf`, `tools`, `reasoning`, `contextWindow`) and optional
non-secret static `headers` (never `Authorization`, `Cookie`, `Host`, `Content-Length` or anything credential-like).
Secrets never live in a manifest: the key is stored by the Manager under `llm/<id>/key` (auth `none` has no key).

Trust follows MCP plugins. `migrations/0098_llm_manifests.sql` stores the canonical manifest and its SHA-256 digest.
Imports land disabled. A provider exists only while `enabled` and `approved_digest == digest`. Enabling asks the shared
policy engine (`llm.provider.trust`, resource `llm/<id>/<digest>`, actor `user`, destructive so it always asks, with the
host, auth placement and capabilities in the prompt). Saving a changed manifest disables it and clears the approval;
removing it, or moving it to another origin, also deletes the stored key so a key reviewed for one host never reaches
another. Revoking, changing or removing a manifest cancels requests already running on it.

`internal/llm/openaicompat` implements the provider: Chat Completions SSE with usage and function tools, no hosted
tools, images and PDFs only to models the manifest declares `vision`/`pdf` for. Models come from the models endpoint
(entries matching a declared model take its capabilities; others have none) and fall back to the declared list when the
endpoint is missing (404/405/501). The origin is pinned at review time: other origins and cross-host redirects are
refused, there is no proxy, loopback manifests may only dial loopback, every other manifest only public addresses (the
private-range list of `internal/automations/http.go`).

IPC: `llm.manifests.list|save|trust|remove` (`manifest.ManifestRecord`, `ManifestSaveRequest`,
`ManifestTrustRequest`, `ManifestRef`); every change publishes `llm.changed`.

## Not built
`@bot` mention routing in the composer, group conversations, embeddings (an interface exists), native OpenAI
deep-research (a seam exists).

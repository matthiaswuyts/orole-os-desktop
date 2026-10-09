# 0007: Chat talks to provider APIs directly

Status: accepted

## Context
Orole OS has a Code mode that drives installed harnesses (Claude Code, Codex, Oh My Pi). The Chat mode is a
different product: the user talks to their own specialised bots (contacts, each with one continuous
conversation) that use web search, research, file upload and memory. A harness owns its system prompt, tools
and context, so it cannot give Chat the control it needs over memory injection and the tool loop, and spawning
an agent process for every background memory call is slow and expensive.

## Decision
- **Direct provider APIs.** `internal/llm` defines `Provider` (streaming, tool calls, usage, capabilities). Built-ins:
  OpenRouter (Chat Completions SSE, server tools) and OpenAI (Responses API). Harnesses stay Code-only.
- **API keys only.** Keys live in Windows Credential Manager under `llm/<provider>/key` and are read just in time by
  the daemon. Subscription OAuth tokens of other tools (`~/.claude`, `~/.codex`) are never read: Anthropic's
  February 2026 terms forbid subscription OAuth in third-party tools. OpenRouter sign-in uses PKCE in headless
  mode (the user pastes the code), because AGENTS.md forbids a listener on a network interface.
- **Daemon owns everything durable.** Bots, messages, runs, tool loops, memory and attachments live in
  `internal/chat`. The desktop `Chat` / `LLM` services forward typed IPC; React renders events.
- **Bots are contacts.** One continuous conversation per bot, no thread list. Continuity comes from a rolling
  summary (compaction) and memory.
- **assistant-ui scope.** `@assistant-ui/react` is used for the Chat conversation surface only
  (`useExternalStoreRuntime` fed by daemon events). Issue #77's "no competing chat runtime" governs the harness Code
  conversation, which is unchanged.
- **Memory built in-house on SQLite** (FTS5): bounded curated core notes injected as a per-run snapshot
  (Hermes pattern), archival facts with validity windows and idle consolidation (Mem0/Zep patterns), scoped sharing
  (Letta). Mem0, Letta, Cognee and Graphiti need Python or a graph database as a second process and would hold
  data outside the policy engine, so they are references, not dependencies.
- **Untrusted by default.** Web results, fetched pages, attachments, MCP results and other bots' answers are
  fenced (`chat.Fence`) and never treated as instructions; facts derived from them are stored `untrusted` and never
  promoted to core memory automatically.

## Consequences
- Users need an API key (or an OpenRouter account). Subscription-only users keep using Code mode.
- Capability data (hosted tools, vision, PDF) comes from provider catalogues; OpenAI's table is a small versioned
  list that must be maintained.
- Costs are the user's: per-turn step and tool-call budgets exist in every bot, and provider-reported usage is shown
  as reported, never estimated.

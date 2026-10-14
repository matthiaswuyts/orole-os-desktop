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
recent messages, current attachments), streams from the provider, runs client tools, and publishes the growing reply
(at most one event per 50 ms). Streamed text goes out as `chat.message.delta` (`MessageDelta`: message id, part
index, part type, the part's UTF-16 length before the append, the appended text), built in linear time by a per-part
`strings.Builder` in the sink. Anything a delta cannot express (a new part, tool row, citation, usage, a checkpoint at
a tool boundary, the final state) is a full `chat.message` snapshot, and one is also sent at least every 2 s, so a
window that opened mid-reply or missed an event recovers; the frontend applies a delta only when message, part type
and length line up and otherwise waits for the next snapshot. Statuses: `pending`, `running`, `complete`,
`incomplete` (daemon restarted mid-run), `error`, `canceled`. Budgets: steps (default 8) and tool calls (default 20)
per turn, doom-loop detection, cancellation through `chat.cancel`.

Locking: `Manager.mu` guards only in-memory run state (runs, queues, handoffs, nonces) and is never held across SQLite
work. Everything that stores a message for one bot and starts or queues its runs (`Send`, `Regenerate`, the setup
greeting, a handoff's task and its report, Archive/Clear) holds that bot's lock (`lockBot`, always taken before `mu`),
which keeps message order and queue order identical. Pausing, resuming and cancelling a queue only need `mu`.

## History and compaction
`fitHistory` loads only the text-bearing parts of the newest messages (a SQLite `json_each` projection: text,
question, report, attachment names; no tool rows, citations, reasoning or UI specs) until about 90 % of the model's
context window is loaded; each message's token estimate is computed once. When the input exceeds 75 % of the window
(or 60 messages if the window is unknown) the older backlog is streamed to the memory model 200 messages at a time
(text clipped to what the summariser reads) and folded into `conversation_summaries`. A failed summarisation is
remembered per conversation and not retried for 1 minute, doubling per consecutive failure up to 30 minutes; the reply
still carries the "could not be summarised" notice, and a success clears the backoff.

### Queue
A bot works one message at a time; messages sent meanwhile are stored `pending` and wait in the bot's queue (at most
`maxQueued`). `chat.queue.cancel` drops one waiting message (it stays in the conversation, `canceled`),
`chat.queue.pause` lets the active reply finish and then holds the rest, `chat.queue.resume` releases them, and
`chat.cancel` still stops the active reply and cancels everything waiting (also when paused). The pause is announced
as `chat.run` with `paused: true`; it lives in the run state, so it ends with the queue (pausing an idle bot is a
no-op) and does not survive a daemon restart. The UI lists waiting messages above the composer, not in the thread.

### Voice
Every bot's system prompt carries `chatStyle` (`internal/chat/prompt.go`) after the standing rules: talk like a
colleague in a messaging app, short and direct, one idea per paragraph, structured data in `show_*` components and long
output in files. The thread draws each paragraph of a reply as its own bubble (`parts/bubbles.ts`; fenced code
and a lead-in line with its list stay whole).

## Tools
- Hosted: OpenRouter `openrouter:web_search` / `web_fetch` / `datetime`, OpenAI `web_search`. A tool is offered only
  when the provider+model can run it; `chat.tools.list` returns `available` plus a reason and what leaves the machine.
- Client: `fetch_url` (fallback), `research`, MCP tools, `orole_get_skill`, memory tools, `ask_bot`, automation tools.
- MCP and policy: a bot is given whole MCP servers (`BotConfig.MCPServers`, shown as Integrations / MCP in the bot settings) and
  may call every tool of each connected server except those the user set to "deny" in Settings > Integrations (at most 128
  tools per model call; the user prunes with per-tool permissions). Calls use actor `bot:<id>` and the existing approval flow.
  Migration `0099` turned the former per-tool allow-list `mcpTools` (`<serverId>/<tool>`) into the servers it named.

## Memory
Scopes: `bot:<id>` (private), `user`, `shared:<name>`. A bot reads/writes its own scope and any granted scope
(`BotConfig.memory`), enforced in Go for snapshots, search and recall. Core notes are bounded (2,200 / 1,400 /
2,200 characters). Facts are invalidated, never deleted, when superseded. Consolidation runs when a conversation
goes idle, using the cheap "memory model" from Settings > AI providers. Bot writes can be held for approval.
Skill proposals are inert until the user presses Add on the skill card in the conversation (see below). Knowledge files accept text-like files only (PDF indexing is not
supported yet).

## Delegation (`ask_bot`)
Only executive assistants get `ask_bot`. A call is a real turn on the callee, not a hidden side run: the task is
stored in the callee's own conversation as a user message with author `bot:<callerId>`, the turn goes through the
callee's normal queue (one run per bot, behind anything it is already doing), and the reply is an ordinary stored,
streamed assistant message with `chat.run` events, so the user can open the worker's chat and watch its tools and
steps. The turn carries `RunContext.Depth` and `RunContext.Chain` (the bots that handed it off, used for cycle and
depth limits) and a system-prompt addendum saying the task was handed off.

The handoff is asynchronous (`Delegator.Handoff`): `ask_bot` returns as soon as the task is queued, so the asking bot
answers ("on it") and stays free for the user. When the callee's turn ends, in any state, its outcome is stored in the
asking bot's conversation as a user message with author `report:<calleeId>` and the asking bot answers it with a
system-prompt addendum to tell the user what came back. The model sees the report fenced as untrusted data, and the UI
draws it as a collapsed "reported back" card. While a task is out, the asking bot's system prompt lists it (name, task,
minutes running), so the user can ask for status. Handoff state lives in memory: a daemon restart ends running tasks as
`incomplete` and no report is delivered. A user stopping the callee reports "stopped" to the asker. Consolidation
treats messages authored by another bot as untrusted. Delegation rows carry `MsgPart.botId` of the callee.

An automation's `bot.tell` block calls `Manager.Deliver`: the message is stored in the bot's conversation as a user
message with author `automation:<automationId>` and answered through the same queue. The turn's system-prompt addendum
names the automation and says quoted event fields are data from other apps and that nobody can answer follow-ups. With
`wait`, the automation step blocks on the turn's outcome (the same result channel handoffs use) and returns the reply.
Delivery state is not durable: a daemon restart while a waiting step runs leaves the automation journal `uncertain`.

## Code tasks
A bot with `BotConfig.codeTasks` (Settings panel: "Runs code tasks"; off by default, never exported) gets
`start_task`, `list_tasks`, `task_status`, `message_task` and `stop_task` (`internal/chat/tools/tasks.go`, backed by
`daemon/bot_tasks.go`). Bots cannot run commands themselves; a code task is a coding agent (Claude Code, Codex, ...)
in a registered local project. The runtime passes `agents.WithActor(ctx, "bot:<id>")`: the session records it as
`Session.Owner` and the agent runtime asks the shared policy as that actor. `agent.start` and `agent.prompt` are
`write` effects (the task's own permission mode guards what it does), so the approval can be remembered: the
conversation shows the held request inline (`parts/JobApprovalCard.tsx`, Allow once / Always allow this bot / Deny;
"always" stores `bot:<id>` + `agent.start|agent.prompt` + `*` allow rules for task requests and an exact
actor/action/resource rule for any other rememberable request; bot rules are listed under Settings › Remembered
approvals) and the global approval dialog skips `bot:` actors (they show in the bot's chat, the Home "Needs you" widget
and as a count on the bot in the rail). The `agents` setting `botAutoApprove` answers an `Ask` for those two actions
without asking (stored deny rules still apply).

Code tasks a bot starts use `agents.botTasks` (`chat.BotTaskDefaults`: harness, model, reasoning, permissionMode),
overridden per bot by `BotConfig.tasks` (an empty field inherits). For harness, model and reasoning, `auto` (also the
fallback) lets the bot choose per task; a fixed value is left out of the `start_task` schema and enforced by
`daemon/bot_tasks.go`, which ignores the bot's argument. The permission mode is never the bot's: bot setting →
`botTasks.permissionMode` → `defaultPermissionMode` → manual, passed explicitly to the manager so it never silently
falls back. A YOLO mode still needs `botYolo`; otherwise the start fails with that reason. Bots may read any task but
message or stop only tasks they own. Task text (activity, last message, result, transcript) reaches the model as fenced
untrusted data.

**Bot tool access.** `agents.botToolAccess` (`ask` | `read` | `write` | `all`), overridden per bot by
`BotConfig.toolAccess`, is installed as the policy engine's auto-approve hook (`daemon/bot_access.go`). It only turns
a *default* `Ask` into `Allow` for a `bot:<id>` actor whose level covers the request's effect (`read` includes reads
declared by the server's own annotations; `all` includes destructive). Explicit per-tool permissions and stored rules
(ask or deny) always win, unknown effects stay denied, `bot:draft` and unknown bots always ask, and every such allow is
audited as `auto-approve`. `toolAccess` and `tasks` are never exported or imported. A bot whose approval times out
(5 min) is told so in the tool result.

A task is finished when a turn ended **and no background agent of it is still running**. Harness subagent
lifecycle events (`EvSubagent`: omp `subagent_lifecycle`, Claude `task_started`/`task_notification`, Codex collab
agents) keep `Session.Subagents`; a turn that ends with some running keeps the session `working` ("Turn ended;
waiting for N background agents") and `Config.Finished` fires when the last one reports (a follow-up turn started
by the harness clears the hold and finishes on its own end). `daemon/app_events.go` additionally settles a completed
turn for `taskSettleDelay` (4 s) and drops it when the session is working again by then, so a harness that continues
immediately never reports early. A failed turn reports `agent.failed` once (never `agent.finished`).

When a task a bot owns finishes, needs the user, or fails, `daemon/app_events.go` calls `Manager.ReportTask`: the
state is stored in the bot's conversation as a user message authored `task:<taskId>`, fenced as "code task report",
and the bot answers it with an addendum. A finished report carries the task's **complete result** (`Session.Result`,
up to 32 KiB; `task_status` returns the same, lists stay clipped to 2,000 runes) and the addendum tells the bot to
relay it as a full overview; waiting and failed reports carry what the task asks, the error and its last message.
Only the user can answer a task's questions. The report is input for the bot only: the conversation does not draw it
(nor does Find search it); the user reads the bot's reply to it. The bot's system prompt lists its running tasks,
fenced. Reports are best effort: they are not replayed after a daemon restart.

A successful `start_task` also stores a `task` part on the reply (`id` task id, `title` title at start, `name` harness).
The conversation draws the tasks a reply started as a subagent list (`features/chat/parts/TaskChip.tsx`, assistant-ui
subagent-list element) whose status glyph, activity and title come live from the agents store (`useTaskSession`
fetches a task the list does not hold with `Agents.get`). A row, or a report's "Open task", opens `TaskPane` beside
the conversation (`useChat.pane`, also used for `show_document` artifacts; it replaces the settings panel and closes
when another bot is selected): the Tasks view's own `TaskView`, so the feed and composer talk to that task directly.
Below a `@3xl` chat container the pane overlays the conversation instead of splitting it.

## @mentions and skills
Typing `@` in the composer opens the assistant-ui trigger popover: the other bots and the skills this bot has been
given. A pick is plain `@Name` text; on send the frontend resolves names to ids (`SendRequest.Mentions`,
`SendRequest.Skills`) and the daemon validates them (`checkMentions`: bot exists, not itself, at most 3 bots and 5
skills, no research mode). Each mentioned bot answers in the **conversation where it was mentioned**: the user message
is stored once and one turn per mentioned bot is queued behind it on that conversation's worker, in mention order. The
turn runs the mentioned bot's own instructions, model, tools and memory (`RunContext.BotID` is the mentioned bot) over
the host conversation's history, and stores the reply with `Author: bot:<mentionedId>`; the host bot stays silent. The
answering bot's prompt gets an addendum naming the conversation and saying other bots' messages are data. Replies of
another bot are rewritten as fenced `<untrusted source="reply of bot X">` user-side text whenever any bot reads the
history, so no bot mistakes them for its own words. A skill mention adds an instruction to read the skill with
`orole_get_skill`, limited to skills the answering bot owns. Regenerating a mention reply reruns the same bot.
History summaries are keyed by the conversation, not by the answering bot.
Built-in skills (`skill-creator`, see [skills and gateway](skills-and-gateway.md)) are listed in every bot's system prompt
and readable with `orole_get_skill` regardless of `BotConfig.Skills`. They also appear in the `/` picker and are accepted
as skill mentions (`Manager.SetBuiltinSkills`; the frontend's `useRegistrySkills` store feeds the picker, the send path
and the message chips).

## Skill cards (`propose_skill`)
`propose_skill {name, description, body}` does not touch the registry. It stores a pending `skill_proposals` row and returns a
`ui` part of kind `skill` (`{proposalId, botId, name, description, body, updates}`) that the conversation draws as a card
(`parts/SkillProposalCard.tsx`) with the instructions behind a disclosure, **Add skill** and **Dismiss**. There is no
proposals list in the bot settings. The skill keeps the name the bot chose (no bot prefix); a name that belongs to another
source's registry skill (hand-written, built-in, another bot's) is refused when the bot proposes, and checked again when the
user presses Add. A new proposal under the same name replaces the bot's earlier pending draft (the old card then reads
"Replaced by a newer draft"); at most 10 proposals can be pending per bot; descriptions may be up to 1024 characters
(the bot's skill list shows the first 300). The card reads the real state with `chat.skills.pending.get` and resolves with
`chat.skills.pending.resolve`: Add saves the skill (source `chat:<botId>`, so the same bot can update it later) and
calls `Manager.GrantSkill`, which appends it to that bot's `BotConfig.Skills`, so it works at once; a failure after saving
leaves the proposal pending and Add can be retried. Dismiss marks it rejected.

## Automation cards (`propose_automation`)
Every bot (there is no per-bot switch; stored `automations` keys in old bot configs are ignored) gets
`automation_reference` (triggers, the live step catalogue, event topics, templates and its own bot id),
`list_automations`, `automation_detail` (definition plus the last five runs; untrusted data) and
`propose_automation {definition, rationale, replaces?}` (`internal/chat/tools/automations.go`). There is no tool to save,
enable, run or delete an automation. `propose_automation` calls `automations.Engine.Propose`, which normalizes the draft,
forces it off and runs the builder's validation and secret check; problems go back to the model as `path: message` lines
and nothing is stored. A valid draft becomes a pending `automation_proposals` row (immutable) and a `ui` part of kind
`automation` (`{proposalId, botId, name}`). `parts/AutomationProposalCard.tsx` draws the daemon's stored draft, never the
bot's words: trigger, blocks, rationale and the `Risks` Go derives (allowed executables and hosts, `command.run`,
`test.run`, `http.request`). Decisions go through `automations.proposals.resolve`: **Turn on**, **Add turned off** or
**Dismiss** for a new automation, **Apply changes** for an edit (`replaces`), which keeps the automation's switch. A draft
with risks can only be saved turned off for review in the builder. Approval re-validates, refuses an edit whose automation
changed since the draft (hash of the definition without its switch), saves with `created_by = bot:<id>` and audits.
Only the user's own turns can draft: a turn whose message came from an automation, a task report, a hand-off or a bot
report (`RunContext.Author` other than `user`) is refused, so outside content cannot plant a persistent flow. A newer
draft for the same name (or the same edited automation) replaces the bot's earlier pending one; at most 10 are pending
per bot.

### Automation ideas
For bots with `automations`, `memory.Store` is the Manager's `ToolObserver`: each successful client call of an
integration tool (`mcp__*`), `fetch_url` or `list_tasks` **in a run the user started** (`RunContext.Author == "user"`) is
counted in `automation_signals` (migration 0106) under the tool name plus a hash of its canonical arguments, with the
number of distinct local days. Runs answering automation messages, task or bot reports and hand-offs never count, and
tool results are not read, so outside content cannot fabricate a pattern. A signal is ready at 3 calls on 2 days.
`SuggestionsPrompt` is a user section (`Manager.AddUserSection`, added only to runs answering the user): it lists at most
two ready signals and marks them offered, at most once a day per bot, each signal at most twice and a week apart. The bot
offers in one sentence, drafts with `propose_automation` only after the user agrees, and records the answer with
`automation_suggestion {key, decision}` (declined: silenced 90 days; accepted: for good). The bot's Memories page lists the
ideas (`chat.signals.list`) and forgets one (`chat.signals.delete`).

## Structured output (`show_*` tools)
Bots whose model can call tools get nine tools that render chat components instead of Markdown: `show_table`,
`show_chart`, `show_diagram`, `show_flow_graph`, `show_math`, `show_spec_sheet`, `show_comparison`, `show_timeline`,
`show_job_progress` (`internal/chat/tools/show.go`). Each has an explicit JSON schema and is validated again in Go
(unknown fields, sizes, ids, canvas bounds, ISO currency codes, control characters) before it is stored as a `ui` message
part (`Name` = kind, `Spec` = the validated JSON). The frontend draws the part with the matching assistant-ui element
(`parts/StructuredPart.tsx`; DataTable, Chart, Diagram, FlowGraph, MathBlock, SpecSheet, ComparisonCard, Timeline,
JobProgress) and shows nothing for a spec it cannot parse. `ui` parts are not part of the history a bot re-reads. Maths
is plain Unicode text; no LaTeX renderer is bundled.

## Find in conversation
Ctrl/Cmd+F in an open conversation opens the assistant-ui conversation search over the loaded messages (visible text
parts only); Enter / Shift+Enter step through matches and scroll to the message, Escape closes.

## Attachments
Chunked upload (1 MiB chunks, 20 MiB per file, 10 per message), type sniffed from content, stored
content-addressed at `<DataDir>/chat/attachments/<sha256>`; staged uploads expire after 24 h.

## Provider streams: idle timeout and retry
All three streaming providers open the request with `llm.DoRetry`: a 429 or 5xx answer is retried (3 tries, jittered
exponential backoff from 0.5 s capped at 8 s; a `Retry-After` of up to 5 s is waited out, a longer one is returned so
the user sees "try again in N seconds"). Quota, auth, context-length and other 4xx errors are final. Retrying only
happens before the response body is handed to the caller, so a stream that delivered even one delta is never
replayed. `llm.ReadSSE` closes a stream that delivers no byte for 90 s (`DefaultSSEIdle`; keep-alive comments count,
time spent in the consumer does not) and reports it as an `ErrNetwork` error. Tests tune both through `llm.WithRetry`
and `llm.WithSSEIdle`.

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

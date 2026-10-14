# Jarvis mode: phone calls with your bots

Status: implemented on `feat/jarvis-mode` (see section 14 for where the build differs) · Date: 2026-10-14

## 1. Goal

A per-bot feature flag, **Jarvis mode**, that lets a bot phone the owner and lets the owner phone their bots.

- A bot calls when the owner told it to (explicit rules) or when its standing call policy says an event is worth a call.
- Every call opens with a freshly written line that contains the bot's name. No call ever says "from Orole" or names the app.
- On the call the owner talks freely ("leave it", "research this", "check my automations", "start a coding task", follow-ups).
  The bot answers with **its own model and tools, fully live**: it can start code tasks, research and automations during the call.
- The owner can dial the Jarvis number, be recognised by caller ID, and talk to the executive bot or a named bot.
- Latency is a first-class requirement: first audio on pickup without waiting for a model, streaming end to end.

Non-goals (v1): SMS, multiple owners or phone numbers, calling third parties, conference calls, call recording audio
(transcripts only), a self-built speech pipeline.

## 2. Decisions taken

| Topic | Decision |
|---|---|
| When to call | Both: a standing "When to call me" policy text per bot, plus structured rules created from chat. |
| Live capability | Fully live: all of the bot's tools run during the call. |
| Approval model | No confirmations. Jarvis can only be enabled when the bot has full permissions (see §4). |
| Direction | Outbound and inbound. Inbound is accepted only from the verified owner number; no passphrase. |
| Voice stack | Twilio (number, PSTN) + ElevenLabs Agents with **custom LLM** pointing at our relay. The brain is the bot's own model. |
| Call model | The bot's own provider and model with reasoning forced off/low. |
| Voice | ElevenLabs voice and TTS model selectable globally, overridable per bot. Model list fetched from the account. |
| Relay | Cloudflare Worker + Durable Object. The daemon connects outbound; the daemon never listens on a network interface. |

Rejected: our own STT/LLM/TTS pipeline over Twilio Media Streams (much more work, worse latency through the home uplink);
OpenAI Realtime over SIP (the voice brain would be OpenAI's realtime model, not the bot's).

## 3. Architecture

```
 phone ⇄ Twilio ⇄ ElevenLabs Agent ──HTTPS──▶ Relay (CF Worker + Durable Object) ◀──WSS (outbound)── orole-daemon
                         (STT, turn-taking,     /llm  /inbound  /status                                 internal/jarvis
                          TTS, barge-in)                                                                 internal/chat (voice turn)
```

| Unit | Location | Responsibility |
|---|---|---|
| Relay | `relay/` (TypeScript, Cloudflare Worker + one Durable Object) | Authenticates provider requests, terminates the custom-LLM endpoint (OpenAI-compatible chat completions, SSE), inbound and status webhooks, and holds exactly one authenticated WebSocket from the daemon. Forwards frames; keeps no data beyond in-flight call state. |
| `internal/jarvis` | daemon | Permission gate, call policy (rules, quiet hours, caps, cool-down), call lifecycle state machine, relay client, provider API clients (Twilio, ElevenLabs), opener generation and check, inbound caller check, call records. |
| `internal/chat` voice turn | daemon | Runs a spoken turn through the existing engine (`executeTurn`/`loop`) with voice options. |
| `internal/chat/tools/jarvis.go` | daemon | Bot tools `jarvis_rule_add`, `jarvis_rule_list`, `jarvis_rule_delete`, `call_owner`. |
| `internal/app` Jarvis service | desktop | Stateless typed service forwarding to the daemon over IPC (settings, rules, calls, test call). |
| Frontend | `frontend/src/features/jarvis/` | Settings › Phone (global setup), the bot's Jarvis section, the call message part. |

Layer rules from AGENTS.md hold: all decisions in Go in the daemon; secrets only in Credential Manager; provider
content (transcripts, webhook payloads) is untrusted data.

### 3.1 Relay protocol

Daemon → relay: `wss://<relay>/daemon` with `Authorization: Bearer <relay token>`. One connection; a new authenticated
connection replaces the old one. Ping every 15 s; the daemon reconnects with backoff (0.5 s → 10 s cap).

Frames are JSON `{type, callId, seq, ...}`:

| Direction | Type | Meaning |
|---|---|---|
| relay → daemon | `llm.request` | A custom-LLM request from ElevenLabs: messages, call variables (`call_id`, `bot_id`), request id. |
| daemon → relay | `llm.delta` / `llm.done` / `llm.error` | Streamed text for that request; the relay writes it out as OpenAI-style SSE chunks. |
| relay → daemon | `inbound.lookup` | Inbound webhook: caller id, called number, Twilio call SID. |
| daemon → relay | `inbound.answer` | `{accept, dynamicVariables, overrides}` or `{accept:false}`. |
| relay → daemon | `call.status` | Twilio/ElevenLabs status (ringing, answered, machine-detected, completed, failed) and post-call data. |
| relay → daemon | `call.hangup.request` / daemon → relay `call.hangup` | End a call from either side. |

Provider authentication at the relay:
- ElevenLabs custom-LLM requests: the agent is configured with a secret request header; the relay compares in constant time.
- ElevenLabs inbound and post-call webhooks: HMAC signature verification (shared webhook secret).
- Twilio status callbacks: `X-Twilio-Signature` verification with the auth token stored as a Worker secret.
- If the daemon socket is absent, `/llm` answers with a single spoken line ("I can't reach your bots right now.") and
  ends the call; `/inbound` rejects.

The relay never stores transcripts. Worker secrets: relay token, ElevenLabs header secret, ElevenLabs webhook secret,
Twilio auth token. The relay token is generated by the daemon at setup and also stored in Credential Manager.

### 3.2 Provisioning

The daemon creates and updates, through provider APIs, everything the user would otherwise click together:
- the ElevenLabs agent (custom LLM URL = relay `/llm`, secret header, empty first message, voice/TTS defaults, latency-oriented
  turn settings, inbound personalization webhook = relay `/inbound`, post-call webhook = relay `/status`);
- the import of the Twilio number into ElevenLabs and its assignment to the agent;
- Twilio status callback and answering-machine detection settings on outbound calls.

Relay deployment: `relay/` ships with `wrangler.toml`; a documented `bunx wrangler@4 deploy` plus `wrangler secret put`
steps. Settings › Phone shows a step-by-step guide and verifies the relay with a handshake.

## 4. Permission gate

Jarvis may be enabled for a bot, a call may start, and every tool call during a call may run only while **all** hold:

1. the bot's effective tool access is `all` (`BotConfig.ToolAccess`, or the global setting when empty);
2. the bot's effective code-task permission mode is `yolo` (`BotConfig.Tasks.PermissionMode`, or the global default);
3. the global `agents.botYolo` setting is on and `agents.yoloDisabled` is off;
4. the bot has code tasks enabled (`BotConfig.CodeTasks`).

Enforced in the daemon: on saving `Jarvis.Enabled=true` (refused with a message naming the failing condition), at call
start, and before each tool call during a call. (As built, the timing differs: see §14, "Gate timing".) If the gate fails mid-call the action is refused, the bot says so,
`Jarvis.Enabled` is set to false and a `jarvis.disabled` audit entry plus a notification are written. Explicit per-tool
`deny` decisions in the shared permission policy still apply. Policy `ask` decisions that would need a UI approval are
answered with "not available on a call, queued for the app" instead of blocking the call.

## 5. When a call starts

### 5.1 Rules (deterministic)

The owner says in chat e.g. "call me when the deploy task finishes". The bot calls `jarvis_rule_add` and confirms the
rule in its reply. The daemon evaluates rules against events; no model has to notice anything.

Triggers (v1):

| Trigger | Target | Event source |
|---|---|---|
| `task_finished`, `task_failed` | a task id, or "any task started by this bot" | agents manager task state changes |
| `automation_finished`, `automation_failed` | an automation id, or any | automations run results |
| `notification` | a notification kind plus optional source match | notifications store |
| `at_time` | a local wall-clock time (once) | daemon scheduler |

Rule fields: `once | repeat`, optional `expiresAt`, `quietHoursOverride` (bool), `urgent` (bool), `note` (≤ 280 chars,
given to the bot on the call). `once` rules are deleted after they fire. Rules are listed and deletable in the bot's
Jarvis section and with `jarvis_rule_list` / `jarvis_rule_delete`.

### 5.2 Bot judgement

`call_owner(reason, urgency)` is offered to Jarvis-enabled bots. The bot's prompt contains the owner's "When to call me"
policy text. The call is placed only if the daemon's checks pass.

### 5.3 Daemon checks before dialling

Permission gate (§4); setup complete and relay connected; quiet hours (rules may override); daily call cap; monthly
estimated-cost cap; per-bot cool-down of 10 minutes for `call_owner` (rules are exempt); no call in progress. If a call
is in progress, a fired rule's brief is injected into the live call ("also, the deploy finished") instead of dialling.
A refused call becomes a notification stating the reason.

## 6. The call

### 6.1 Outbound lifecycle

`queued → dialing → ringing → in_progress → completed | no_answer | voicemail | failed | canceled`

1. The daemon builds the **call brief**: trigger, rule note, relevant task result or automation run, a snapshot of
   running tasks and recent automation results. Untrusted content in the brief is fenced as data.
2. The daemon places the call immediately (ElevenLabs outbound-call API via the Twilio number) with dynamic variables
   `call_id`, `bot_id`, and per-call overrides for voice and TTS model.
3. While the phone rings, the daemon asks the bot's model for the **opener** from the brief.
4. Opener check in Go: must contain the bot's name (case-insensitive, word boundary), must not contain "Orole"
   (case-insensitive), ≤ 240 chars, single paragraph. Failing or not ready at pickup → a fallback from a varied template
   set, e.g. "{name} here, quick one: {reason}." / "Hey, it's {name}. {reason}." Templates never mention the app.
5. On answer, ElevenLabs requests the first turn from `/llm`; the daemon streams the cached opener at once.
6. ElevenLabs voicemail detection (system tool `voicemail_detection`): on machine or no answer the call is ended without speaking task details. The owner gets
   a notification and a chat message ("Atlas tried to call: migration task failed."). An `urgent` rule retries once after
   5 minutes; nothing else retries.

### 6.2 Inbound lifecycle

1. ElevenLabs' inbound personalization webhook → relay `/inbound` → daemon `inbound.lookup`.
2. Caller id must equal the verified owner number (E.164 compare). Otherwise `accept:false`: the call is rejected, nothing
   is spoken, an audit entry is written.
3. The executive bot answers with a fresh greeting that contains its name, unless the owner names another bot; only
   bots passing the §4 gate are reachable. Switching bots mid-call swaps voice, model, tools, policy and brief; the new bot introduces itself by name.
   (As built: the voice and the call card stay the first bot's; see §14.)

### 6.3 A spoken turn

`ElevenLabs → relay /llm → llm.request → jarvis → chat.Manager.VoiceTurn → model + tools → llm.delta → SSE → ElevenLabs TTS`

`VoiceTurn` reuses the existing engine with options:
- reasoning forced to off (or the provider's lowest level when off is unsupported);
- a voice prompt section: spoken style, one to three sentences, no markdown or lists, never name the app, read numbers
  and identifiers in a speakable way;
- the call brief as a user-context section, stable across turns for prompt caching;
- no attachments, no compaction, step limit 6, tool-call limit 10;
- the same toolset and permission policy as chat, acting as `bot:<id>`.

ElevenLabs sends the full conversation each turn; the daemon keeps its own per-call history keyed by `call_id` and uses
only the newest user utterance from the request, so prompt prefixes stay identical between turns.

**Fillers.** When the model emits a tool call, the daemon immediately streams a scripted line chosen from the tool's
metadata ("Checking your automations.", "Starting that coding task now."), then the model's continuation.

**Long work.** Code tasks and research are started and the bot says so; it does not wait. If the work completes while
the call is live, the result is injected as context for the next turn and the bot mentions it. Otherwise the existing
completion notifications and any matching rules apply.

### 6.4 Call record

Each call gets one assistant message with a `PartCall` card, created at call start and updated at call end; the spoken turns are stored between it as ordinary user/assistant messages. The card shows the call's direction (outbound or inbound), status, reason, duration and estimated cost; the transcript is the normal chat messages around it. Tool, task and research parts produced
during the call are attached to the same message so the chat shows them exactly as if typed. The bot sees the call in its
later history (transcript summarised when long).

## 7. Speed budget

Target: first audio ≤ 300 ms after pickup (cached opener); typical reply start ≤ 1.2 s after the owner stops speaking.

- Opener generated during ringing; empty ElevenLabs first message; daemon streams the cached text.
- Brief prefetched before dialling so common questions need no tools.
- Persistent daemon↔relay socket; the Durable Object is placed near the daemon (first connector).
- Streaming everywhere; ElevenLabs starts TTS on the first sentence.
- Reasoning off; short answers; stable prompt prefix for provider prompt caching.
- Instant fillers on tool calls.
- Default TTS model is ElevenLabs' lowest-latency model; the picker shows a latency hint per model.
- Daemon logs per-turn timings (STT end → request in, first token, first delta out) in the call record for tuning.

## 8. Settings and data

### 8.1 Global (Settings › Phone)

- Credentials (Credential Manager, never SQLite or the frontend): Twilio Account SID + auth token, ElevenLabs API key,
  relay token, ElevenLabs webhook and header secrets.
- Plain settings: relay URL, owner phone number (E.164, verified by a test call that reads a 6-digit code entered in the
  app), quiet hours (start, end, timezone), daily call cap (default 10), monthly cost cap (default $20), default voice id,
  default TTS model id.
- Status: setup checklist (Twilio ✓, number ✓, ElevenLabs ✓, relay connected ✓, phone verified ✓), last error,
  this month's call count and estimated cost.
- When setup is incomplete the bot's Jarvis toggle is replaced by a link to this screen.

### 8.2 Per bot

`BotConfig.Jarvis`:

```go
type BotJarvis struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Policy   string `json:"policy,omitempty"`   // "When to call me", ≤ 2000 chars
	VoiceID  string `json:"voiceId,omitempty"`  // "" = global default
	TTSModel string `json:"ttsModel,omitempty"` // "" = global default
}
```

The bot's Jarvis section shows: toggle (with gate status and which condition fails), policy text, voice and TTS model
pickers with a preview button, the rule list, and recent calls.

### 8.3 Migration `0109_jarvis.sql`

```sql
CREATE TABLE jarvis_rules (
  id TEXT PRIMARY KEY,
  bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  trigger TEXT NOT NULL,        -- task_finished | task_failed | automation_finished | automation_failed | notification | at_time
  target TEXT NOT NULL DEFAULT '', -- id, "any", notification kind[:source], or HH:MM
  repeat INTEGER NOT NULL DEFAULT 0,
  urgent INTEGER NOT NULL DEFAULT 0,
  quiet_override INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT '',
  expires_at INTEGER,           -- unix ms
  created_at INTEGER NOT NULL
);
CREATE TABLE jarvis_calls (
  id TEXT PRIMARY KEY,
  bot_id TEXT,                  -- null until an inbound caller picks a bot
  direction TEXT NOT NULL,      -- outbound | inbound
  rule_id TEXT,
  reason TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  provider_call_id TEXT,        -- Twilio call SID
  conversation_id TEXT,         -- ElevenLabs conversation id
  message_id TEXT,              -- chat message holding PartCall
  started_at INTEGER, answered_at INTEGER, ended_at INTEGER,
  est_cost_micros INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX jarvis_calls_created ON jarvis_calls(created_at);
```

(The final number follows whatever migration is last on `main` when implemented.)

## 9. Failure handling

| Situation | Behaviour |
|---|---|
| Daemon offline / relay socket down | No outbound calls. Inbound: one spoken line, hang up. Missed rule events while down become notifications on reconnect, not late calls. |
| Socket drops mid-call | Relay waits 5 s for reconnect; then speaks "Lost connection, I'll message you." and ends. Daemon posts what it had to chat. |
| Model or tool error | Spoken briefly ("That failed: …"), recorded in the call message; the call continues. |
| Provider API error when placing a call | Call marked `failed`; notification with the provider's error. |
| Caps reached | Call not placed; notification with the reason. |
| Gate fails mid-call | Action refused, bot says so, Jarvis disabled for the bot, audit + notification. |
| Opener invalid or late | Fallback template. |
| Prompt injection in task output, mail, MCP results | Content stays fenced as data in the brief and tool results; standard chat rules apply. |

## 10. Security notes (threat model additions)

- New trust boundary: the relay. It sees transcripts in flight and the custom-LLM traffic; it stores nothing. A stolen
  relay token lets an attacker impersonate the daemon to the relay (receive calls' LLM traffic) but not reach the daemon.
  Rotate from Settings › Phone.
- Full-permission bots on the phone mean whoever is on the call can direct the bot. Mitigations: outbound only to the
  verified number, voicemail detection with no details spoken. Inbound acceptance is caller-ID match only.
- Accepted risk (owner decision, 2026-10-14): inbound has no second factor. Caller ID can be spoofed, and a spoofed call
  from the owner's number reaches a full-permission bot. Every inbound call is audited and shown in the bot's chat, and
  the owner gets a notification for each inbound call, so a call they did not make is visible immediately.
- Audit entries: `jarvis.enabled`, `jarvis.disabled`, `jarvis.call.started`, `jarvis.call.ended`, `jarvis.inbound.rejected`.
- Transcripts live in the bot's chat history (SQLite) like other messages.

## 11. Testing

- Go unit tests: permission gate matrix; rule matching per trigger, expiry, once/repeat, quiet hours and override; caps
  and cool-down; opener check and fallback; inbound caller-ID acceptance; call state machine.
- Relay protocol: a fake relay in Go tests drives `internal/jarvis` end to end (outbound call, opener served on answer,
  voice turn with a tool call and filler, voicemail path, socket drop).
- Fake provider servers (`httptest`) for Twilio and ElevenLabs APIs used by provisioning and call placement.
- Relay: Vitest with Miniflare for auth checks, SSE framing and the no-daemon path.
- Frontend: Vitest for the Phone settings, Jarvis bot section (gate states, rules list), `PartCall` rendering.
- Manual end-to-end checklist with a real number: outbound rule call, bot-judgement call, voicemail, inbound from the
  verified number and from another number, live code task start, bot switch on inbound.

## 12. Documentation

`docs/architecture/jarvis.md` (new), `docs/security/threat-model.md` (relay boundary, phone auth),
`docs/architecture/chat.md` (voice turn, `PartCall`), `docs/architecture/database-schema.md`,
`docs/research/upstream-attribution.md` if any provider sample code is adapted, and `CHANGELOG.md`.
The UI and docs state that calls require the daemon to be running and the relay to be reachable.

## 13. Owner prerequisites

- Twilio account with a voice-capable number (non-US numbers may need a regulatory bundle; start early).
- ElevenLabs account and API key with Agents, Voices and Text-to-Speech access.
- Cloudflare account with Workers (already logged in via wrangler on this machine).
- Keys are entered by the owner in Settings › Phone.

## 14. Implementation notes

What was built differs from the text above in these places. The code is authoritative; `docs/architecture/jarvis.md`
describes it.

**Renamed.** Go types are `JarvisRule`, `JarvisTrigger`, `CallRecord` and `PhoneSettings` (the spec's `Rule`, `Trigger`,
`Call`, `Settings`). Statuses: `dialing | in_progress | completed | no_answer | voicemail | failed | rejected` (no
`queued`, `ringing` or `canceled`; `rejected` is an inbound call turned away). Chat adds `VoiceTurn`, `Compose`,
`PostCallCard`, `UpdateCallCard` and `SetSaveCheck` (`internal/chat/voice.go`). The migration is `0109_jarvis.sql`
(adds `trigger` and length checks and an index on `(bot_id, created_at)`; `provider_call_id`, `conversation_id` and
`message_id` default to `''` instead of NULL). The IPC methods are `jarvis.status`, `.secrets.set`,
`.relaySecrets.generate`, `.configure`, `.verify.start`, `.verify.confirm`, `.voices`, `.models`, `.rules.list`,
`.rules.delete`, `.calls.recent` and `.call.test`, with the `jarvis.changed` event. The relay's custom-LLM path is
`/llm/chat/completions` (the agent's URL is `<relay>/llm`; ElevenLabs appends the rest).

**Relay authentication.** `/llm/chat/completions` and `/inbound` are authenticated by the secret header `x-orole-relay`
(not an HMAC on `/inbound`); `/status` by the `ElevenLabs-Signature` HMAC. There is no Twilio status callback, so no
Twilio signature check and no Twilio auth token on the Worker. The Worker secrets are `RELAY_TOKEN`, `LLM_SECRET` and
`WEBHOOK_SECRET`. The relay fails closed with 503 **per path**: each path refuses while its own secret is unset
(`/daemon` needs `RELAY_TOKEN`, the `/llm` paths and `/inbound` need `LLM_SECRET`, `/status` needs `WEBHOOK_SECRET`),
so a missing webhook secret leaves calls working. It bounds a pending turn at 20 s idle with the daemon connected, 5 s
grace when the socket is gone, and 120 s in total. `/llm` and `/llm/v1/chat/completions` are aliases of
`/llm/chat/completions`. `/inbound` answers 503 (nothing spoken) when the daemon is offline, its socket drops or it does
not answer within 4 s, and 403 when the daemon refuses the caller; an accepted answer carries `custom_llm_extra_body`
`{call_id}` so inbound turns reach their call.

**Secrets.** ElevenLabs generates the post-call webhook signing secret when the webhook is created. The app stores it
(`jarvis/relay/webhook`) and shows it once after Connect, with the `wrangler secret put WEBHOOK_SECRET` step; it
generates only the relay token and the LLM secret itself. Changing the relay URL creates a new webhook and orphans the
old one: the app sends a notice naming the old id to delete.

**Workspace-wide settings.** ElevenLabs' inbound personalization webhook and post-call webhook are settings of the
owner's whole workspace, so Connect changes them for every agent in that account.

**Call ending and hang-up.** The post-call webhook is not the only way a call ends. While a call is live the daemon
polls Twilio for the call's status by its SID every 5 s (both directions), then the ElevenLabs conversation as a
fallback, and enforces a 30-minute ceiling. A call that ends without anyone answering is `no_answer`. There is no
`call.hangup` frame: the daemon hangs up through the Twilio REST API (`Status=completed`) after a verification code or
a gate refusal, on voicemail, at the ceiling and for turns of an ended call. Injected news no finished turn has seen is
posted as one notification at the end. An `urgent` retry goes through the same policy checks (caps, quiet hours) as
any call.

**Opener and voicemail.** The opener is a name-only greeting; it never states the reason (§6.1 step 4's templates
are replaced). The reason comes in the first model turn after the owner answers. The first turn always speaks the
opener: a bare greeting from the owner (only greeting or filler words, no question or request) gets only the opener,
anything else gets the opener and the answer. Until the reason has been given (the first model turn has completed),
each utterance of an outbound call is checked for answering-machine phrases (EN, NL, FR, DE), also after a greeting
counted as an answer, since a machine greeting can arrive in pieces; a match cancels any turn still speaking and ends
the call silently as `voicemail` (§6.1 step 6: ElevenLabs' `voicemail_detection` stays configured but nothing relies on it). A
call counts as answered at the first non-empty utterance that is not a voicemail prompt.

**Bot switch.** `switch_bot` swaps the bot's model, tools, policy and history; the voice stays the first bot's
(ElevenLabs sets it when the call starts) and the call card stays in the first bot's chat.

**Turns.** A new request for a call cancels the in-flight turn (barge-in); the cancelled turn speaks no more and leaves
only a one-line note in the history, "(I was interrupted. These tools already ran: ...)", when it had started tools.
Streamed speech passes a sanitizer that replaces "Orole" and "Orole OS" with "the app". Voice limits are the smaller of
the voice cap and the bot's own limit. A scripted filler is spoken when a tool starts.

**Gate timing.** The gate runs on save, before a call, on bot switch and at the start of each spoken turn (not before
every individual tool call). A failure mid-call turns Jarvis off for the bot and ends the call. On save, turning Jarvis
on is refused when the gate fails; a bot that already has Jarvis on is saved with Jarvis switched off instead
(`jarvis.disabled` audit entry and a notification). Saving the global `agents` setting switches Jarvis off the same way
for every bot that no longer passes. A policy `ask` during a call fails at once ("Needs your approval in the app — not
available on a call.").

**Missed scheduled calls.** The ticker keeps its last tick across restarts. An `at_time` rule that fell due while the
daemon was down, or more than two minutes late, becomes a notification ("<bot> missed a scheduled call").

**Records.** Per-turn timings (`first_audio_ms`, `total_ms`) go to `logs/daemon.log` as `jarvis: turn` lines, not into the
call record. The call card's cost is an estimate from the `centsPerMinute` setting (default 12).

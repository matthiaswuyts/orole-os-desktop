# Jarvis mode: phone calls with your bots

A per-bot switch that lets a bot phone the owner and lets the owner phone their bots. Voice (speech to text, turn
taking, text to speech) is ElevenLabs Agents on a Twilio number; the brain is the bot's own model and tools, reached
through a custom-LLM endpoint on a small Cloudflare relay. The daemon connects **out** to the relay, so it never
listens on a network interface. Design: `docs/superpowers/specs/2026-10-14-jarvis-mode-design.md`. Relay:
`relay/README.md`. Manual test: `docs/development/jarvis-e2e.md`. Threat model: `docs/security/threat-model.md`.

**Calls need the daemon running and the relay reachable. Terminal sessions, tasks and automations behave as usual; a
call does not survive a daemon restart.**

```
 phone <-> Twilio <-> ElevenLabs Agent --HTTPS--> Relay (CF Worker + Durable Object) <--WSS (outbound)-- orole-daemon
                       (STT, turn-taking,          /llm/chat/completions /inbound /status               internal/jarvis
                        TTS, barge-in)                                                                   internal/chat (voice turn)
```

| Unit | Location | Responsibility |
|---|---|---|
| Relay | `relay/` (TypeScript Worker + one Durable Object `Hub`) | Authenticates provider requests, turns them into frames for the daemon's one WebSocket, writes the daemon's reply as OpenAI-style SSE. Stores nothing. |
| `internal/jarvis` | daemon | Gate, rules, policy, call state machine, relay client, Twilio and ElevenLabs clients, provisioning, openers, inbound check, call records. |
| `internal/chat/voice.go` | daemon | `VoiceTurn`, `Compose`, `PostCallCard`, `UpdateCallCard`. |
| `internal/chat/tools/jarvis.go` | daemon | Bot tools `jarvis_rule_add`, `jarvis_rule_list`, `jarvis_rule_delete`, `call_owner`, `switch_bot`. |
| `internal/daemon/svc_jarvis.go` | daemon | IPC methods, event hooks, the save check, audit and notifications. |
| `internal/app/jarvis.go` | desktop | Stateless MyGo service forwarding to the daemon. |
| `frontend/src/features/jarvis/` | UI | Settings > Phone, the bot's Phone calls page, the call card. |

## The permission gate

A phone call has no approval prompt, so Jarvis only works for bots that already act without asking. `jarvis.CheckGate`
requires **all** of:

1. the bot's effective tool access is `all` (`BotConfig.ToolAccess`, else the global setting);
2. code tasks are on for the bot (`BotConfig.CodeTasks`);
3. the bot's effective code-task permission mode is `yolo`;
4. `agents.yoloDisabled` is off;
5. `agents.botYolo` ("Bots may start YOLO tasks") is on.

Unreadable agent settings fail closed. The daemon resolves the effective values (`jarvisGateInput`), the pure check
lives in `internal/jarvis/gate.go`. It runs:

- when a bot is saved with Jarvis on (`chat.Manager.SetSaveCheck`; the save is refused with the failing condition);
- before a call is placed, and for each bot an inbound call can reach, and on `switch_bot`;
- at the start of every spoken turn. If it fails there, the daemon turns Jarvis off for the bot (`jarvis.disabled`
  audit entry plus a notification), the bot says why, and the call is ended.

Explicit `deny` decisions in the shared permission policy still apply to tool calls on a call.

## Rules and triggers

The owner tells a bot in chat, "call me when the deploy task finishes"; the bot saves a rule with `jarvis_rule_add`
and confirms it. The daemon evaluates rules against events, so no model has to notice anything (`rules.go`).

| Trigger | Target | Event source |
|---|---|---|
| `task_finished`, `task_failed` | a task id, or `any` (any task this bot started) | agent session finished/failed events |
| `automation_finished`, `automation_failed` | an automation id, or `any` | automation run results (`succeeded`; `failed`/`uncertain`) |
| `notification` | notification kind, optionally `kind:source` (source matched by prefix) | the notifications store (Jarvis's own notices are ignored) |
| `at_time` | `HH:MM` local wall-clock time | the daemon's 20 s ticker |

Rule fields: `repeat` (default: deleted after it fires), `expiresAt`, `urgent` (one retry after 5 minutes if
unanswered), `quietOverride` (call during quiet hours), `note` (at most 280 characters, given to the bot on the call).
A bot keeps at most 50 rules and sees only its own. The bot may also decide by itself with `call_owner(reason, urgent)`;
its prompt carries the owner's "When to call me" policy text. Rules are listed and deleted in the bot's Phone calls
page or with `jarvis_rule_list` / `jarvis_rule_delete`.

The ticker resumes from a persisted timestamp (settings key `jarvis.state`). An `at_time` that fell due while the daemon
was down, or by more than two minutes, becomes a notification ("<bot> missed a scheduled call"), never a late call.

Before dialling, `Allow` checks: no call in progress, quiet hours (unless the rule overrides), daily cap (default 10),
monthly estimated-cost cap (default $20) and, for `call_owner` only, a 10-minute per-bot cool-down. If a call is
already live, the fired rule's reason is injected into that call as news ("also, the deploy finished") instead of
dialling. A refusal becomes a notification with the reason; for `call_owner` it is returned to the bot.

## Call lifecycle

Statuses: `dialing -> in_progress -> completed | no_answer | voicemail | failed`, plus `rejected` for an inbound call
that was turned away. Each call has a `jarvis_calls` row and one `PartCall` card in the bot's chat (created at start,
rewritten at the end). The card carries the direction (outbound or inbound), status, reason, duration and estimated
cost; the spoken turns are the ordinary user and assistant messages around it, with tool, task and research parts
attached as if typed.

### Outbound

1. The brief is built: trigger, rule note, event detail. Untrusted text (task output, rule notes) is fenced as data.
2. `Place` records the call, posts the card and calls ElevenLabs' Twilio outbound-call API with dynamic variables
   `call_id` and `bot_id` and per-call overrides for voice and TTS model.
3. While the phone rings, `Compose` asks the bot's model for the **opener**. `CheckOpener` requires the bot's name,
   forbids "Orole", caps length at 240 characters and a single paragraph. A bad or late (3 s after pickup) opener is
   replaced by a varied fallback template that never names the app.
4. On pickup ElevenLabs requests the first turn; the cached opener is streamed at once.
5. ElevenLabs' `voicemail_detection` tool ends the call on a machine without speaking task details. The owner gets a
   notification ("<bot> tried to call"). Only an `urgent` call retries (once, after 5 minutes, through the same policy
   checks).

### Inbound

ElevenLabs' personalization webhook reaches the relay's `/inbound`, which asks the daemon (`inbound.lookup`). The caller
id must equal the **verified** owner number, otherwise the call is rejected, nothing is spoken and
`jarvis.inbound.rejected` is audited. The executive bot (else the first bot passing the gate) answers with a fresh
greeting containing its name. The owner is notified of every accepted inbound call. "Put me through to <bot>" calls
`switch_bot`: voice, model, tools and history are swapped from the next sentence and the new bot introduces itself.

### A spoken turn

`ElevenLabs -> relay /llm/chat/completions -> llm.request -> Calls.turn -> chat.Manager.VoiceTurn -> model + tools ->
llm.delta -> SSE -> ElevenLabs TTS`.

- The daemon uses only the newest user utterance of the request and keeps its own per-call history (last 40 lines),
  so prompt prefixes stay identical between turns.
- `VoiceTurn` runs under its own run key (`call:<id>`), so a call never waits behind the bot's chat queue. It uses the
  voice prompt (`voiceStyle`), the cheapest reasoning level the model offers, no attachments or compaction, and limits
  of at most 6 steps and 10 tool calls (never above the bot's own limits).
- **Fillers.** When the model starts a tool, a scripted line for that tool ("Checking your automations.") is streamed
  immediately, then the model's continuation.
- **Barge-in.** A new request for the same call cancels the in-flight turn, waits for its run slot, and the cancelled turn
  speaks no more and leaves no history.
- **Sanitizer.** Streamed speech passes through a filter that replaces "Orole" and "Orole OS" with "the app" (across chunk
  boundaries), because tool text names the app.
- **Long work.** Code tasks and research are started and the bot says so; it does not wait. Completion notifications and
  matching rules apply as usual; a rule that fires during the call is injected as news.
- Model or tool errors are spoken briefly ("That failed: ...") and the call continues.

### Ending

A call ends on the first of: the post-call webhook, the 5 s poll of the ElevenLabs conversation (`done` or `failed`),
or a 30-minute ceiling (the daemon asks the relay to hang up). Ending is idempotent. It records duration and estimated
cost (`centsPerMinute`, default 12), updates the card, audits `jarvis.call.ended` and notifies on no-answer or voicemail.

## Speed design

Target: first audio at most 300 ms after pickup (cached opener), reply start at most 1.2 s after the owner stops
speaking. The opener is written during ringing and ElevenLabs' first message is empty; the daemon-to-relay socket is
persistent; everything streams; reasoning is minimal and replies are short; the voice prompt and brief are stable for
provider prompt caching; tools get instant fillers; the default TTS model is the lowest-latency one. Per-turn timings
(`first_audio_ms`, `total_ms`) are written to `logs/daemon.log` as `jarvis: turn` lines. Spoken or transcribed text is
never logged.

## Settings and secrets

Plain settings (`PhoneSettings`, settings key `jarvis`, writable only through `jarvis.configure` and verification, not
`settings.set`): relay URL (https, or loopback http), Jarvis number, verified owner number, quiet hours, daily cap,
monthly cost cap, cents per minute, default voice and TTS model, and the provider ids provisioning created (agent, phone
number, post-call webhook). Per bot (`BotConfig.Jarvis`): enabled, policy text (2000 characters), voice, TTS model.

| Credential key (`internal/credentials`) | Holds | Set by |
|---|---|---|
| `jarvis/twilio/sid`, `jarvis/twilio/token` | Twilio account SID and auth token | Settings > Phone |
| `jarvis/elevenlabs/key` | ElevenLabs API key | Settings > Phone |
| `jarvis/relay/token` | daemon-to-relay bearer token | generated by the app ("Generate relay secrets") |
| `jarvis/relay/llm-secret` | `x-orole-relay` header ElevenLabs sends to `/llm` and `/inbound` | generated by the app |
| `jarvis/relay/webhook` | HMAC secret of the post-call webhook | **generated by ElevenLabs** when the webhook is created |

Secrets never reach SQLite, logs or the frontend. The relay token and LLM secret are shown once after generating them;
the webhook secret is shown **once**, after Connect creates the ElevenLabs webhook, with the matching
`wrangler secret put WEBHOOK_SECRET` step. Until it is set on the relay, post-call webhooks are rejected and calls end
through the 5 s poll instead.

Connect (`Configure`) validates the settings and provisions ElevenLabs idempotently: the agent (custom LLM URL
`<relay>/llm`, secret header, empty first message, voicemail tool), the Twilio number imported and assigned to it, and
a post-call webhook. Verifying the owner number places a call that reads a six-digit code, entered in the app (10
minutes, 5 attempts).

Things the owner should know:

- **The ElevenLabs inbound personalization webhook and the post-call webhook are workspace-wide settings.** Connect
  sets them for the owner's whole ElevenLabs account, so they affect every agent there, not only the Jarvis agent. Use
  an ElevenLabs account or workspace that has no other agents you rely on.
- **Changing the relay URL orphans the old ElevenLabs webhook.** Nothing deletes it automatically. The app creates a
  new webhook and sends a notice naming the old webhook's id; delete it in the ElevenLabs workspace webhook settings.
- Rotating the relay token or LLM secret means redeploying the relay secrets and running Connect again so the agent
  sends the new LLM secret.

## Relay protocol

The daemon holds one `wss://<relay>/daemon` connection (`Authorization: Bearer <relay token>`); a new authenticated
connection replaces the old one. Ping every 15 s; reconnect with backoff (0.5 s to 10 s). Frames are JSON; the field
names in `internal/jarvis/relay.go` and `relay/src/hub.ts` must match exactly.

| Direction | Frame | Meaning |
|---|---|---|
| relay -> daemon | `llm.request` | A custom-LLM turn: request `id`, `messages`, `vars` (`call_id`, `bot_id`) |
| daemon -> relay | `llm.delta` / `llm.done` / `llm.error` | Streamed text for that request; `llm.error` text is spoken |
| relay -> daemon | `inbound.lookup` | Inbound webhook: `caller`, `called`, `callSid`, `conversationId` |
| daemon -> relay | `inbound.answer` | `accept`, `vars`, `overrides` (voice and model), or `accept:false` |
| relay -> daemon | `call.status` | Post-call webhook: `status`, `conversationId`, `durationSecs` |
| daemon -> relay | `call.hangup` | End the call after the current turn |

Provider authentication at the relay: the daemon socket by bearer token; `/llm/chat/completions` and `/inbound` by the
`x-orole-relay` header (constant-time compare); `/status` by the `ElevenLabs-Signature` HMAC (timestamp within 30
minutes). With any secret missing the relay answers 503 (fails closed). A pending turn is given up after 20 s without
a frame while the daemon is connected, 5 s after the socket is gone, or 120 s in total. The relay keeps nothing beyond
the in-flight request.

## Failure behaviour

| Situation | Behaviour |
|---|---|
| Daemon offline or relay socket down | No outbound calls ("The relay is not connected."). Inbound: the relay speaks one line and hangs up. Scheduled calls missed meanwhile become notifications. |
| Socket drops mid-call | The relay waits 5 s, then speaks "Lost connection, I'll message you." and ends the turn. |
| Model or tool error | Spoken briefly, recorded in the chat, the call continues. |
| Provider error placing a call | The call is `failed`; notification with the provider's error. |
| Caps, quiet hours, cool-down | No call; notification with the reason. |
| Gate fails mid-call | The bot says it cannot act, Jarvis is turned off for it, audit entry plus notification, the call ends. |
| Opener invalid or late | Fallback template. |
| Webhook never arrives | The 5 s poll or the 30-minute ceiling ends the call. |
| Injection in task output, mail, MCP results | Stays fenced as data in the brief and in tool results. |

## Audit entries

`jarvis.enable.checked`, `jarvis.disabled`, `jarvis.call.started`, `jarvis.call.ended`, `jarvis.inbound.rejected`,
`jarvis.rule.add`, `jarvis.rule.delete`, `jarvis.secrets.set`, `jarvis.relay.secrets`, `jarvis.configure`,
`jarvis.verify.started`, `jarvis.webhook.orphaned`. Values of secrets are never recorded.

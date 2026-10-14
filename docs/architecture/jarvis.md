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

- when a bot is saved with Jarvis on (`chat.Manager.SetSaveCheck`). Turning Jarvis on is refused with the failing
  condition. A bot that already has Jarvis on is saved anyway with Jarvis switched off (`jarvis.disabled` audit entry
  plus a "Jarvis turned off for <bot>" notification), so lowering a permission, granting a skill or saving the bot's
  profile still works;
- when the global `agents` setting is saved (`settings.set`): every Jarvis bot that no longer passes is switched off
  the same way;
- before a call is placed, and for each bot an inbound call can reach, and on `switch_bot`;
- at the start of every spoken turn. If it fails there, the daemon turns Jarvis off for the bot (`jarvis.disabled`
  audit entry plus a notification), the bot says why, and the call is ended.

Explicit `deny` decisions in the shared permission policy still apply to tool calls on a call. A policy `ask` cannot
be answered on a call: voice turns run with `policy.WithoutApprovals`, so the tool fails at once with "Needs your
approval in the app — not available on a call." instead of parking a five-minute approval, and the bot says so.

## Rules and triggers

The owner tells a bot in chat, "call me when the deploy task finishes"; the bot saves a rule with `jarvis_rule_add`
and confirms it. The daemon evaluates rules against events, so no model has to notice anything (`rules.go`).

| Trigger | Target | Event source |
|---|---|---|
| `task_finished`, `task_failed` | a task id, or `any` (any task this bot started) | agent session finished/failed events |
| `automation_finished`, `automation_failed` | an automation id, or `any` | automation run results (`succeeded`; `failed`/`uncertain`) |
| `notification` | notification kind, optionally `kind:source` (the source equals it or continues with `.` or `/`: `github` matches `github.ci`, not `githubber`) | the notifications store (Jarvis's own notices are ignored) |
| `at_time` | `HH:MM` local wall-clock time | the daemon's 20 s ticker |

Rule fields: `repeat` (default: deleted after it fires), `expiresAt`, `urgent` (one retry after 5 minutes if
unanswered), `quietOverride` (call during quiet hours), `note` (at most 280 characters, given to the bot on the call).
A bot keeps at most 50 rules and sees only its own. The bot may also decide by itself with `call_owner(reason, urgent)`;
its prompt carries the owner's "When to call me" policy text. Rules are listed and deleted in the bot's Phone calls
page or with `jarvis_rule_list` / `jarvis_rule_delete`.

The ticker resumes from a persisted timestamp (settings key `jarvis.state`). An `at_time` that fell due while the daemon
was down, or by more than two minutes, becomes a notification ("<bot> missed a scheduled call"), never a late call.
Right after start the relay may still be connecting: while it is configured but has not connected yet (for at most two
minutes), the ticker keeps its window open, and the relay's first connect re-runs it, so a time that fell due in that
gap is called instead of refused.

Before dialling, `Allow` checks: no call in progress, quiet hours (unless the rule overrides), daily cap (default 10),
monthly estimated-cost cap (default $20) and, for `call_owner` only, a 10-minute per-bot cool-down. If a call is
already live, the fired rule's reason is injected into that call as news ("also, the deploy finished") instead of
dialling; the bot sees it, fenced, in its next turn's brief. News that no finished turn has seen when the call ends
(the call failed, or the owner hung up first) is posted as one notification ("News you missed on the call"). A refusal
becomes a notification with the reason; for `call_owner` it is returned to the bot.

## Call lifecycle

Statuses: `dialing -> in_progress -> completed | no_answer | voicemail | failed`, plus `rejected` for an inbound call
that was turned away. Each call has a `jarvis_calls` row and one `PartCall` card in the bot's chat (created at start,
rewritten at the end). The card carries the direction (outbound or inbound), status, reason, duration and estimated
cost; the spoken turns are the ordinary user and assistant messages around it, with tool, task and research parts
attached as if typed.

### Outbound

1. The brief is built: trigger, rule note, event detail. Untrusted text (event titles, task output, rule notes, call
   reasons) is fenced as data, and titles and reasons are clipped to 200 characters.
2. `Place` records the call, posts the card and calls ElevenLabs' Twilio outbound-call API with dynamic variables
   `call_id` and `bot_id`, `custom_llm_extra_body` `{call_id}` and per-call overrides for voice and TTS model. The Twilio
   call SID it returns is kept for hang-up and polling.
3. While the phone rings, `Compose` asks the bot's model for the **opener**: a **name-only greeting** ("Hey, it's
   Atlas. Got a minute?") that does not say why it is calling, because the call may reach voicemail. The instruction
   carries no reason and no context. `CheckOpener` requires the bot's name, forbids "Orole", caps length at 240
   characters and a single paragraph. A bad or late (3 s after the first request) opener is replaced by a varied
   name-only fallback that never names the app. The reason is given in the first model turn after the owner answers
   (the brief tells the bot so).
4. The first turn **always** speaks the opener, whatever the request carries. If the owner already said something: a
   bare greeting (every word a greeting or filler such as "hello", "yes", "hallo", "speaking", "goedemorgen", apart
   from a name in "met <name>", "this is <name>" or "<name> speaking", and no question word or request verb such as
   "what", "who", "can", "check", "start", "wat", "hoe") is recorded in the call history and gets only the opener;
   anything else, however short ("ok, what's running?", "yes, start it"), is a real request and gets the opener and
   the answer in the same response.
5. **Voicemail.** Until someone answers, every utterance is checked for answering-machine phrases (English, Dutch,
   French and German: "leave a message", "after the tone", "spreek een bericht in", "na de piep", "laissez un message",
   "nach dem Signalton", "mailbox" and so on). On a match the daemon says nothing more, hangs up through Twilio and ends
   the call as `voicemail`. The owner gets a notification ("<bot> tried to call"). Only an `urgent` call retries (once,
   after 5 minutes, through the same policy checks). The call counts as answered (`in_progress`, `answeredAt`) at the
   first non-empty utterance that is not a voicemail prompt. The agent still carries ElevenLabs' `end_call` and
   `voicemail_detection` tools, but nothing relies on them, and the voice prompt says to say nothing about the reason to
   voicemail.

### Inbound

ElevenLabs' personalization webhook reaches the relay's `/inbound`, which asks the daemon (`inbound.lookup`). The caller
id must equal the **verified** owner number, otherwise the call is rejected, nothing is spoken and
`jarvis.inbound.rejected` is audited. The executive bot (else the first bot passing the gate) answers with a fresh
greeting containing its name. The answer returns `custom_llm_extra_body` `{call_id}`, so ElevenLabs sends the call id
on every inbound `/llm` request, and the Twilio call SID from the lookup is kept for polling and hang-up. The owner is
notified of every accepted inbound call. "Put me through to <bot>" calls `switch_bot`: model, tools, policy and history
are swapped from the next sentence and the new bot introduces itself. The **voice stays the first bot's** (ElevenLabs
fixes it when the call starts) and the call card stays in the first bot's chat.

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
  speaks no more. It leaves only a one-line note in the call history naming the tools it already started, so the next
  turn knows a task may already be running.
- **Approvals.** A tool whose policy decision is `ask` fails at once ("Needs your approval in the app — not available
  on a call.") instead of waiting for an approval nobody can give mid-call.
- **Unknown call.** A request for a call that is not live, or with no call id, gets an empty reply (nothing spoken);
  if the call is on record with a Twilio SID it is hung up.
- **Sanitizer.** Streamed speech passes through a filter that replaces "Orole" and "Orole OS" with "the app" (across chunk
  boundaries), because tool text names the app.
- **Long work.** Code tasks and research are started and the bot says so; it does not wait. Completion notifications and
  matching rules apply as usual; a rule that fires during the call is injected as news.
- Model or tool errors are spoken briefly ("That failed: ...") and the call continues.

### Ending

A call ends on the first of: the post-call webhook; the 5 s poll, which asks **Twilio** for the call's status by its
SID (both directions; `completed`, `busy`, `no-answer`, `canceled` and `failed` are final, and the first four count as
`no_answer` unless someone answered) and then the ElevenLabs conversation (`done` or `failed`) as a fallback; the
voicemail check; or a 30-minute ceiling. The daemon hangs up itself through the Twilio REST API
(`POST /2010-04-01/Accounts/<sid>/Calls/<CallSid>.json` with `Status=completed`): after a verification code or a gate
refusal has been spoken (it waits for an estimate of the speaking time first), on voicemail, at the ceiling, and for a
turn of a call that already ended. Ending is idempotent. It records duration and estimated cost (`centsPerMinute`,
default 12), updates the card, audits `jarvis.call.ended`, notifies on no-answer or voicemail and posts undelivered news.

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

Secrets never reach SQLite or logs. **Exception R8 (owner-approved, 2026-10-14) to "secrets never reach the
frontend":** the relay token and LLM secret are returned to Settings > Phone once, right after generating them, and the
webhook secret once, right after Connect creates the ElevenLabs webhook, so the owner can copy them into
`wrangler secret put`. They live only in that page's component state (never in a store, never persisted) and cannot be
read back later; the provider keys entered by the owner are never sent to the frontend at all. The webhook secret is
optional: until it is set on the relay, post-call webhooks are rejected and calls end through the 5 s Twilio and
ElevenLabs poll instead (the setup checklist marks it "optional").

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
| relay -> daemon | `llm.request` | A custom-LLM turn: request `id`, `callId` (from `elevenlabs_extra_body.call_id`), `messages` |
| daemon -> relay | `llm.delta` / `llm.done` / `llm.error` | Streamed text for that request; `llm.error` text is spoken |
| relay -> daemon | `inbound.lookup` | Inbound webhook: `caller`, `called`, `callSid` (no conversation id: ElevenLabs has not created one yet) |
| daemon -> relay | `inbound.answer` | `accept`, `vars`, `overrides` (voice and model), or `accept:false`; the relay adds `custom_llm_extra_body` `{call_id}` |
| relay -> daemon | `call.status` | Post-call webhook: `status`, `conversationId`, `durationSecs` |

There is no hang-up frame: the daemon hangs up through Twilio itself. The custom-LLM endpoint answers on
`/llm/chat/completions` and on the aliases `/llm` and `/llm/v1/chat/completions`.

Provider authentication at the relay: the daemon socket by bearer token; the `/llm` paths and `/inbound` by the
`x-orole-relay` header (constant-time compare); `/status` by the `ElevenLabs-Signature` HMAC (timestamp within 30
minutes). The relay fails closed **per path**: a path whose own secret is missing answers 503, while paths whose
secret is set keep working (no `WEBHOOK_SECRET` refuses only `/status`). A pending turn is given up after 20 s without
a frame while the daemon is connected, 5 s after the socket is gone, or 120 s in total. The relay keeps nothing beyond
the in-flight request.

## Failure behaviour

| Situation | Behaviour |
|---|---|
| Daemon offline or relay socket down | No outbound calls ("The relay is not connected."). Inbound: `/inbound` answers 503 and nothing is spoken (also when the daemon socket drops or does not answer within 4 s); a refused caller gets 403. A turn on `/llm` with no daemon speaks one goodbye line. Scheduled calls missed meanwhile become notifications. The bot's Phone calls page keeps the Jarvis switch and rules usable; only Test call waits for the relay. |
| Socket drops mid-call | The relay waits 5 s, then speaks "Lost connection, I'll message you." and ends the turn. |
| Model or tool error | Spoken briefly, recorded in the chat, the call continues. |
| Provider error placing a call | The call is `failed`; notification with the provider's error. |
| Caps, quiet hours, cool-down | No call; notification with the reason. |
| Gate fails mid-call | The bot says it cannot act, Jarvis is turned off for it, audit entry plus notification, the call ends. |
| Opener invalid or late | Fallback template. |
| Webhook never arrives | The 5 s Twilio (then ElevenLabs) poll or the 30-minute ceiling ends the call. |
| Voicemail answers | Nothing about the reason is said (the opener is name-only); the voicemail check hangs up; notification, urgent retry. |
| Approval needed mid-call | The tool fails at once with "Needs your approval in the app"; the bot says so. |
| Injection in event titles, task output, mail, MCP results | Stays fenced as data in the brief, injected news and tool results; the opener prompt carries none of it. |

## Audit entries

`jarvis.enable.checked`, `jarvis.disabled`, `jarvis.call.started`, `jarvis.call.ended`, `jarvis.inbound.rejected`,
`jarvis.rule.add`, `jarvis.rule.delete`, `jarvis.secrets.set`, `jarvis.relay.secrets`, `jarvis.configure`,
`jarvis.verify.started`, `jarvis.webhook.orphaned`. Values of secrets are never recorded.

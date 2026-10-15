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
| `internal/chat/voice.go` | daemon | `VoiceTurn` (spoken and cue turns), `Compose`, `PostCallCard`, `UpdateCallCard` (and `Manager.CallMessages`). The chat queue and delegation keep follow-ups of a call in the call's thread (`chat.md`). |
| `internal/chat/tools/jarvis.go` | daemon | Bot tools `jarvis_rule_add`, `jarvis_rule_list`, `jarvis_rule_delete`, `call_owner`, `switch_bot`, `end_call` (see Bot tools). |
| `internal/daemon/svc_jarvis.go` | daemon | IPC methods, event hooks, the save check, audit and notifications. |
| `internal/app/jarvis.go` | desktop | Stateless MyGo service forwarding to the daemon. |
| `frontend/src/features/jarvis/` | UI | Settings > Phone, the bot's Phone calls page, the call card and the call pane. |

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
| `bot_reported` | the name of a bot the rule's bot handed work to (case-insensitive), or `any` | a bot the rule's bot handed work to reported back (`Service.OnFollowUp`, see Follow-ups of a call). The event's source is the callee's name and the rule matches only for the bot that handed the work out. Reports only: code tasks reach rules through `task_finished` / `task_failed`. |

Rule fields: `repeat` (default: deleted after it fires), `expiresAt`, `urgent` (one retry after 5 minutes if
unanswered), `quietOverride` (call during quiet hours), `note` (at most 280 characters, given to the bot on the call).
A bot keeps at most 50 rules and sees only its own. The bot may also decide by itself with `call_owner(reason, urgent)`;
its prompt carries the owner's "When to call me" policy text. Rules are listed and deleted in the bot's Phone calls
page or with `jarvis_rule_list` / `jarvis_rule_delete`. The voice prompt tells a bot that the owner's "call me back when
it is done" is a rule: `bot_reported` for work it handed to another bot, `task_finished` for a code task.

Bot tools (`internal/chat/tools/jarvis.go`), offered only to bots with Jarvis on:

| Tool | What it does |
|---|---|
| `jarvis_rule_add`, `jarvis_rule_list`, `jarvis_rule_delete` | Save, list and delete this bot's call rules. The trigger is one of the six above (`bot_reported` takes a bot name or `any` as its target). |
| `call_owner(reason, urgent)` | Phone the owner now because something matches their "When to call me" policy. Refused on a call, in quiet hours, over a cap or within the cool-down; the refusal is returned to the bot. |
| `switch_bot(name)` | On a call only: put the owner through to another bot. |
| `end_call` | On a call only: hang up once this reply has been spoken. The daemon refuses it ("The owner has not said they are done. Ask whether there is anything else instead.") unless the owner's words in the turn being answered are a closing, see Ending a call. |

The ticker resumes from a persisted timestamp (settings key `jarvis.state`). An `at_time` that fell due while the daemon
was down, or by more than two minutes, becomes a notification ("<bot> missed a scheduled call"), never a late call.
Right after start the relay may still be connecting: while it is configured but has not connected yet (for at most two
minutes), the ticker keeps its window open, and the relay's first connect re-runs it, so a time that fell due in that
gap is called instead of refused.

Before dialling, `Allow` checks: no call in progress, quiet hours (unless the rule overrides), daily cap (default 10),
monthly estimated-cost cap (default $20) and, for `call_owner` only, a 10-minute per-bot cool-down. If a call is
already live, the fired rule's reason is injected into that call as news ("also, the deploy finished") instead of
dialling; the bot sees it, fenced, in its next turn's brief. News that no turn has had and finished without an error
when the call ends (the call failed, or the owner hung up first) is posted as one notification ("News you missed on the
call"); follow-ups folded into the call are handled on their own (see Follow-ups of a call). A refusal becomes a
notification with the reason; for `call_owner` it is returned to the bot.

## Call lifecycle

Statuses: `dialing -> in_progress -> completed | no_answer | voicemail | failed`, plus `rejected` for an inbound call
that was turned away. Each call has a `jarvis_calls` row and one `PartCall` card in the bot's chat (created at start,
rewritten at the end). The card carries the direction (outbound or inbound), status, reason, duration and estimated
cost. The spoken turns are user and assistant messages in the bot's thread, with tool, task and research parts attached
as if typed, but tagged with the call id (`messages.call_id`): the bot keeps them in its history and memory, while the
chat UI never shows them (see `chat.md`), nor the later reports of work started on the call and the bot's written
answers to them (see Follow-ups of a call). The card is the call's only entry in the thread; it is drawn like the bot's
text bubble and clicking it (or Enter/Space on it) opens the **call pane** beside the conversation
(`frontend/src/features/jarvis/CallPane.tsx`): a header with the direction (Called you, You called, Missed call), the
bot, the start time, duration, estimated cost and status, then the transcript in order, the owner's lines and the
bot's, with tool, delegation and code-task activity as compact rows (a task row opens the task pane) and the follow-ups
as collapsed items. It loads `jarvis.calls.transcript {callId}` -> `{call: CallRecord, messages: Message[]}`
(owner-only; no provider ids or secrets) and, while the call is live, follows the `chat:call-message` and `chat:call-delta` events, refreshing the
header when the call's card changes. After "put me through" (`switch_bot`) the turns are stored in the new bot's thread
with the same call id: the header keeps the bot that started the call, each bot line is labelled with its own bot and a
"Handed to <bot>" line marks the change. Only the live state ("Call in progress", "<bot> is answering") is announced to
screen readers, not the growing transcript. Outside the desktop app the pane says transcripts need it. A call card is
not a reply: the thread offers no Regenerate, Answer or Copy on it, and `chat.regenerate` refuses a message with a call
part, or any regenerate while the bot is answering on a call. Calls made before this change keep their turns in the thread.

### Outbound

1. The brief is built: trigger, rule note, event detail. Untrusted text (event titles, task output, rule notes, call
   reasons) is fenced as data, and titles and reasons are clipped to 200 characters.
2. `Place` records the call, posts the card and calls ElevenLabs' Twilio outbound-call API with dynamic variables
   `call_id` and `bot_id`, `custom_llm_extra_body` `{call_id, lang}` (`lang` is `nl` for Dutch and Auto, `en` for English;
   the relay needs it for its own fixed lines) and per-call overrides for voice and TTS model. The Twilio call SID it
   returns is kept for hang-up and polling.
3. While the phone rings, `Compose` asks the bot's model for the **opener**: a **name-only greeting** ("Hey, it's
   Atlas. Got a minute?") that does not say why it is calling, because the call may reach voicemail. The instruction
   carries no reason and no context, only the call language. `CheckOpener` requires the bot's name, forbids "Orole", caps length at 240
   characters and a single paragraph. A bad or late (3 s after the first request) opener is replaced by a varied
   name-only fallback that never names the app. The reason is given in the first model turn after the owner answers
   (the brief tells the bot so).
4. The first turn **always** speaks the opener, whatever the request carries. If the owner already said something: a
   bare greeting (every word a greeting or filler such as "hello", "yes", "hallo", "speaking", "goedemorgen", apart
   from a name in "met <name>", "this is <name>" or "<name> speaking", and no question word or request verb such as
   "what", "who", "can", "check", "start", "wat", "hoe") is recorded in the call history and gets only the opener;
   anything else, however short ("ok, what's running?", "yes, start it"), is a real request and gets the opener and
   the answer in the same response. On an **outbound** call the first reply gets that answer only if it contains a
   question word or request verb: anything else ("Hey, it's Ray.", "Hello, Ray here", "Ja?") may be the start of a
   machine greeting, so it gets only the opener and the bot waits for the next utterance before giving the reason.
   Inbound calls answer any real request at once.
5. **Voicemail.** On an outbound call, every utterance is checked for answering-machine phrases (English, Dutch, French
   and German: "leave a message", "after the tone", "you've reached", "not available right now", "spreek een bericht
   in", "na de piep", "u bent verbonden met", "laissez un message", "vous êtes bien sur", "nach dem Signalton", "ist
   nicht erreichbar", "Sie haben ... erreicht", "mailbox" and so on) until the reason has been given, that is until the
   first model turn has completed. A machine greeting often arrives in pieces ("Hi, this is Ray." ... "Please leave a
   message after the tone."), and the first piece passes as someone answering, so the check does not stop at the
   answer. On a match the daemon cancels a turn that is still speaking, says nothing more, hangs up through Twilio and
   ends the call as `voicemail`, even if it had already counted as answered. After the reason has been given, the word
   "voicemail" is just conversation (the owner may ask about their own voicemail) and never ends the call. The owner
   gets a notification ("<bot> tried to call"). Only an `urgent` call retries (once, after 5 minutes, through the same
   policy checks). The call counts as answered (`in_progress`, `answeredAt`) at the first non-empty utterance that is
   not a voicemail prompt. The agent also carries ElevenLabs' `voicemail_detection` tool, but nothing relies on it (the
   daemon hangs up through Twilio itself), and the voice prompt says to say nothing about the reason to voicemail. The
   other two system tools are used: `skip_turn` (see Turn settings of the voice agent) and `end_call`, with which the
   daemon ends a call right after a goodbye (see Ending a call).

### Inbound

ElevenLabs' personalization webhook reaches the relay's `/inbound`, which asks the daemon (`inbound.lookup`). The caller
id must equal the **verified** owner number, otherwise the call is rejected, nothing is spoken and
`jarvis.inbound.rejected` is audited. The executive bot (else the first bot passing the gate) answers with a fresh
greeting containing its name. The answer returns `custom_llm_extra_body` `{call_id, lang}`, so ElevenLabs sends the call
id and the call language on every inbound `/llm` request, and the Twilio call SID from the lookup is kept for polling and
hang-up. ElevenLabs asks for the greeting 1 s after the call connects (the agent's `initial_wait_time`), not after its
default 7 s. The owner is notified of every accepted inbound call. "Put me through to <bot>" calls `switch_bot`: model, tools, policy and history
are swapped from the next sentence and the new bot introduces itself. The **voice stays the first bot's** (ElevenLabs
fixes it when the call starts) and the call card stays in the first bot's chat.

### Turn settings of the voice agent

The first test calls (2026-10-14) were ended by ElevenLabs itself: the conversation had `status: failed`,
`termination_reason: "custom_llm generation failed"` and `metadata.error` `llm_error` ("custom_llm_error: LLM Cascade
Error: TimeoutError"). Each time our stream had gone quiet, either no first token for about 4 s or one short sentence
and then about 5 s of nothing while a tool ran. ElevenLabs gives a custom LLM `cascade_timeout_seconds` to start
answering (default **4**), retries on timeouts, errors and **empty responses** (the duplicate requests seen about 4 s
apart) and fails the call after a few attempts. The agent was also set to `eager` turn-taking, which ended the owner's
turn on a hesitation ("geef me een, uhm-"), and with an empty `first_message` ElevenLabs waits `initial_wait_time`,
which falls back to `turn_timeout` (default 7 s): an inbound caller would hear 7 s of silence before the opener.
`agentBody` (`internal/jarvis/elevenlabs.go`) therefore sets:

| Field | Value | Why |
|---|---|---|
| `turn.turn_eagerness` | `patient` | Do not end the owner's turn on a pause. |
| `turn.turn_timeout` | 10 s | After 10 s of owner silence ElevenLabs sends the user message "..." (the silence prompt). |
| `turn.initial_wait_time` | 1 s | The opener comes 1 s after pickup instead of after the default 7 s. |
| `turn.speculative_turn` | `false` | No generation before the owner's turn has ended. |
| `turn.silence_end_call_timeout` | -1 | ElevenLabs never ends a call for silence; the daemon decides (see Silences). |
| `turn.soft_timeout_config` | `{timeout_seconds: -1}` | No filler line from ElevenLabs while the LLM thinks; the daemon keeps the request open with buffer chunks and speaks its own lines only when they help (see Tool lines and Keep-alive). |
| `agent.prompt.cascade_timeout_seconds` | 15 (the maximum) | How long the custom LLM may take for its first chunk. |
| `conversation.max_duration_seconds` | 1800 | Matches the daemon's 30-minute ceiling. |
| `built_in_tools.skip_turn` | system tool | ElevenLabs passes system tools to a custom LLM in the OpenAI `tools` parameter; the daemon answers a turn with a `skip_turn` call when it has nothing to say, so the agent stays silent and keeps listening. |

The fields follow the ElevenLabs agent API reference; `docs/development/jarvis-e2e.md` step 0 lists what to confirm on a
live call. `agentRevision` (2 for these settings) numbers the shape of `agentBody`; provisioning records it in
`PhoneSettings.AgentRevision`. When it is behind (and when the language differs), the daemon updates the agent once in
the background on its next start (see Settings and secrets), so an existing agent picks up a release's changes. The relay
must be redeployed for `skip_turn` and the Dutch relay lines (see Relay protocol). Until the agent is updated, the
status in Settings > Phone shows "The phone agent still has an older configuration: press Connect to update it." after
any provisioning error (both in `StatusView.lastError`).

### A spoken turn

`ElevenLabs -> relay /llm/chat/completions -> llm.request -> Calls.turn -> chat.Manager.VoiceTurn -> model + tools ->
llm.delta -> SSE -> ElevenLabs TTS`.

- The daemon uses only the newest user utterance of the request and keeps its own per-call history (last 40 lines),
  so prompt prefixes stay identical between turns.
- `VoiceTurn` runs under its own run key (`call:<id>`), so a call never waits behind the bot's chat queue. It uses the
  voice prompt (`voiceStyle`, the bot's own name and the call language), the cheapest reasoning level the model offers,
  no attachments or compaction, and limits of at most 6 steps and 10 tool calls (never above the bot's own limits). The
  brief and a list of the tasks the bot handed to other bots that have not reported back go in as the run's addendum, so
  it can answer "how is that going?".

`Calls.turn` gives every request after the voicemail check one **kind** and logs it as `jarvis: request` with the call
id, the kind and the message count (never any text):

| Kind | The request | What happens |
|---|---|---|
| `opener` | The first of the call | The opener is spoken (see Outbound). |
| `new` | An utterance, nothing running | A model turn answers it. |
| `bargein` | An utterance while a model turn runs | That turn is cancelled; this one is answered, with the owner's words the cancelled turn had not answered yet in front (see Retries and barge-in). |
| `retry` | ElevenLabs re-sent the running turn's request (same utterance and message count) | The running turn carries on in the new request. |
| `silence_adopted` | The silence prompt while a turn is answering or waiting on a hesitation | That turn carries on in the new request. |
| `silence` | The silence prompt with nothing to say | `skip_turn`, "Are you still there?" or the goodbye; only `skip_turn` while a goodbye is being spoken. |
| `no_user` | A request whose last message is not the owner's (ElevenLabs asking again after a tool call) | A running turn carries on in it; otherwise `skip_turn`. It never counts as a silence. |
| `hesitation` | An utterance that trails off | The turn waits about 2.5 s, then answers; a continuation in that time is answered together with it (see Hesitations). |
| `news` | The silence prompt while news waits | A cue turn tells the news. |
| `verify` | A later request on a verification call | Ended at once. |

- **Tool lines.** The moment the model starts a tool that leaves the PC or hands work on, the bot says what it is doing
  (`internal/jarvis/lang.go`, `toolKinds`), then the model carries on:

  | Tool | English | Dutch |
  |---|---|---|
  | MCP tool `mcp__<server>__<tool>` | "I'm using your <server> tools now. Give me a second to get the results." | "Ik gebruik nu je <server> koppeling. Even geduld, dan heb ik de resultaten." |
  | MCP tool whose server part is no name | "I'm using one of your integrations now. Give me a second to get the results." | "Ik gebruik nu een van je koppelingen. Even geduld, dan heb ik de resultaten." |
  | `web_search` | "I'm searching the web for that now." | "Ik zoek het even op internet op." |
  | `web_fetch`, `fetch_url` | "I'm opening that page now." | "Ik open die pagina even." |
  | `research` | "I'm starting the research, that can take a moment." | "Ik start het onderzoek, dat kan even duren." |
  | `start_task` | "I'm starting that coding task now." | "Ik start die codetaak nu." |
  | `message_task` | "I'm passing that on to the task." | "Ik geef het door aan de taak." |
  | `stop_task` | "I'm stopping that task." | "Ik stop die taak." |
  | `ask_bot` | "I'm passing it on." | "Ik geef het door." |
  | `switch_bot` | "I'll put you through." | "Ik verbind je door." |
  | `list_automations` | "I'm checking your automations." | "Ik kijk even naar je automatiseringen." |
  | `automation_detail` | "I'm looking at that automation and its runs." | "Ik bekijk die automatisering en de runs." |

  `<server>` is the server part of the tool name with underscores and dashes read as spaces and an `orole_` prefix
  dropped (`canvas_lms` is "canvas lms", `orole_mail` is "mail"). The app names its own MCP servers by a UUID in the
  tool name, and a UUID, a part without letters, one longer than 40 characters, or one that names the app is not said:
  the line without a name is used instead. Fast tools that only read or write local state say nothing: `memory`,
  `memory_search`, `search_history`, `knowledge_search`, `knowledge_write`, `list_tasks`, `task_status`, the
  `jarvis_rule_*` tools, `call_owner`, `end_call`, `datetime`, `orole_get_skill`, the automation drafting tools, the
  `show_*` tools, the setup tools, and any tool not in the table. A tool says its line at most once per turn, and the
  same line is never said twice in a row (two tools of one integration). The provider's hosted web search and page
  fetch run at the provider and never reach the daemon, so they have no line; the long-wait line covers them.
- **Keep-alive.** ElevenLabs fails the call when the custom LLM goes quiet, so a model turn first streams a buffer chunk,
  `"... "` (the documented way to buy time; it is not spoken as words and does not count as the bot speaking), before
  the gate and the model run, and again whenever nothing at all went out for 3 s. A quick answer therefore needs no
  acknowledgement and gets none. A turn that has said nothing after 4 s, not even a tool line, says once "Bear with me,
  I'm still working on it." (Dutch "Even geduld, ik ben er nog mee bezig."). With 12 s without words (a long tool run)
  it says a "still working" line, at most 3 times per turn. A turn that has said nothing for a while after about a
  minute (never mid-answer; hard cap 100 s) is cancelled: the bot says "This is taking too long on the phone. Shall I
  run it as a background task instead?" (Dutch "Dit duurt te lang voor aan de telefoon. Zal ik het als taak op de
  achtergrond laten doen?") and the history notes what was asked and which tools already ran, so the next turn can pick
  it up. Work that was already handed off (a started code task, a handoff) carries on; the cancelled model turn does
  not.
- **Silences.** After `turn_timeout` (10 s) of owner silence ElevenLabs sends a user message "..." and waits for the
  agent to decide. A running turn carries on in the new request (`silence_adopted`). While a goodbye is being spoken
  before the call ends, the prompt is answered with `skip_turn` and not counted. Otherwise, if news is waiting, a cue
  turn tells it (`news`: the instruction "(Nobody spoke. News came in while you were talking (listed under New): tell
  the owner briefly, then ask if there is anything else.)" is stored with the author `cue`, so it is in the bot's
  history but not in the call transcript). Otherwise the daemon counts silence prompts in a row: the 1st and 2nd are
  answered with `skip_turn`, the 3rd says "Are you still there?" (Dutch "Ben je er nog?"), the 4th to 8th skip again,
  and the 9th says "I can't hear you anymore, so I'll hang up. Call me back anytime." (Dutch "Ik hoor je niet meer, dus
  ik hang op. Bel me gerust terug.") and ends the call the way a goodbye does (`end_call` with the reason "no answer",
  see Ending a call): after about 30 s and 90 s of silence. Any utterance resets the count. Only the silence prompt
  itself counts: a request that does not end with an owner message at all (`no_user`, for example ElevenLabs asking
  again after a tool call) is answered with `skip_turn` and never counted. `skip_turn` is a function call returned in
  the stream (relay frame `llm.tool`), not an empty response, which ElevenLabs would retry.
- **Hesitations.** The owner's turn can still end mid-sentence ("geef me een, uhm"). An utterance trails off when it ends
  with a dash or "…", or when its last word is a hesitation (uh, uhm, um, uhh, hmm, eh, ehm, euh, eeh) or a connector that
  cannot end a sentence (Dutch: en, of, want, omdat, zoals, een, de; English: and, because, the, a), unless it ends with
  a question mark. The list is short on purpose: "het", "maar" or "so" can end a sentence ("laat maar", "I think so"),
  and the owner would wait for an answer that is held back. The first turn and bare greetings are never held. Such a
  request (`hesitation`) is not answered at once and does not use `skip_turn`: the turn waits about 2.5 s, streaming
  buffer chunks so ElevenLabs keeps the request open, and then answers the held words. If the owner carries on within
  that time, ElevenLabs sends a new request that supersedes the waiting turn, and the held words and the new ones are
  answered together as one utterance (a continuation that repeats the held words is not doubled). The held words stay
  with the turn after the wait too, until the model says its first words, so the rest of a sentence that comes after a
  longer pause is still answered together with them (see Retries and barge-in). A held turn that waits for the turn it
  superseded to let go of its run slot streams buffer chunks meanwhile as well.
- **Retries and barge-in.** A request that repeats the in-flight turn's utterance with the same number of messages is
  ElevenLabs retrying, not a barge-in: the running turn adopts it (the rest of the reply goes to the new request, which
  gets a buffer chunk at once; the old one is closed) instead of restarting. A new utterance cancels the in-flight turn,
  waits for its run slot, and the cancelled turn speaks no more. Until the model has said its first words (scripted
  acknowledgements and tool fillers do not count), the owner's words the cancelled turn was answering go in front of the
  new utterance (unless it repeats them), so a sentence split by a pause is answered as one. A cancelled turn that had
  already spoken leaves the owner's words and what it said, marked "(I was interrupted here.)", in the call history; one
  that had only started tools leaves the owner's words and a note naming those tools, so the next turn knows a task may
  already be running; one that did neither leaves nothing, since its words live on in the newer turn.
- **No empty answers.** A turn that ends without having said a word (the voicemail path, for example) answers with
  `skip_turn` instead of an empty response, because ElevenLabs retries an empty custom-LLM response and, after a few
  attempts, fails the call. A later request on a verification call and a request for an unknown call end with `llm.done`
  only.
- **Ending a call.** The voice style asks the bot to check whether there is anything else once it is done, and to end
  the call only when the owner clearly says they are done: a short goodbye and `end_call` in the same reply. The daemon
  enforces it: `end_call` is refused ("The owner has not said they are done. Ask whether there is anything else
  instead.") unless the owner's words that the running turn answers contain a closing, matched as whole words or phrases:
  Dutch `nee`, `nee hoor`, `dat was het`, `dat is het`, `dat is alles`, `dat was alles`, `bedankt`, `dankje`,
  `dank je`, `oké bedankt`, `merci`, `dag`, `doei`, `tot later`, `tot straks`, `tot ziens`, `slaapwel`, `salut`, `ciao`,
  `niks`, `laat maar`, `laat het maar`, `goed zo`, `ophangen`, `hang op`, `ik ben klaar`, `klaar`; English `no`,
  `that's all`, `that is all`, `that's it`, `that is it`, `thanks`, `thank you`, `bye`, `goodbye`, `talk later`,
  `see you`, `good night`, `nothing`, `leave it`, `all good`, `i'm done`, `we're done`, `hang up`. Words that end with a
  question mark are never a closing, and neither are words that also say the owner goes on: once the closings and the
  courtesies `nog een fijne dag`, `nog een fijne avond`, `nog een prettige dag`, `fijne dag nog`, `fijne avond nog`,
  `jij ook`, `u ook` and `you too` are taken out, what is left must not contain a continuation word (Dutch `wacht`,
  `maar`, `nog`, `ook`, `eerst`, `eigenlijk`; English `wait`, `but`, `also`, `first`, `actually`, `one more`). So
  "nee, bedankt" and "nee hoor, dat was alles" end the call, "nee, wacht" and "dat was het, maar ..." do not. The bot
  cannot hang up on a question, a cue turn or by itself.

  When it is allowed, the reply (the bot's own goodbye, or "Okay, talk later!" if it said none) ends in ElevenLabs'
  `end_call` system tool with the arguments `{"reason":"owner said goodbye"}`, streamed after the words (relay frame
  `llm.tool`, then `llm.done`). ElevenLabs runs it as soon as the voice has said the goodbye, so the line goes dead
  right after it. The silence goodbye (reason "no answer") and the gate refusal ("not allowed") end the same way. The
  daemon logs `jarvis: ending call` with the reason, whether `end_call` was used and the fallback wait. The Twilio
  hang-up stays as a fallback: the speaking-time estimate (2 s plus 0.4 s a word) plus 4 s after the stream ended, the
  daemon checks that the call has not ended on record and that Twilio does not report it over, and hangs it up if it is
  still live (`jarvis: hanging up at the carrier`). Anything the owner says before then ("oh wacht, nog één ding") calls
  the fallback off (`jarvis: hang-up called off` in the log) and is answered as usual, if ElevenLabs kept the call open;
  the gate refusal's fallback cannot be called off. With a relay known to be out of date (see Relay protocol) `end_call`
  cannot reach ElevenLabs: the turn ends plainly and the daemon hangs up through Twilio after a tighter estimate, 0.8 s
  plus 0.33 s a word from the end of the stream.
- **Approvals.** A tool whose policy decision is `ask` fails at once ("Needs your approval in the app — not available
  on a call.") instead of waiting for an approval nobody can give mid-call.
- **Unknown call.** A request for a call that is not live, or with no call id, gets an empty reply (nothing spoken);
  if the call is on record with a Twilio SID it is hung up.
- **Sanitizer.** Streamed speech passes through a filter that replaces "Orole" and "Orole OS" with "the app" (across chunk
  boundaries), because tool text names the app.
- **Long work and news.** Code tasks and research are started and the bot says so; it does not wait. A rule that fires
  during the call, a report from a bot the call's bot handed work to, and a code task started on the call reaching a
  result are folded into the call as news: the next turn's brief carries them, fenced, under "New (not told yet)", and
  news a turn has already had (and finished without an error) under "Already told (do not repeat it unless the owner
  asks)"; on the next silence prompt a cue turn tells what is new unprompted. News no such turn has had when the call
  ends is posted as a notification, except follow-ups, which are handled on their own (see Follow-ups of a call).
  Completion notifications and matching rules apply as usual.
- Model or tool errors are spoken briefly ("That failed: ...") and the call continues.

### Ending

A call ends on the first of: the post-call webhook; the 5 s poll, which asks **Twilio** for the call's status by its
SID (both directions; `completed`, `busy`, `no-answer`, `canceled` and `failed` are final, and the first four count as
`no_answer` unless someone answered) and then the ElevenLabs conversation (`done` or `failed`) as a fallback; the
voicemail check; an answered call that has had no LLM request for 5 minutes (ElevenLabs asks at least every 10 s while
the call is open, so such a call is no longer heard: the daemon logs `jarvis: no requests, hanging up`, hangs up and ends
it as `completed`); or a 30-minute ceiling. A goodbye (`end_call`, the silence goodbye) and the gate refusal end the
call through ElevenLabs' `end_call` tool (see Ending a call). The daemon hangs up itself through the Twilio REST API
(`POST /2010-04-01/Accounts/<sid>/Calls/<CallSid>.json` with `Status=completed`): after a verification code has been
spoken (it waits for an estimate of the speaking time first), as the fallback when a call is still live after a goodbye
(called off when the owner speaks first) or a gate refusal, or after a tighter estimate of the speaking time when the
relay is out of date, on voicemail, when no requests come, at the ceiling, and for a turn of a call that already
ended. Ending is idempotent. It records duration and estimated cost (`centsPerMinute`, default 12), updates the card,
audits `jarvis.call.ended`, notifies on no-answer or voicemail, posts undelivered news and settles the follow-ups the
call never told (see Follow-ups of a call).

**Call outcome.** After every call that reached the provider, the daemon asks ElevenLabs how it recorded the
conversation (`GET /v1/convai/conversations/{id}`), 10 s after the call ended; a conversation that is not finished yet is
read once more 30 s later. The conversation id comes from the call record; for an inbound call that has none, the daemon
lists the agent's latest conversations and takes the one whose `metadata.phone_call.call_sid` equals the call's Twilio
SID (started within 120 s of the call). It logs `jarvis: call outcome` with the call id, `status`, `termination_reason`,
`error_type` and `error_reason`: provider metadata, never transcript text. When the status is `failed` the owner gets one
notification, "Call with <bot> dropped" ("The voice service ended the call: <reason>", level warning, opens the bot), so
a call ElevenLabs gave up on is no longer silent. It is skipped for a call our own record already ended as unanswered or
voicemail (the owner was told) and for verification calls. The check is best effort: a failure is logged and nothing else.

### Follow-ups of a call

Nothing that comes out of a call appears in the bot's main chat thread. The owner's words, the bot's replies, their
tool and delegation items, and everything that arrives later live in the call's thread (messages with `call_id`) and
are read in the call pane. The call card is the call's only entry in the chat. That includes:

- the report of a bot the call's bot handed work to (`ask_bot` on the call): a user message authored `report:<botId>`,
  then the bot's written answer to it;
- the report of a code task started on the call (`task:<taskId>`) and the answer to it. A task remembers the call it was
  started on (`agents.Session.CallID`), and `message_task` from another call, or from chat, moves where it reports next;
- anything those answers start (they run with the thread's call id as `RunContext.CallThread`, which is not a live call:
  `end_call` and `switch_bot` refuse, `call_owner` works).

The callee bot's own thread (the task handed to it) is that bot's work and stays in its chat. Chat delivers every report
to `Service.OnFollowUp` (the chat manager's follow-up hook) before it queues the written answer; the follow-up names the
id its message will be stored under (`FollowUp.MessageID`):

1. **The call is live** and belongs to the bot (or is the call the follow-up came from): the follow-up is told on the
   call as news. The reason is "<status>: <text>", clipped to 500 runes and fenced when it reaches the model; the label
   is the callee's name, or "A code task" for tasks, whose titles (written by a bot) only appear inside the fence. When
   the follow-up came from this very call and the call is not hanging up (no accepted `end_call`, no goodbye being
   spoken), the call takes it over: the bot says it on the phone and **no written answer** is queued; the report is only
   stored in the call thread. A follow-up that came from chat during a live call, or one that arrives while the call
   hangs up, is also told on the phone (if the call goes on) but is still answered in writing, in chat or in the call
   thread. What happens next:
   - **Told.** Once a model turn that had it in its brief finishes without an error, a report uses up the once
     `bot_reported` rules it matches, without placing a call; repeating rules stay. So "bel me terug als je meer weet"
     does not ring the owner days later for a report that was already told.
   - **Not told when the call ends.** A report reaches the `bot_reported` rules after all, so a "call me back" rule
     phones the owner. A follow-up the call had taken over gets its written answer in the call thread after all
     (`chat.Manager.AnswerFollowUp`, with the usual report or task instructions; a report still on its way into the store
     is answered as soon as it is stored), and the owner gets the notification of item 3. Follow-ups never become "News
     you missed on the call".
2. **The call is not live** and the follow-up is a bot's report: it is an event for `bot_reported` rules. A matching rule
   (callee name or `any`, for the bot that handed the work out) places a call with the report in the brief, through the
   usual checks (quiet hours, daily and monthly caps, no call in progress). Code task follow-ups do not fire rules here:
   they already reach `task_finished` and `task_failed` rules through task events.
3. **The follow-up carries a call id and its call did not take it over** (the call ended or was hanging up), or the call
   took it over but ended without telling it: the owner gets a notification titled "<name> reported back" (or
   "<name> failed", "<name> stopped", "<name> is waiting for you" for a task), "From your call with <bot>. Open the call
   to read it.", which opens the bot. The written answer lands in the call thread, which raises no chat notification of
   its own.
The call pane draws these as they arrive, also after hang-up (the header stays Ended): a report as the chat's collapsed
"<bot> reported back" card, a task as a collapsed "Code task update" with an "Open the task" button, any other author as
a collapsed item with a friendly label, and cue messages not at all.

## Speed design

Target: first audio at most 300 ms after pickup (cached opener), reply start at most 1.2 s after the owner stops
speaking. The opener is written during ringing and ElevenLabs' first message is empty; the daemon-to-relay socket is
persistent; everything streams; reasoning is minimal and replies are short; the voice prompt and brief are stable for
provider prompt caching; a slow tool says what it is doing the moment it starts, while a quick answer comes with no
acknowledgement at all; the default TTS model is the lowest-latency one. Per-turn timings (`first_audio_ms`,
`total_ms`) are written to `logs/daemon.log` as `jarvis: turn` lines, and each request's kind as `jarvis: request`
lines. Spoken or transcribed text is never logged.

## Settings and secrets

Plain settings (`PhoneSettings`, settings key `jarvis`, writable only through `jarvis.configure` and verification, not
`settings.set`): relay URL (https, or loopback http), Jarvis number, verified owner number, quiet hours, daily cap,
monthly cost cap, cents per minute, default voice and TTS model, call language, and the provider ids provisioning
created (agent, phone number, post-call webhook).

**Call language** (`callLanguage`: `nl`, the default when empty; `en`; `auto`), Settings > Phone > Calls. It is fixed
when a call starts and sets:

- the ElevenLabs agent `language` (`nl` or `en`; speech recognition and voice). `auto` keeps `nl` and adds the
  `language_detection` system tool (`built_in_tools.language_detection`, `params.system_tool_type`
  `language_detection`) with an `en` entry in `conversation_config.language_presets`, so the agent can switch when the
  owner speaks English. Other languages need another preset. Without `auto` the tool is sent as `null` and the presets
  empty, so switching away from Auto removes them. A language other than English with no voice model chosen sends
  `model_id` `eleven_flash_v2_5` (on the agent and per call), because ElevenLabs' default may be an English-only model
  that it refuses for a non-English agent. Provisioning records the language it gave the agent in `agentLanguage` (not
  user-editable; an agent from before the setting counts as English) and the shape of the agent in `agentRevision`
  (see Turn settings of the voice agent). Calls use that **provisioned** language for the bot's words and the fixed
  lines, so speech recognition, voice and words always match. Saving the setting re-provisions (Connect always updates
  the agent); on daemon start, and after a save that did not provision, the daemon updates only the agent in the
  background (`syncAgent`: no number import, no webhook) when its language differs or its revision is behind, and
  notifies "Couldn't update the phone agent" with the error if that fails. Missing ElevenLabs key, relay URL or relay
  secret skip that update quietly;
- the bot's spoken language: `chat.VoiceRequest.Language` adds one line to the voice prompt (`nl`: speak Dutch; `en`:
  speak English; `auto`: answer in the owner's language, Dutch until known);
- the opener and inbound greeting instructions ("Write it in Dutch"), and the daemon's fixed lines in
  `internal/jarvis/lang.go`: the name-only fallback openers, the inbound fallback greeting, the verification-code line,
  the tool lines, the long-wait and "still working" lines, "I can't do that right now", the gate refusal prefix and the
  error prefix. `auto` uses the Dutch lines.
  The gate's reason and a provider error text stay English. The relay's own lines (the goodbye when the daemon is
  offline, "Lost connection, I'll message you." and its default failure line) follow the call language: the daemon sends
  `lang` (`nl` for Dutch and Auto, `en` for English) in the call's `custom_llm_extra_body` next to `call_id`, ElevenLabs
  returns it on every `/llm` request, and the relay speaks Dutch for `nl` and English otherwise (an older daemon sends
  none).

Greeting and request detection (step 4 above) knows Dutch: greetings such as "hallo", "ja", "met <name>",
"goedemiddag", and request words and phrases such as "wat", "wie", "hoe", "waarom", "kun", "kan", "check", "start",
"vertel", "zeg het maar", "ga je gang". Per bot (`BotConfig.Jarvis`): enabled, policy text (2000 characters), voice, TTS model.
The voice model choices are a curated list in `internal/jarvis/elevenlabs.go` (`curatedModels`: Flash v2.5, recommended
for calls; v3 Conversational; Turbo v2.5, older; the first two name Dutch support), filtered to what the account's `/v1/models` offers. Voices carry
ElevenLabs' description, or one built from their labels. The UI picker is `frontend/src/features/jarvis/VoicePicker.tsx`.

**Voices per call language** (`internal/jarvis/voices.go`). `jarvis.voices {language}` (empty = the chosen call
language; `auto` lists Dutch) returns `{language, groups: [{label, voices}], library}`; each voice carries `id`,
`publicOwnerId` (Voice Library voices), `name`, `description`, `accent`, `gender`, `age`, `previewUrl` and `inLibrary`.
Sources are ElevenLabs' Voice Library (`GET /v1/shared-voices`, sorted by `usage_character_count_1y`) and the account's
own voices (`GET /v1/voices`, returned as `library` to name a stored voice outside the groups):

- Dutch: shared voices with `language=nl`, plus a best-effort `locale=nl-BE` query so Flemish voices appear. Grouped by
  the voice's Dutch locale or accent (from its `verified_languages` entry for `nl` when present): "Standaard
  Nederlands" (nl-NL, a standard or no accent), "Vlaams" (nl-BE, Flemish or Belgian accent), "Andere" (anything else).
  Conversational voices first, then featured, then most used; at most 12 per group.
- English: one group "Best for calls": the account's premade voices labelled conversational, then up to 15 shared
  voices with `language=en` and `use_cases=conversational` (featured first).

Lists are cached in the daemon for 10 minutes per language; saving a new ElevenLabs key clears the cache.
`jarvis.voices.add {voiceId, publicOwnerId}` runs when the owner picks a voice that is not in their library: the
daemon checks the library itself, adds a Voice Library voice with `POST /v1/voices/add/{public_user_id}/{voice_id}`
(`new_name` = the voice's name) and returns the id to store (audit `jarvis.voice.add`). ElevenLabs documents adding
shared voices but not using one directly by id in an agent, so the daemon always adds before storing. Each option has
a preview button that plays the voice's public `preview_url` clip in the app (one clip at a time; the app sets no
content security policy, so the ElevenLabs CDN needs no allowance).

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
`<relay>/llm`, secret header, empty first message, the turn settings above, the `end_call`, `skip_turn` and
`voicemail_detection` tools), the Twilio number imported and assigned to it, and
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
| daemon -> relay | `llm.delta` / `llm.done` / `llm.error` | Streamed text for that request; `llm.error` text is spoken. A delta of `"... "` is the buffer chunk that keeps ElevenLabs waiting |
| daemon -> relay | `llm.tool` | Answers the request with a function call (`tool`, `args` as a JSON string), used for `skip_turn` and for `end_call` (`{"reason": ...}`, after the goodbye's words): the relay streams it as an OpenAI tool call (`delta.tool_calls`, a chunk with `finish_reason: "tool_calls"`, `[DONE]`) after any text already sent and ends the request. The daemon follows it with `llm.done`, which the relay ignores |
| relay -> daemon | `inbound.lookup` | Inbound webhook: `caller`, `called`, `callSid` (no conversation id: ElevenLabs has not created one yet) |
| daemon -> relay | `inbound.answer` | `accept`, `vars`, `overrides` (voice and model), or `accept:false`; the relay adds `custom_llm_extra_body` `{call_id}` and, when `vars` carries it, `lang` |
| relay -> daemon | `call.status` | Post-call webhook: `status`, `conversationId`, `durationSecs` |

There is no hang-up frame: a goodbye ends the call through ElevenLabs' `end_call` (an `llm.tool` frame), and otherwise
the daemon hangs up through Twilio itself. The custom-LLM endpoint answers on `/llm/chat/completions` and on the
aliases `/llm` and `/llm/v1/chat/completions`.

**The relay must be redeployed (`bunx wrangler@4 deploy` in `relay/`) after updating the app.** An old relay ignores
`llm.tool` but ends the request at once on the `llm.done` that follows it, so every `skip_turn` becomes an empty answer:
ElevenLabs retries it and, after a few attempts, fails the call. An old relay also speaks its own lines in English.
From version 2 on the relay answers `GET /health` with `{"daemon": <connected>, "version": 2}`. Each time the relay
connects, the daemon reads it once; when the version is missing or below 2 it logs `jarvis: the relay is out of date;
redeploy it` and Settings > Phone shows "The call relay is out of date: redeploy it (see relay/README.md). Until then a
silence can drop a call." Until it is redeployed, a goodbye ends the call through Twilio after a tighter estimate of
its speaking time instead of through `end_call` (see Ending a call). A health page it cannot read is only logged, and
the relay is then taken to be current.

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
| Socket drops mid-call | The relay waits 5 s, then speaks "Lost connection, I'll message you." (Dutch: "De verbinding is weggevallen, ik stuur je een bericht.") and ends the turn. |
| ElevenLabs ends the call itself (for example the custom LLM timed out) | The outcome check about 10 s after the call logs the provider's reason and notifies "Call with <bot> dropped". |
| A turn runs for about a minute without finishing | The turn is cancelled and the bot offers to run the work as a background task; work already handed off continues. |
| The relay was not redeployed | `skip_turn` is not understood and answers are empty; ElevenLabs retries and drops the call (see Relay protocol). Settings > Phone says the relay is out of date, and a goodbye ends the call through Twilio instead of `end_call`. |
| ElevenLabs does not end the call after a goodbye | The daemon hangs it up through Twilio 4 s after the goodbye's estimated end (`jarvis: hanging up at the carrier`), unless Twilio reports the call over. |
| Model or tool error | Spoken briefly, recorded in the chat, the call continues. |
| Provider error placing a call | The call is `failed`; notification with the provider's error. |
| Caps, quiet hours, cool-down | No call; notification with the reason. |
| Gate fails mid-call | The bot says it cannot act, Jarvis is turned off for it, audit entry plus notification, the call ends. |
| Opener invalid or late | Fallback template. |
| Webhook never arrives | The 5 s Twilio (then ElevenLabs) poll or the 30-minute ceiling ends the call. |
| An answered call gets no LLM request for 5 minutes | The daemon hangs it up and ends it as completed (see Ending). |
| Voicemail answers | Nothing about the reason is said (the opener is name-only); the voicemail check hangs up; notification, urgent retry. |
| Approval needed mid-call | The tool fails at once with "Needs your approval in the app"; the bot says so. |
| Injection in event titles, task output, mail, MCP results | Stays fenced as data in the brief, injected news and tool results; the opener prompt carries none of it. |

## Audit entries

`jarvis.enable.checked`, `jarvis.disabled`, `jarvis.call.started`, `jarvis.call.ended`, `jarvis.inbound.rejected`,
`jarvis.rule.add`, `jarvis.rule.delete`, `jarvis.secrets.set`, `jarvis.relay.secrets`, `jarvis.configure`,
`jarvis.verify.started`, `jarvis.webhook.orphaned`, `jarvis.voice.add`. Values of secrets are never recorded.

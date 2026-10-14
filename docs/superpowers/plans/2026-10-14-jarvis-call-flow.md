# Jarvis call flow: reliable, natural phone turns

Spec: `docs/superpowers/specs/2026-10-14-jarvis-mode-design.md` (Jarvis mode). This plan amends its call-turn behaviour
after real calls failed. Branch: `jarvis-mode`.

## Evidence (2026-10-14 test calls)

- ElevenLabs ended three calls itself: conversation `status: failed`, `termination_reason: "custom_llm generation
  failed"`, `metadata.error = {code: 1002, error_type: "llm_error", reason: "custom_llm_error: LLM Cascade Error:
  TimeoutError"}`. In each, our SSE stream went quiet (no first token for ~4 s, or one short sentence followed by ~5 s
  of nothing while a tool ran).
- `agent.prompt.cascade_timeout_seconds` defaults to **4** (range 2-15; lowered from 8 in July 2026). A custom LLM is
  retried (at least 3 attempts) on timeouts, errors **and empty responses**, then the conversation fails. That is the
  duplicate-request pattern (~4 s apart) and the dropped calls.
- `turn.turn_eagerness` is `eager` in our agent config, so the owner's turn ends on hesitations ("geef me een, uhm-").
- With `first_message` empty, ElevenLabs waits `initial_wait_time` (falls back to `turn_timeout`, default 7 s) before
  it asks the LLM for the first turn: an inbound caller hears 7 s of silence, then the opener.
- After `turn_timeout` seconds of owner silence ElevenLabs sends a user message `"..."` (platform event
  `user_turn_timeout`).
- System tools configured on the agent (`end_call`, `skip_turn`, ...) are passed to a custom LLM in the OpenAI `tools`
  parameter; the LLM calls one by returning an ordinary function call in the stream, and ElevenLabs executes it.
  `skip_turn` makes the agent stay silent and wait for the user.
- ElevenLabs docs: to buy time, stream a first chunk ending in `"... "` (with the trailing space).
- Diagnostics: `GET /v1/convai/conversations/{id}` returns `status`, `metadata.termination_reason`, `metadata.error`
  (`code`, `error_type`, `reason`), `metadata.phone_call` (`direction`, `call_sid`, ...);
  `GET /v1/convai/conversations?agent_id=&page_size=` lists conversations (`conversation_id`, `start_time_unix_secs`,
  `call_duration_secs`, `status`).

## Global constraints

- AGENTS.md applies: secrets only via `internal/credentials`, never in logs; no network listener; untrusted content
  fenced; `go fmt`, `go vet`, `go test -race` clean for touched packages; relay `bun run --cwd relay test` (or the
  existing relay test command) clean when the relay changes.
- Logs record timings, kinds and counts only, never spoken or transcribed text.
- Another session is editing `CHANGELOG.md`, `docs/architecture/jarvis.md` and files under `frontend/` in this checkout.
  Tasks 1-7 must not touch those files. Stage files explicitly by path (never `git add -A`/`.`), never commit build
  artefacts. Commits: conventional, lowercase, ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Timing constants are fields on `Calls` (or package constants copied into fields in `NewCalls`) so tests can shorten
  them; tests never sleep for real seconds.

## Task 1: agent configuration revision and call diagnostics (internal/jarvis)

Files: `internal/jarvis/elevenlabs.go`, `internal/jarvis/types.go`, `internal/jarvis/service.go`,
`internal/jarvis/provision.go`, `internal/jarvis/calls.go` (finish hook only), tests in the same package.

1. `agentBody` sets, in addition to what it sends today:
   - `conversation_config.turn`: `turn_eagerness: "patient"`, `turn_timeout: 10`, `initial_wait_time: 1`,
     `speculative_turn: false`, `silence_end_call_timeout: -1`, `soft_timeout_config: {timeout_seconds: -1}`.
     (Replaces `turn_eagerness: "eager"`.)
   - `conversation_config.conversation.max_duration_seconds: 1800` (matches the daemon's 30-minute ceiling).
   - `conversation_config.agent.prompt.cascade_timeout_seconds: 15`.
   - `built_in_tools.skip_turn`: `{"name": "skip_turn", "params": {"system_tool_type": "skip_turn"}}` (same shape as
     the existing `end_call` entry).
   Update the `agentBody` doc comment. Keep every existing field.
2. Agent revision: add `const agentRevision = 2` and `PhoneSettings.AgentRevision int` (`json:"agentRevision"`),
   set to `agentRevision` wherever provisioning records `AgentLanguage` (create and update paths in provision.go).
   Generalise `syncAgentLanguage` into `syncAgent`: it re-PATCHes the agent in the background when
   `AgentLanguage != callLanguage()` **or** `AgentRevision < agentRevision`, then saves both fields (same guards,
   locking and notify-on-error as today; the error notice title becomes "Couldn't update the phone agent"). Rename
   call sites. So after this ships, the first daemon start re-provisions the existing agent once.
3. Diagnostics after every call: new `ElevenLabs.ConversationOutcome(ctx, conversationID) (Outcome, error)` returning
   `Outcome{Status, TerminationReason, ErrorType, ErrorReason string}` from the conversation detail, and
   `ElevenLabs.FindConversation(ctx, agentID, callSID string, near time.Time) (string, error)`: list
   `?agent_id=<id>&page_size=10`, for entries whose `start_time_unix_secs` is within 120 s of `near`, fetch the
   detail and return the one whose `metadata.phone_call.call_sid` equals callSID. Add both to the `Dialer` interface
   (or a new small interface the calls manager gets) and to the test fakes.
   After `finish` ends a call, spawn (on `c.spawn`, with a 30 s timeout, after a 10 s delay so ElevenLabs has
   recorded it) a check: conversation id from the record, else `FindConversation` with the agent id, provider call
   SID and answered/started time. Log `jarvis: call outcome` with call id, status, termination_reason, error_type,
   error_reason (these are provider metadata, never transcript text). If `status == "failed"` notify the owner once:
   title "Call with <bot> dropped", body "The voice service ended the call: <error_reason or termination_reason>"
   (clip to 200 runes), level "warning", target `bot:<id>`. Store the conversation id on the record if it was
   missing (persist). Failures of the check itself are logged at Info and otherwise ignored.
4. Tests: agentBody contains the new turn/conversation/prompt fields and `skip_turn`; syncAgent runs when only the
   revision is behind and saves `AgentRevision`; the outcome check notifies on `failed` and stays quiet on `done`;
   FindConversation matches by call SID (httptest server).

## Task 2: tool calls through the relay (relay/ + internal/jarvis/relay.go)

Files: `relay/src/hub.ts`, `relay/test/*`, `relay/README.md`, `internal/jarvis/relay.go`, `internal/jarvis/calls.go`
(helper only), Go tests.

1. Frame protocol: new frame type `llm.tool` (daemon -> relay) with `id`, `callId`, `tool` (function name) and `args`
   (JSON object as a string, default `"{}"`). Go: `FrameLLMTool = "llm.tool"`, `Frame.Tool string json:"tool,omitempty"`,
   `Frame.Args string json:"args,omitempty"`.
2. Relay: on `llm.tool` for a pending LLM request, write one OpenAI `chat.completion.chunk` SSE event whose
   `choices[0].delta.tool_calls` is `[{index: 0, id: "call_<random>", type: "function", function: {name, arguments}}]`,
   then a chunk with `finish_reason: "tool_calls"` and empty delta, then `data: [DONE]`, and close (same `finish`
   bookkeeping as `llm.done`). Text deltas sent before it stay in the stream before the tool call. Verify the chunk
   shape against the ElevenLabs custom LLM docs (https://elevenlabs.io/docs/eleven-agents/customization/llm/custom-llm)
   and OpenAI streaming tool-call format; note in the report what was verified.
3. Relay language: ElevenLabs passes `elevenlabs_extra_body` on every request. The daemon will add `lang` ("nl" or
   "en") next to `call_id` (Task 2 also changes the Go side: the outbound call's `custom_llm_extra_body` and the inbound
   answer's `custom_llm_extra_body` include `lang: "nl"` for Dutch and Auto, `"en"` for English; the relay copies vars
   into extra body today only for `call_id`, extend it with `lang`). The relay's GOODBYE and LOST lines get Dutch
   versions used when `lang === "nl"`: GOODBYE "Sorry, ik kan je bots nu niet bereiken. Ik stuur je een bericht." and
   LOST "De verbinding is weggevallen, ik stuur je een bericht.".
4. Go helper on `Calls`: `skip(ctx, f Frame)` sends `Frame{Type: FrameLLMTool, ID: f.ID, CallID: f.CallID, Tool:
   "skip_turn", Args: "{}"}`. Not called yet except by its test (Task 5 uses it).
5. Tests: relay unit test for the tool-call SSE sequence and for Dutch LOST/GOODBYE; Go test for the frame JSON shape
   and the extra-body `lang` on outbound and inbound.
6. README: document the frame and that the relay must be redeployed (`wrangler deploy`) for skip_turn to work; an old
   relay ignores `llm.tool`, so the request ends empty.

## Task 3: voice prompt, cue turns and the end_call tool (internal/chat, internal/chat/tools)

Files: `internal/chat/voice.go`, `internal/chat/prompt.go`, `internal/chat/store.go` / `manager.go` (CallMessages
filter only), `internal/chat/tools/jarvis.go`, tests.

1. Cue turns: `VoiceRequest.Cue bool`. A cue turn's `Text` is an instruction from the daemon, not the owner's words: it
   is stored as the user message with `Author: "cue"` (still `CallID`-tagged and hidden from chat like other call
   messages), and `CallMessages` (the call transcript) leaves out user messages whose author is `"cue"`. History
   given to the model keeps it.
2. Voice turns see delegations: `VoiceTurn`'s `extra` is `req.Brief` plus `m.handoffsNote(bot.ID)` (joined with a
   blank line, empty parts skipped), so the bot can answer "how is that task going?".
3. Voice style (`voiceStyle` / `buildSystem` voice branch), replacing the lines added in commit bc5811d where they
   overlap:
   - "Your name is <bot.Name>. Use only that name for yourself." (computed in buildSystem from `in.Bot.Name`).
   - "When you hand work to another bot or start a background task, say who is doing it and why in one sentence
     (for example: I've passed it to Code Assistant, it specialises in that), then ask whether there is anything
     else you can do in the meantime."
   - "When the owner asks how a handed-off or started task is going, check it with your tools before answering."
   - "When the owner asks you to call back when it is done, save a call rule for it with jarvis_rule_add (trigger
     bot_reported for work you handed to a bot, task_finished for a code task) and confirm it."
   - "When you have done what the owner asked, ask briefly whether there is anything else."
   - "Only end the call when the owner clearly says they are done (no, that's all, thanks, bye): say a short friendly
     goodbye and call end_call in the same reply. Never end the call for any other reason."
   - Keep the existing lines (short sentences, no markdown, never name the app, approvals, voicemail).
4. `end_call` tool description: "On a phone call only: hang up after this reply. Only when the owner has clearly said
   they are done (no, that's all, thanks, bye). Say a short, friendly goodbye in the same reply." Result text
   unchanged; a runtime refusal is returned as a tool error (already the case; keep a test).
5. `jarvis_rule_add` schema: add `bot_reported` to the trigger enum; the target description gains "for bot_reported:
   the name of the bot you handed the work to, or \"any\"".
6. Tests: cue messages hidden from CallMessages but present in model history; voice prompt contains the bot name and
   the new lines; handoffsNote appears in a voice turn's system prompt while a handoff is out.

## Task 4: follow-ups of a call stay in the call thread (internal/chat, internal/agents, internal/daemon)

Owner requirement: nothing that comes out of a phone call may appear in the bot's main chat thread. Everything (the
owner's words, the bot's replies, tool and delegation items, later reports from bots or code tasks started on the
call, and the bot's answers to those reports) lives in the call's thread (messages with `call_id`), shown in the call
pane. The call card itself stays in the chat. The callee bot's own thread (the task handed to it) is that bot's work
and stays as it is.

Files: `internal/chat/queue.go`, `internal/chat/engine.go`, `internal/chat/delegate.go`, `internal/chat/automation.go`,
`internal/chat/api.go` (hook), `internal/chat/tools/tasks.go`, `internal/agents/*` (session call id),
`internal/daemon/bot_tasks.go`, migrations if needed, tests.

1. Call-tagged turns: `turn.callID string`. `submit` stores the user message with `CallID` = the turn's call id and
   publishes it with `publishCallMessage` when set. `runTurn` gives the assistant message the same `CallID`, its sink
   publishes with `publishCallMessage`/`publishCallDelta`, and the `chat.run` emission is skipped for call-tagged
   turns (check `cancelQueued` and error paths publish through the call publishers too; reply notifications and
   `bot.message` run on `chat.message` only, so they are skipped automatically). A call-tagged turn otherwise runs
   exactly like a chat turn (the bot's queue, tools, memory).
2. Delegation: `handoff` gets `callID` from `from.CallID`. `awaitReport` stores the report with `CallID = h.callID`
   and queues the answering turn with `callID: h.callID`. Before queuing it calls the hook (3); when the hook returns
   `true` (the report was handed to the live call to be spoken) the report message is still stored in the call thread
   but no written answering turn is queued.
3. Hook: `Manager.SetFollowUpHook(func(FollowUp) bool)`, `FollowUp{BotID, CallID, Kind ("report" | "task"), From
   (callee bot name or task title), Status ("complete" | "failed" | "stopped" for reports; the task topic's status for
   tasks), Text (clipped to 2000 runes)}`. Called for every report and task report, call-tagged or not, outside any
   lock, before queuing; returns whether the follow-up was delivered into a live call. Nil hook = false.
4. Code tasks: a task started by a bot during a call remembers the call: `agents.Session.CallID` (persisted the same way
   as `Owner`; add a migration only if `Owner` is a column), set from the run context: `tools/tasks.go` passes
   `rc.CallID` to `TaskRuntime.Start` (extend the signature or the input struct), the daemon's bot task runtime puts it
   on the session (e.g. `agents.WithCall(ctx, callID)` next to `WithActor`). `reportToOwner` passes `s.CallID` to
   `Chat.ReportTask(ctx, botID, taskID, text, callID)`, which stores the task report with that `CallID`, calls the hook
   (Kind "task") and, unless delivered live, queues the answering turn with `callID`.
5. Tests: a delegation started on a call (rc.CallID set) stores its report and answer with that call id and publishes
   only call events, never `chat.message`/`chat.run`; with the hook returning true no answering turn runs; a
   delegation from chat is unchanged; a code task started on a call reports into the call thread; the session keeps
   its call id across a store round-trip.

## Task 5: reports during and after calls (internal/jarvis, internal/daemon)

Files: `internal/jarvis/types.go`, `internal/jarvis/rules.go`, `internal/jarvis/service.go`,
`internal/jarvis/calls.go`, `internal/daemon/svc_jarvis.go` (and where the chat manager is wired), tests.

1. Trigger `BotReported JarvisTrigger = "bot_reported"`: rule target is a bot name (case-insensitive) or `"any"`.
   `Event` gets the callee name in `Source` and the caller bot id in `OwnerBotID`; a rule matches only if
   `r.BotID == ev.OwnerBotID` and the target is `any` or equals the callee name. Rule validation accepts the new
   trigger (non-empty target).
2. The daemon sets the chat follow-up hook to `Jarvis.OnFollowUp(f) bool`:
   - If a call is live with `f.BotID`: fold it into the call as news (`Calls.Inject(callID, from, reason)`; reuse the
     `inject` slice/`injected` type: bot = f.From, reason = "<status>: <text>" clipped to 500 runes, fenced where it
     reaches a model as today). Return true only when `f.CallID` equals the live call's id (a chat-originated
     follow-up is also told on the phone but still gets its written answer in chat).
   - Otherwise, for Kind "report", build `Event{Trigger: BotReported, OwnerBotID: f.BotID, Source: f.From, Title:
     f.From + " reported back" (+ " (failed)" / " (stopped)"), Detail: f.Text}` and pass it to `OnEvent` (rule
     matching, call placing, quiet hours, caps as for other events). Task follow-ups already reach rules through task
     events; do not fire them twice.
   - When `f.CallID` is set and that call has ended, notify the owner: title "<From> reported back", body "From your
     call with <bot>. Open the call to read it.", target `bot:<id>`, level "info" (the written answer lands in the
     call thread, which raises no chat notification).
   - Returns false in every other case.
3. Tests: rule matching by callee name and any; a follow-up with the live call's id is injected, returns true and does
   not dial; a chat-originated follow-up during a live call is injected and returns false; no live call + rule -> a
   call is placed with the report in the brief; ended call -> notice; no rule -> no call.

## Task 6: turn handling on calls (internal/jarvis/calls.go, lang.go)

Files: `internal/jarvis/calls.go`, `internal/jarvis/lang.go`, `internal/jarvis/calls_test.go`.

Turn kinds, decided in `turn` after the voicemail check, and logged once per request as `jarvis: request` with call
id, kind, message count (no text): `opener`, `new`, `bargein` (cancels an in-flight turn), `retry` (adopted),
`silence`, `hesitation`, `news`, `pending` (answering a held hesitation).

1. Keep-alive, replacing the bc5811d keep-alive (`ackAfter` 1.5 s / `stillAfter` 12 s / 3 nudges):
   - On every model turn emit the buffer chunk `"... "` immediately (before the gate/bot lookup). It does not count as
     spoken words: track `words time.Time` (last model or scripted words) separately from `last` (any chunk).
   - `ackAfter` 1.5 s: if no words yet, speak the acknowledgement line (as today).
   - `bufferEvery` 3 s: whenever nothing at all was sent for 3 s, send `"... "` (keeps the cascade timer fed without
     words).
   - `stillAfter` 12 s: when no words for 12 s, speak a still-working line; at most 3 per turn (then only buffers).
   - `turnDeadline` 60 s: a turn still running at 60 s is cancelled; the speaker says `lines.tooLong` and finishes
     (nl: "Dit duurt langer dan verwacht. Ik werk verder en laat het je weten.", en: "This is taking longer than
     expected. I'll keep at it and let you know.") and the history notes it like a superseded turn.
   - Filler skipping after the acknowledgement stays.
2. Silence (`"..."`, `isSilence`) after the first turn, in this order:
   - a held hesitation exists (see 3): answer it as kind `pending`;
   - undelivered news exists (`len(l.inject) > l.delivered`): run a model turn as kind `news` with `Cue: true` and
     Text "(Nobody spoke. News came in while you were talking: tell the owner briefly, then ask if there is anything
     else.)" (English instruction; the voice prompt sets the spoken language);
   - otherwise count consecutive silences on the call (reset by any non-silence utterance): on the 3rd say
     `lines.stillThere` (nl "Ben je er nog?", en "Are you still there?") and finish; on the 9th say `lines.byeSilent`
     (nl "Ik hoor je niet meer, dus ik hang op. Bel me gerust terug.", en "I can't hear you anymore, so I'll hang up.
     Call me back anytime.") and `hangupAfter`; else `c.skip(ctx, f)` (no words, no empty response).
3. Hesitation: an utterance whose last word (lowercased, trailing punctuation and dashes removed) is a hesitation or
   connector, or that ends with "-" or "…": hesitations `uh uhm um uhh hmm eh ehm euh eeh`, Dutch connectors `en maar
   of dus want omdat zoals een de het`, English `and but or so because like the a`. Then hold the utterance on the
   call (`l.held`), `c.skip(ctx, f)`, kind `hesitation`. The next non-silence utterance is answered as
   `held + " " + utter` unless `utter` already starts with `held` (case-insensitive, ignoring trailing "-" / "…" /
   punctuation of held); the held text is then cleared. Bare greetings and the first turn are never held.
4. `end_call` guard: `EndAfterReply` refuses (error "The owner has not said they are done. Ask whether there is
   anything else instead.") unless the utterance of the turn in flight is a closing: it contains one of nl `nee`
   (as a word), `dat was het`, `dat is alles`, `bedankt`, `dankje`, `dank je`, `merci`, `dag`, `doei`, `tot later`,
   `tot ziens`, `salut`, `ciao`, `nee hoor`, `niks`, `laat maar`, `ophangen`, `hang op`; en `no`, `that's all`,
   `that is all`, `thanks`, `thank you`, `bye`, `goodbye`, `talk later`, `nothing`, `hang up`. (Store the in-flight
   utterance on `live` when a model turn starts.)
5. Empty responses never go out after the first turn: every path that would `finish` without words now either speaks,
   or calls `skip`. The verify call and `endUnknown` keep their current behaviour.
6. Tests (fake relay sees `llm.tool` frames): silence -> skip_turn tool frame; 3rd silence -> "Are you still there?";
   9th -> goodbye and hang-up; news on silence -> cue turn with `Cue: true`; hesitation held then combined with the
   next utterance (both "continuation only" and "repeated full sentence"); buffer chunk sent first; ack after
   ackAfter; still lines capped; deadline cancels and speaks tooLong; end_call refused without a closing and accepted
   with "nee, bedankt"; request kinds logged without text.

## Task 7: call pane shows follow-ups (frontend)

Files: `frontend/src/features/jarvis/CallPane.tsx` (+ its test), `frontend/src/features/jarvis/describe.ts` if useful.
Do not touch files another session is editing (`JarvisPage*`, `PhoneSettings.tsx`, `VoicePicker.tsx`,
`components/ui/popover.tsx`, `notifications/NotificationCenter.tsx`); check `git status` before editing.

1. User-role call messages are rendered by author: `"user"` (or empty) -> the owner's bubble as today;
   `"report:<botId>"` -> a compact item "<bot name> reported back" with the text collapsed (expand on click), styled
   like the main chat's report card (reuse `MessageBubble`'s report rendering if it is exported or small enough to
   share; otherwise mirror its look); `"task:<taskId>"` -> a "Code task update" item with collapsed text and the task
   button when the task id is known; `"cue"` -> nothing (the daemon already filters them; render nothing as a
   guard); any other author -> a collapsed generic item with the author's label.
2. Messages that arrive after the call ended (follow-ups) appear live through `chatCallMessage` as today; the header
   status stays "Ended".
3. `bun run --cwd frontend check` clean (tsc, lint, vitest); add tests for the renderings.

## Task 8: documentation (controller)

`docs/architecture/jarvis.md` (call turns, agent config, diagnostics, bot_reported, relay frame), `CHANGELOG.md`,
`docs/development/jarvis-e2e.md` (relay redeploy step), `relay/README.md` (Task 2). Done by the controller after
Tasks 1-5, committing only its own hunks if another session still has uncommitted edits in those files.

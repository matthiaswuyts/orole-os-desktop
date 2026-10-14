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
  Tasks 1-5 must not touch those files. Stage files explicitly by path (never `git add -A`/`.`), never commit build
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

## Task 3: chat side of calls (internal/chat, internal/chat/tools)

Files: `internal/chat/voice.go`, `internal/chat/prompt.go`, `internal/chat/delegate.go`, `internal/chat/api.go` (or
wherever `Manager` options live), `internal/chat/tools/jarvis.go`, `internal/daemon/svc_jarvis.go` (transcript only),
tests.

1. Cue turns: `VoiceRequest.Cue bool`. A cue turn's `Text` is an instruction from the daemon, not the owner's words: it
   is stored as the user message with `Author: "cue"` (still `CallID`-tagged and hidden from chat like other call
   messages), and `CallMessages` / the call transcript (`jarvisTranscript`, `CallMessages`) leaves out user messages
   whose author is `"cue"`. History given to the model keeps it.
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
4. Reports reach Jarvis: `Manager` gets an optional hook (option or setter) `OnReport func(callerBotID string, callee
   Bot, status, text string)` called by `awaitReport` after the report is stored (status: "complete", "failed",
   "stopped"; text: `reportText`, clipped to 2000 runes). Never blocks (call in a goroutine or document that the
   hook must not block).
5. `end_call` tool description: "On a phone call only: hang up after this reply. Only when the owner has clearly said
   they are done (no, that's all, thanks, bye). Say a short, friendly goodbye in the same reply." Its result text
   unchanged. If the runtime refuses (Task 5 adds the guard), return the refusal as a tool error.
6. `jarvis_rule_add` schema: add `bot_reported` to the trigger enum; target description gains "for bot_reported: the
   name of the bot you handed the work to, or \"any\"".
7. Tests: cue messages hidden from CallMessages but present in model history; voice prompt contains the bot name and
   the new lines; handoffsNote appears in a voice turn's system prompt while a handoff is out; OnReport fires once per
   report with the right status.

## Task 4: reports to calls and call-back rules (internal/jarvis, internal/daemon)

Files: `internal/jarvis/types.go`, `internal/jarvis/rules.go`, `internal/jarvis/service.go`,
`internal/jarvis/calls.go`, `internal/daemon/svc_jarvis.go` (+ wherever the chat manager hook is wired in the
daemon), tests.

1. Trigger `BotReported JarvisTrigger = "bot_reported"`: rule target is a bot name (case-insensitive) or `"any"`.
   `Event` gets the callee name in `Source` and the caller bot id in `OwnerBotID`; a rule matches only if
   `r.BotID == ev.OwnerBotID` and the target is `any` or equals the callee name. Rule validation accepts the new
   trigger (non-empty target).
2. Daemon wires `chat.Manager.OnReport` to `Jarvis.OnReport(callerID, calleeName, status, text)`.
3. `Service.OnReport`:
   - If a call is live with `callerID` as its bot: fold the report into the call as news (`Calls.Inject(callID,
     from, reason)`; reuse the `inject` slice/`injected` type: bot = callee name, reason = "finished: <text>" clipped
     to `maxTitle`-style limit of 500 runes, fenced where it reaches a model as today). Do not also place a call.
   - Otherwise build an `Event{Trigger: BotReported, OwnerBotID: callerID, Source: callee, Title: callee + " reported
     back" (+ " (failed)" / " (stopped)"), Detail: text}` and pass it to `OnEvent` (rule matching, call placing,
     quiet hours and caps all as for other events).
4. Tests: rule matching by callee name and any; live-call report is injected and does not dial; no live call + rule ->
   a call is placed with the report in the brief; no rule -> nothing.

## Task 5: turn handling on calls (internal/jarvis/calls.go, lang.go)

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

## Task 6: documentation (controller)

`docs/architecture/jarvis.md` (call turns, agent config, diagnostics, bot_reported, relay frame), `CHANGELOG.md`,
`docs/development/jarvis-e2e.md` (relay redeploy step), `relay/README.md` (Task 2). Done by the controller after
Tasks 1-5, committing only its own hunks if another session still has uncommitted edits in those files.

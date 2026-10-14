# Jarvis end-to-end checklist

A manual run with real accounts. The owner runs it; nothing in CI touches Twilio, ElevenLabs or Cloudflare. Design and
behaviour: `docs/architecture/jarvis.md`.

You need: a Twilio account with a voice-capable number, an ElevenLabs account and API key (Agents, Voices and
Text-to-Speech), a Cloudflare account with Workers, and your own mobile number. ElevenLabs' inbound and post-call
webhooks are workspace-wide, so use a workspace without other agents you rely on. Use a dev data dir
(`OROLE_HOME=.orole-dev`) if you do not want to touch your real data.

## 0. Live smoke test of the unverified ElevenLabs fields

The provisioning code sends these fields, written from the public docs and not yet run against the live API. Run this
first; if any item fails, fix `internal/jarvis/elevenlabs.go` (and `relay/` if the path differs) before the rest.

- [ ] **Initiation webhook flag.** After Connect, open the agent in the ElevenLabs dashboard (or `GET
      /v1/convai/agents/<id>`): `platform_settings.overrides.enable_conversation_initiation_client_data_from_webhook` is
      true and Security shows the personalization webhook at `<relay>/inbound`. An inbound call (step 8) reaches the
      daemon as `inbound.lookup`.
- [ ] **Per-call TTS model override.** Place a call for a bot with a TTS model set (or via `jarvis.call.test`): the call
      uses that bot's voice and `tts.model_id`, not the agent default, and ElevenLabs does not reject the override
      ("overrides not allowed" or a 4xx in the call card's error).
- [ ] **String-form `request_headers`.** `request_headers` is sent as `{"x-orole-relay": "<secret>"}` both on the
      agent's custom LLM and on the workspace webhooks. Confirm the relay receives the header: a call gets a spoken reply
      (a 401 in `bunx wrangler@4 tail` means the header was not sent or was sent in another form).
- [ ] **Custom LLM path.** Run `bunx wrangler@4 tail` in `relay/` during a call and confirm ElevenLabs POSTs to
      `<relay>/llm/chat/completions` (the agent's URL is `<relay>/llm`). If it posts to `/llm` or
      `/llm/v1/chat/completions`, add that path to `relay/src/index.ts` and `relay/README.md` before continuing.
- [ ] **Post-call webhook.** After a call, the relay logs a POST to `/status` that passes the HMAC check once
      `WEBHOOK_SECRET` is set, and the call card ends within a few seconds (not after the 5 s poll).
- [ ] **Voicemail tool.** The agent shows the `voicemail_detection` system tool (configured, but the daemon's own
      voicemail check is what ends the call).
- [ ] **Turn settings.** After the first daemon start on this build (or Connect), `GET /v1/convai/agents/<id>` shows
      `turn_eagerness: patient`, `turn_timeout: 10`, `initial_wait_time: 1`, `silence_end_call_timeout: -1`,
      `cascade_timeout_seconds: 15`, `max_duration_seconds: 1800` and the `skip_turn` built-in tool. If ElevenLabs
      rejects a field you get a "Couldn't update the phone agent" notice: fix `agentBody` in
      `internal/jarvis/elevenlabs.go` (`soft_timeout_config` and `speculative_turn` are the least certain).
- [ ] **skip_turn through the relay.** Stay silent on a call for about 10 s: the agent stays quiet and the call goes on
      (`bunx wrangler@4 tail` shows no repeated `/llm` requests). If it does not register, the tool-call chunk in
      `sseTool` (`relay/src/hub.ts`) is the place to look.
- [ ] **Buffer chunk.** A slow answer (step 13) does not end the call: ElevenLabs accepts the `"... "` chunk as a
      reason to keep waiting.

## 1. Setup

- [ ] Deploy the relay (`relay/README.md`): `bunx wrangler@4 deploy`, set `RELAY_TOKEN` and `LLM_SECRET` from Settings
      > Phone > Generate relay secrets (shown once), note the Worker URL.
- [ ] **Updating an existing install: redeploy the relay first** (`bunx wrangler@4 deploy` in `relay/`), then start the
      new app. An old relay ignores `skip_turn`, so every silence ends in an empty answer and ElevenLabs drops the call,
      and it speaks its own lines in English. The first daemon start also updates the existing ElevenLabs agent once
      (agent revision 2); no "Couldn't update the phone agent" notice should appear.
- [ ] Settings > Phone: enter the Twilio SID and token and the ElevenLabs key, enter the relay URL and the Jarvis
      number, press Connect. The checklist goes green (Twilio, number, ElevenLabs, relay connected).
- [ ] Connect shows the **webhook secret once**; copy it and run `wrangler secret put WEBHOOK_SECRET`, then redeploy.
      (If you close the panel first, you cannot see it again.)
- [ ] `GET <relay>/health` answers `{"daemon":true}` while the app runs.

## 2. Verify your number

- [ ] Enter your mobile number; the phone rings, reads a six-digit code; entering it marks the number Verified.
- [ ] A wrong code is refused; five wrong codes require starting again.

## 3. The permission gate

- [ ] A bot with tool access All, code tasks on, YOLO tasks, and "Bots may start YOLO tasks" on: turn Jarvis on in the
      bot's Phone calls page. Allowed.
- [ ] Lower the bot's tool access (or turn off YOLO): enabling Jarvis is refused with the failing condition named, and
      the toggle shows the reason.
- [ ] With Jarvis on, make the gate fail (turn off "Bots may start YOLO tasks"). Observe what happens on the next call
      attempt: the call is refused and Jarvis is turned off with a notification.

## 4. Outbound by rule

- [ ] In chat: "call me when task X finishes". The bot saves a rule and confirms it; it is listed in the bot's Phone
      calls page.
- [ ] Finish the task. Your phone rings within 10 s. The first sentence contains the bot's name and **not** "Orole".
- [ ] A once-rule is gone afterwards; a repeating rule is still listed.

## 5. On the call

- [ ] "What's running?" The bot says a short filler, then summarises.
- [ ] "Start a coding task to ..." The task appears in the app during the call and the bot says it started it.
- [ ] Interrupt the bot mid-sentence: it stops and answers the new sentence.
- [ ] Ask it to read a task title containing "Orole": the voice says "the app", not "Orole".
- [ ] "Leave it" (or "laat maar" about a task): the bot acknowledges and stops; it asks whether there is anything else
      and does not hang up unless you say you are done (step 13).
- [ ] The chat shows a call card (direction, status, reason, duration, estimated cost) with the spoken turns and the task
      card around it.

## 6. Not answered

- [ ] Decline the call: notification "<bot> tried to call", card shows a missed call.
- [ ] Let voicemail answer: only the name-only greeting is heard, the call is hung up as soon as the greeting
      ("leave a message after the tone") is transcribed, the card says voicemail and the notification arrives.
- [ ] Answer and say only "Hello?": the bot greets by name and asks for a minute; after "yes" it says why it called.
- [ ] Answer, then hang up from the phone without the post-call webhook secret set: the card ends within ~5 s (Twilio poll).
- [ ] Call in from your phone and ask "what is running?": the bot answers (the inbound call id reaches `/llm`).
- [ ] An `urgent` rule retries once after 5 minutes; a normal rule does not.

## 7. Judgement call

- [ ] Give the bot a "When to call me" policy, trigger the condition: `call_owner` places a call. A second attempt within
      10 minutes is refused with the cool-down reason.

## 8. Inbound

- [ ] Call the Jarvis number from your verified phone: the executive bot greets you by name about a second after
      pickup (not seven) and you get a notification.
- [ ] "Put me through to <other bot>": that bot introduces itself by name; an ungated bot is refused.
- [ ] The chat shows an inbound call card.

## 9. Inbound from another number

- [ ] Call the Jarvis number from a different phone: the call is rejected, nothing is spoken, and the audit log has
      `jarvis.inbound.rejected`. (Caller-ID spoofing is an accepted risk; see the threat model.)

## 10. Quiet hours and limits

- [ ] Set quiet hours around now and fire a rule: a notification instead of a call. A rule with "even in quiet hours"
      calls.
- [ ] Set the daily cap to 1: the second call is refused with the reason in a notification.
- [ ] Add an `at_time` rule two minutes ahead; quit the daemon and restart it after the time passed: you get "missed a
      scheduled call", not a late call.

## 11. Failure

- [ ] Stop the daemon mid-call: the relay says "Lost connection, I'll message you." (in Dutch, "De verbinding is
      weggevallen, ik stuur je een bericht.", with the call language on Dutch or Auto) within about 5 s.
- [ ] With the daemon stopped, call the Jarvis number: one spoken line, then hang-up.
- [ ] Let ElevenLabs fail a call (for example run `wrangler secret put LLM_SECRET` with a wrong value, place a test
      call, then restore the secret and press Connect): about 10 s after it ends, a "Call with <bot> dropped"
      notification names the reason, and `logs/daemon.log` has a `jarvis: call outcome` line with `status=failed`.
- [ ] Change the relay URL, Connect again: the app sends a notice naming the old ElevenLabs webhook id; delete it in the
      ElevenLabs workspace webhook settings.

## 12. Latency

Per-turn timings are not in the call record; they are the `jarvis: turn` lines in `logs/daemon.log`
(`first_audio_ms`, `total_ms`, `opener`). The `jarvis: request` lines beside them give each request's `kind`
(`opener`, `new`, `bargein`, `retry`, `silence`, `hesitation`, `news`, `pending`) and message count, never any text.

- [ ] Opener: first audio at most 300 ms after pickup (`opener:true`).
- [ ] Replies: start at most 1.2 s after you stop speaking.
- [ ] Note the numbers and the voice/model in your test log.

## 13. Natural turns

Speak Dutch (the default call language) or English. Watch `logs/daemon.log` for the `jarvis: request` kinds.

- [ ] **Hesitation.** Say "Geef me een, uhm" and, after a second, "overzicht van mijn taken". The bot does not answer
      the fragment; it answers the whole request once (`kind=hesitation`, then the next request).
- [ ] Say "Ik wil weten, uhm" and stop. About 2.5 s later the bot answers what it heard; it does not wait for a
      silence prompt.
- [ ] **Slow answer.** Ask something that takes a while (a research question, "wat is er allemaal gebeurd?"). Within
      about 2 s you hear a short acknowledgement ("Momentje, ik ben ermee bezig", "Even kijken"); the call is not dropped
      and no "dropped" notice follows.
- [ ] **Silence.** Say nothing. The bot stays quiet (no "I didn't hear anything", no repeated requests in
      `wrangler tail`). After about 30 s it asks "Ben je er nog?"; keep quiet and after about 90 s it says it is
      hanging up and the call ends. Speaking at any point starts the count over.
- [ ] **Closing.** Ask "wat draait er?". The bot answers and asks whether there is anything else; it does not hang up
      by itself. Say "nee, bedankt": a short goodbye and the call ends (`end_call` was accepted). "Wat draait er?" never
      ends the call, even if the bot says goodbye.

## 14. Delegation and follow-ups

Needs a second bot that Jarvis may hand work to, and a bot with code tasks on.

- [ ] On a call: "Vraag <other bot> om ..." The bot says who is doing it and why, asks whether there is anything else
      and keeps talking. "Hoe gaat het ermee?" makes it check before it answers.
- [ ] Stay on the line until the other bot finishes: the bot tells you on the phone what it reported, on your next
      turn or at the next silence. The call pane shows a "<other bot> reported back" item. The first bot's
      chat thread gets no new message.
- [ ] Do it again and hang up before it finishes. When the other bot reports, open the call card: the pane shows the
      report and the bot's written answer, the header still says Ended, and the chat thread has not changed. A
      notification "<other bot> reported back" ("From your call with <bot>. Open the call to read it.") arrives.
- [ ] Start a coding task on the call ("start een codetaak om ...") and hang up. When it finishes, the pane shows "Code
      task update" with an Open the task button and the bot's written answer; the chat thread shows only the call card.
- [ ] **Call back.** On a call, hand work to a bot and say "Bel me terug als het klaar is". The bot confirms; the bot's
      Phone calls page lists "When <other bot> reports back". Hang up. When the other bot reports, your phone rings
      and the bot tells you the report; a once-rule is gone afterwards. For a coding task the rule is "When task ...
      finishes" (`task_finished`) instead.

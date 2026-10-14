-- Jarvis phone calls: the spoken turns of a call (the owner's utterances and the bot's replies)
-- are stored in the bot's thread so the bot remembers the call, but they carry the call id and the
-- chat UI never shows them (paging, previews and live chat events skip them); the call card opens
-- the transcript instead. '' for every other message, including those from before this column.
ALTER TABLE messages ADD COLUMN call_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_messages_call ON messages(call_id, seq) WHERE call_id <> '';

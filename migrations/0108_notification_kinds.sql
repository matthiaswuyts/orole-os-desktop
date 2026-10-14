-- Notifications become typed events. kind (approval, failed, finished, reply, message)
-- selects the in-app/sound preference and the cue; target names what clicking opens
-- ("task:<id>", "bot:<id>", "automation:<id>"); ref identifies the wait an approval
-- reports, and needs_you stays 1 until that wait clears. Existing rows are messages.
ALTER TABLE notifications ADD COLUMN kind TEXT NOT NULL DEFAULT 'message';
ALTER TABLE notifications ADD COLUMN target TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN ref TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN needs_you INTEGER NOT NULL DEFAULT 0;
CREATE INDEX notifications_needs_you ON notifications(ref) WHERE needs_you=1;

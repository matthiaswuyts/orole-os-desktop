-- When a message reached a final status (complete, incomplete, error, canceled), in Unix
-- milliseconds; set once. 0 while it is pending or running, and for messages that finished
-- before this column existed.
ALTER TABLE messages ADD COLUMN finished_at INTEGER NOT NULL DEFAULT 0;

-- markInterrupted scans messages by status at every startup; only the in-flight states are indexed,
-- so the index stays tiny (finished messages are the overwhelming majority).
CREATE INDEX idx_messages_active_status ON messages(status) WHERE status IN ('pending', 'running');

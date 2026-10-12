-- Saved workspaces were removed; the Home dashboard is one layout stored as the `dashboard` setting.
-- Carry over the first workspace's layout (widgets and their config) so nothing on Home is lost.
INSERT OR IGNORE INTO settings(key, value, updated_at)
SELECT 'dashboard', layout, updated_at FROM workspaces
WHERE json_valid(layout) ORDER BY position, created_at LIMIT 1;

DROP TABLE workspaces;

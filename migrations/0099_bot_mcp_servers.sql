-- Bots choose whole MCP servers (integrations), not individual tools: the allow-list entries
-- "<serverId>/<tool>" of config.mcpTools become the distinct server ids in config.mcpServers.
-- Per-tool control stays with the server's own tool permissions (Settings > Integrations).
UPDATE bots
SET config = json_set(
      json_remove(config, '$.mcpTools'),
      '$.mcpServers',
      json((SELECT COALESCE(json_group_array(server), '[]') FROM (
        SELECT DISTINCT substr(value, 1, instr(value, '/') - 1) AS server
        FROM json_each(bots.config, '$.mcpTools')
        WHERE instr(value, '/') > 1
        ORDER BY 1))))
WHERE json_valid(config) AND json_type(config, '$.mcpTools') = 'array';

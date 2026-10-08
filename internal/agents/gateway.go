package agents

// attachGateway gives a structured launch Orole's central skills and MCP servers: the stdio
// gateway ("orole") every structured harness must register, and the enabled-skills index for its
// system prompt. Env carries only the data directory and the session id; the gateway process
// authenticates to the daemon over the per-user pipe itself, so no secret is passed. Remote
// machines cannot reach the daemon's pipe and never get it.
func (m *Manager) attachGateway(spec *LaunchSpec) {
	h := m.cfg.Host
	if h == nil || h.GatewayExe == "" {
		return
	}
	env := map[string]string{"OROLE_GATEWAY_SESSION": spec.SessionID}
	if h.DataDir != "" {
		env["OROLE_HOME"] = h.DataDir
	}
	spec.Gateway = &Gateway{Command: h.GatewayExe, Args: []string{"mcp-gateway"}, Env: env}
	if m.cfg.SkillsPrompt != nil {
		spec.SkillsPrompt = m.cfg.SkillsPrompt()
	}
}

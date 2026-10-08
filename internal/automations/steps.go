package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rayorole/orole-os-desktop/internal/automations/expr"
	"net/http"
	"path/filepath"
	"strings"
)

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func stringsMap(v any) map[string]string {
	out := map[string]string{}
	m, _ := v.(map[string]any)
	for k, v := range m {
		s, _ := v.(string)
		out[k] = s
	}
	return out
}
func (e *Engine) invoke(ctx context.Context, r Run, s Step, p map[string]any, key string, env map[string]any, beforeDispatch func() error) (any, error) {
	e.deps.RLock()
	tools, agents, gh, notify, commands := e.tools, e.agents, e.github, e.notifier, e.commands
	e.deps.RUnlock()
	actor := "automation:" + r.AutomationID
	switch s.Type {
	case "command.run", "test.run":
		path, err := resolveCommand(str(p, "executable"), r.Definition.AllowedExecutables)
		if err != nil {
			return nil, err
		}
		dir := str(p, "dir")
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("explicit absolute working directory required")
		}
		a, _ := p["args"].([]any)
		args := make([]string, len(a))
		for i, v := range a {
			args[i], _ = v.(string)
		}
		request := CommandRequest{Executable: path, Args: args, Dir: dir, Env: stringsMap(p["env"])}
		if err := e.authorizeCommand(ctx, actor, s.Type, request); err != nil {
			return nil, err
		}
		if err := beforeDispatch(); err != nil {
			return nil, err
		}
		result, err := commands.RunCommand(ctx, request)
		result.Passed = err == nil && result.ExitCode == 0
		if err == nil && result.ExitCode != 0 {
			err = fmt.Errorf("command exited with code %d", result.ExitCode)
		}
		return result, err
	case "mcp.tool":
		if tools == nil {
			return nil, fmt.Errorf("MCP tool adapter is unavailable")
		}
		args, _ := p["args"].(map[string]any)
		return tools.InvokeTool(ctx, ToolRequest{Server: str(p, "server"), Tool: str(p, "tool"), Args: args, Key: key, Actor: actor, Chain: r.Chain})
	case "agent.start", "agent.prompt", "agent.wait":
		if agents == nil {
			return nil, fmt.Errorf("Agent control is unavailable")
		}
		req := AgentRequest{AgentID: str(p, "agentId"), ProjectID: str(p, "projectId"), Adapter: str(p, "adapter"), Prompt: str(p, "prompt"), Key: key, Actor: actor, Chain: r.Chain}
		switch s.Type {
		case "agent.start":
			return agents.StartAgent(ctx, req)
		case "agent.prompt":
			return agents.SendPrompt(ctx, req)
		default:
			return agents.WaitForAgent(ctx, req.AgentID)
		}
	case "github.query":
		if gh == nil {
			return nil, fmt.Errorf("GitHub query is unavailable")
		}
		limit, _ := p["limit"].(float64)
		if limit == 0 {
			limit = 30
		}
		return gh.QueryGitHub(ctx, QueryRequest{Kind: str(p, "kind"), Repo: str(p, "repo"), Query: str(p, "query"), Limit: int(limit)})
	case "notify":
		if notify == nil {
			return nil, fmt.Errorf("Notification adapter is unavailable")
		}
		return notify.Notify(ctx, NotificationRequest{Title: e.redact.Text(str(p, "title")), Body: e.redact.Text(str(p, "body")), Level: str(p, "level"), Source: "automation/" + r.AutomationID, Key: key})
	case "branch":
		ex, err := expr.Parse(str(p, "condition"))
		if err != nil {
			return nil, err
		}
		yes, err := ex.EvalBool(env)
		if err != nil {
			return nil, err
		}
		next := str(p, "else")
		if yes {
			next = str(p, "then")
		}
		return map[string]any{"condition": yes, "next": next}, nil
	case "http.request":
		headers := stringsMap(p["headers"])
		for name := range headers {
			if strings.EqualFold(name, "X-Orole-Chain") || strings.EqualFold(name, "Idempotency-Key") {
				delete(headers, name)
			}
		}
		if s.Strategy == "key" {
			headers["Idempotency-Key"] = key
		}
		chain, _ := json.Marshal(r.Chain)
		headers["X-Orole-Chain"] = string(chain)
		return e.http.do(ctx, HTTPPolicy{AllowedHosts: r.Definition.AllowedHosts, AllowHTTP: r.Definition.AllowHTTP, MaxBytes: 64 << 10}, HTTPRequest{Method: str(p, "method"), URL: str(p, "url"), Headers: headers, Body: str(p, "body")}, func(request *http.Request) error {
			if err := e.authorizeHTTP(request.Context(), actor, request.Method, request.URL.String()); err != nil {
				return err
			}
			return beforeDispatch()
		})
	}
	return nil, fmt.Errorf("unsupported step %q", s.Type)
}
func Templates() []Definition {
	return []Definition{
		Normalize(Definition{Name: "Notify me when CI fails", Description: "Checks a selected repository ref every 15 minutes. Fill in owner/repo and ref. A persistent failure notifies on each check.", Trigger: Trigger{Type: "schedule", Schedule: "every 15m", CatchUp: "skip"}, Steps: []Step{
			{ID: "checks", Type: "github.query", Params: json.RawMessage(`{"kind":"checks","repo":"","query":"main","limit":30}`)},
			{ID: "failed", Type: "branch", Params: json.RawMessage(`{"condition":"steps.checks.output.failed > 0","then":"notify","else":"end"}`)},
			{ID: "notify", Type: "notify", Params: json.RawMessage(`{"title":"CI failed","body":"{{ steps.checks.output.repo }}: {{ steps.checks.output.failed }} failed checks","level":"error"}`)},
		}}),
		Normalize(Definition{Name: "Morning GitHub issues", Trigger: Trigger{Type: "schedule", Schedule: "0 9 * * 1-5", CatchUp: "skip"}, Steps: []Step{{ID: "issues", Type: "github.query", Params: json.RawMessage(`{"kind":"issues","repo":"","limit":30}`)}, {ID: "notify", Type: "notify", Params: json.RawMessage(`{"title":"Morning GitHub issues","body":"{{ steps.issues.output }}","level":"info"}`)}}}),
		Normalize(Definition{Name: "Run tests after agent finishes", AllowedExecutables: []string{"go"}, Trigger: Trigger{Type: "event", Event: "agent.finished"}, Steps: []Step{{ID: "tests", Type: "test.run", Params: json.RawMessage(`{"executable":"go","args":["test","./..."],"dir":""}`)}}}),
	}
}

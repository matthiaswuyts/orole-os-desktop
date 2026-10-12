package daemon

import (
	"context"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

type mcpRef struct {
	ID string `json:"id"`
}
type mcpCall struct {
	ID        string         `json:"id"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	CallID    string         `json:"callId"`
	TimeoutMs int64          `json:"timeoutMs"`
}

func (d *Daemon) registerMCP() {
	d.Policy = policy.New(d.DB.DB, d.IPC, d.Log)
	d.MCP = mcp.New(mcp.Options{DB: d.DB.DB, Creds: credentials.New(), Pub: d.IPC, Policy: d.Policy, Events: d.Events, Log: d.Log, Version: Version})
	if d.Automations != nil {
		d.Automations.SetToolInvoker(automationMCP{d})
		d.Automations.SetWatchSource("mcp", mcpWatch{d: d})
	}
	handle(d, "mcp.servers.list", func(ctx context.Context, _ none) ([]mcp.ServerState, error) { return d.MCP.List(ctx) })
	handle(d, "mcp.servers.add", func(ctx context.Context, p mcp.AddParams) (mcp.ServerState, error) { return d.MCP.Add(ctx, p) })
	handle(d, "mcp.servers.update", func(ctx context.Context, p struct {
		ID     string           `json:"id"`
		Update mcp.UpdateParams `json:"update"`
	}) (mcp.ServerState, error) {
		return d.MCP.Update(ctx, p.ID, p.Update)
	})
	handle(d, "mcp.servers.remove", func(ctx context.Context, p mcpRef) (none, error) { return none{}, d.MCP.Remove(ctx, p.ID) })
	handle(d, "mcp.servers.enable", func(ctx context.Context, p struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}) (mcp.ServerState, error) {
		return d.MCP.SetEnabled(ctx, p.ID, p.Enabled)
	})
	handle(d, "mcp.connect", func(ctx context.Context, p mcpRef) (none, error) { return none{}, d.MCP.Connect(p.ID) })
	handle(d, "mcp.auth.discover", func(ctx context.Context, p mcpRef) (*mcp.Discovery, error) { return d.MCP.DiscoverAuth(ctx, p.ID) })
	handle(d, "mcp.auth.start", func(ctx context.Context, p mcpRef) (mcp.AuthStart, error) { return d.MCP.StartAuth(ctx, p.ID) })
	handle(d, "mcp.auth.cancel", func(ctx context.Context, p mcpRef) (none, error) { d.MCP.CancelAuth(p.ID); return none{}, nil })
	handle(d, "mcp.auth.signout", func(ctx context.Context, p mcpRef) (none, error) { return none{}, d.MCP.SignOut(ctx, p.ID) })
	handle(d, "mcp.tools", func(ctx context.Context, p mcpRef) ([]mcp.ToolInfo, error) { return d.MCP.Tools(ctx, p.ID) })
	handle(d, "mcp.catalog", func(ctx context.Context, p mcpRef) (*mcp.Catalog, error) { return d.MCP.Catalog(ctx, p.ID) })
	handle(d, "mcp.catalog.refresh", func(ctx context.Context, p mcpRef) (*mcp.Catalog, error) { return d.MCP.RefreshCatalog(ctx, p.ID) })
	handle(d, "mcp.diagnostics", func(ctx context.Context, p mcpRef) (mcp.Diagnostics, error) { return d.MCP.Diagnostics(ctx, p.ID) })
	handle(d, "mcp.call", func(ctx context.Context, p mcpCall) (*mcp.CallResult, error) {
		timeout := time.Duration(p.TimeoutMs) * time.Millisecond
		if timeout < 0 || timeout > 10*time.Minute {
			timeout = 60 * time.Second
		}
		return d.MCP.InvokeChecked(ctx, p.ID, p.Tool, p.Arguments, mcp.CallOptions{Actor: "user", CallID: p.CallID, Timeout: timeout})
	})
	handle(d, "mcp.cancel", func(ctx context.Context, p struct {
		CallID string `json:"callId"`
	}) (bool, error) {
		return d.MCP.CancelCall(p.CallID), nil
	})
	handle(d, "mcp.permissions.set", func(ctx context.Context, p struct {
		ID       string `json:"id"`
		Tool     string `json:"tool"`
		Decision string `json:"decision"`
	}) (none, error) {
		return none{}, d.MCP.SetToolPermission(ctx, p.ID, p.Tool, p.Decision)
	})
	handle(d, "policy.pending", func(ctx context.Context, _ none) ([]policy.Pending, error) { return d.Policy.Pending(), nil })
	handle(d, "policy.resolve", func(ctx context.Context, p struct {
		ID       string `json:"id"`
		Allow    bool   `json:"allow"`
		Remember bool   `json:"remember"`
	}) (none, error) {
		return none{}, d.Policy.Resolve(ctx, p.ID, policy.Resolution{Allow: p.Allow, Remember: p.Remember})
	})
	handle(d, "policy.rules.list", func(ctx context.Context, _ none) ([]policy.Rule, error) { return d.Policy.Rules(ctx) })
	handle(d, "policy.rules.add", func(ctx context.Context, p policy.Rule) (policy.Rule, error) { return d.Policy.AddRule(ctx, p) })
	handle(d, "policy.rules.delete", func(ctx context.Context, p struct {
		ID int64 `json:"id"`
	}) (none, error) {
		return none{}, d.Policy.DeleteRule(ctx, p.ID)
	})
	handle(d, "policy.decide", func(ctx context.Context, p policy.Request) (policy.Evaluation, error) { return d.Policy.Decide(ctx, p) })
}

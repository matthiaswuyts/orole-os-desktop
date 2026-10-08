package daemon

import (
	"context"
	"fmt"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
)

type automationMCP struct{ d *Daemon }

func (a automationMCP) InvokeTool(ctx context.Context, p automations.ToolRequest) (any, error) {
	// MCP has no standard idempotency key: never claim durable deduplication.
	ctx = events.WithChain(ctx, p.Chain)
	out, err := a.d.MCP.InvokeChecked(ctx, p.Server, p.Tool, p.Args, mcp.CallOptions{Actor: p.Actor})
	if err == nil && out.IsError {
		err = fmt.Errorf("MCP tool %s reported failure", p.Tool)
	}
	return out, err
}

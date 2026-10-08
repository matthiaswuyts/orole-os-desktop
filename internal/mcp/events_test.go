package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
)

func TestToolEventPreservesProvenance(t *testing.T) {
	h := newHarness(t)
	h.m.events = events.NewBus()
	sub := h.m.events.Subscribe("mcp.tool.called", 4)
	defer sub.Close()
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	ctx := events.WithChain(context.Background(), []string{"automation-a"})
	if _, err := h.m.CallTool(ctx, st.ID, "echo", map[string]any{"message": "hello"}, CallOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-sub.C:
		if len(ev.Chain) != 1 || ev.Chain[0] != "automation-a" || ev.Data["tool"] != "echo" || ev.Data["success"] != true {
			t.Fatalf("event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no tool event")
	}
}

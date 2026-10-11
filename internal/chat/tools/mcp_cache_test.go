package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
)

func specsOf(t *testing.T, tool *MCPTools, servers ...string) []llm.ToolSpec {
	t.Helper()
	return tool.Specs(mcpBot("b", servers...), llm.Caps{Tools: true})
}

func TestMCPSpecsAreCachedUntilTheGenerationMoves(t *testing.T) {
	f := &fakeMCP{
		servers:  []mcp.ServerState{server("s", mcp.StatusConnected, true)},
		catalogs: map[string][]mcp.ToolInfo{"s": {{Name: "a", Description: "first", InputSchema: json.RawMessage(objSchema)}, {Name: "b"}}},
	}
	tool := &MCPTools{Client: f}

	first := specsOf(t, tool, "s")
	if len(first) != 2 {
		t.Fatalf("specs = %v", specNames(first))
	}
	for range 5 {
		if got := specsOf(t, tool, "s"); len(got) != 2 || got[0].Description != first[0].Description {
			t.Fatalf("cached specs = %+v", got)
		}
	}
	if l, c := f.listCalls.Load(), f.catalogCalls.Load(); l != 1 || c != 1 {
		t.Fatalf("List/Catalog calls = %d/%d after 6 Specs, want 1/1", l, c)
	}

	// A different bot given the same server reuses the same generation's lookups.
	if got := tool.Specs(mcpBot("other", "s"), llm.Caps{Tools: true}); len(got) != 2 || f.listCalls.Load() != 1 || f.catalogCalls.Load() != 1 {
		t.Fatalf("second bot: specs=%v list=%d catalog=%d", specNames(got), f.listCalls.Load(), f.catalogCalls.Load())
	}

	// Without a generation change the cache is, by contract, trusted.
	f.mu.Lock()
	f.catalogs["s"] = []mcp.ToolInfo{{Name: "a", Description: "second", InputSchema: json.RawMessage(objSchema)}}
	f.mu.Unlock()
	if got := specsOf(t, tool, "s"); !strings.Contains(got[0].Description, "first") {
		t.Fatalf("cache dropped without a generation change: %q", got[0].Description)
	}
}

// A tool the user set to "deny" is not offered; the cache must follow permission writes even though
// the generation does not move for them.
func TestMCPSpecsFollowToolPermissions(t *testing.T) {
	f := &fakeMCP{
		servers:  []mcp.ServerState{server("s", mcp.StatusConnected, true)},
		catalogs: map[string][]mcp.ToolInfo{"s": {{Name: "a"}, {Name: "b"}}},
	}
	tool := &MCPTools{Client: f}
	if got := specsOf(t, tool, "s"); len(got) != 2 {
		t.Fatalf("initial specs = %v", specNames(got))
	}
	f.changePermissions(func() { f.catalogs["s"] = []mcp.ToolInfo{{Name: "a", Permission: "deny"}, {Name: "b"}} })
	got := specsOf(t, tool, "s")
	if len(got) != 1 || !strings.HasSuffix(got[0].Name, "__b") {
		t.Fatalf("specs after deny = %v, want only b", specNames(got))
	}
	f.changePermissions(func() { f.catalogs["s"] = []mcp.ToolInfo{{Name: "a"}, {Name: "b"}} })
	if got := specsOf(t, tool, "s"); len(got) != 2 {
		t.Fatalf("specs after allow = %v, want both", specNames(got))
	}
	// Call resolves tools from the same cache, so a denied tool is refused without reaching the server.
	f.changePermissions(func() { f.catalogs["s"] = []mcp.ToolInfo{{Name: "a", Permission: "deny"}, {Name: "b"}} })
	name := "mcp__s__a"
	res, err := tool.Call(context.Background(), chat.RunContext{Bot: mcpBot("b", "s"), BotID: "b"}, name, json.RawMessage(`{}`))
	if err != nil || !res.IsError || len(f.calls()) != 0 {
		t.Fatalf("denied tool: res=%+v err=%v invoked=%d", res, err, len(f.calls()))
	}
}

func TestMCPSpecsInvalidation(t *testing.T) {
	newFake := func() *fakeMCP {
		return &fakeMCP{
			servers:  []mcp.ServerState{server("s", mcp.StatusConnected, true)},
			catalogs: map[string][]mcp.ToolInfo{"s": {{Name: "a", Description: "first"}}},
		}
	}
	cases := []struct {
		name   string
		change func(f *fakeMCP)
		check  func(t *testing.T, got []llm.ToolSpec)
	}{
		{"catalog changed", func(f *fakeMCP) {
			f.catalogs["s"] = []mcp.ToolInfo{{Name: "a", Description: "second"}}
		}, func(t *testing.T, got []llm.ToolSpec) {
			if len(got) != 1 || !strings.Contains(got[0].Description, "second") {
				t.Fatalf("specs = %+v, want the new description", got)
			}
		}},
		{"tool removed from catalog", func(f *fakeMCP) { f.catalogs["s"] = nil }, func(t *testing.T, got []llm.ToolSpec) {
			if len(got) != 0 {
				t.Fatalf("specs = %v, want none", specNames(got))
			}
		}},
		{"server lost connection", func(f *fakeMCP) { f.servers[0].Status = mcp.StatusBackoff }, func(t *testing.T, got []llm.ToolSpec) {
			if len(got) != 0 {
				t.Fatalf("specs = %v, want none while the server is down", specNames(got))
			}
		}},
		{"server disabled", func(f *fakeMCP) { f.servers[0].Enabled = false }, func(t *testing.T, got []llm.ToolSpec) {
			if len(got) != 0 {
				t.Fatalf("specs = %v, want none for a disabled server", specNames(got))
			}
		}},
		{"server removed", func(f *fakeMCP) { f.servers = nil; delete(f.catalogs, "s") }, func(t *testing.T, got []llm.ToolSpec) {
			if len(got) != 0 {
				t.Fatalf("specs = %v, want none for a removed server", specNames(got))
			}
		}},
		{"server renamed", func(f *fakeMCP) { f.servers[0].Name = "Renamed" }, func(t *testing.T, got []llm.ToolSpec) {
			if len(got) != 1 || !strings.Contains(got[0].Description, "Renamed") {
				t.Fatalf("specs = %+v, want the new server name", got)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			tool := &MCPTools{Client: f}
			if got := specsOf(t, tool, "s"); len(got) != 1 {
				t.Fatalf("initial specs = %v", specNames(got))
			}
			f.change(func() { tc.change(f) })
			tc.check(t, specsOf(t, tool, "s"))
		})
	}
}

func TestMCPSpecsReappearWhenServerReconnects(t *testing.T) {
	f := &fakeMCP{
		servers:  []mcp.ServerState{server("s", mcp.StatusBackoff, true)},
		catalogs: map[string][]mcp.ToolInfo{"s": {{Name: "a"}}},
	}
	tool := &MCPTools{Client: f}
	if got := specsOf(t, tool, "s"); len(got) != 0 {
		t.Fatalf("specs while down = %v", specNames(got))
	}
	f.change(func() { f.servers[0].Status = mcp.StatusConnected })
	if got := specsOf(t, tool, "s"); len(got) != 1 {
		t.Fatalf("specs after reconnect = %v", specNames(got))
	}
}

// failingCatalog fails the first catalog lookups; a failure must not be remembered.
type failingCatalog struct {
	*fakeMCP
	failMu sync.Mutex
	fails  int
}

func (f *failingCatalog) Catalog(ctx context.Context, id string) (*mcp.Catalog, error) {
	f.failMu.Lock()
	if f.fails > 0 {
		f.fails--
		f.failMu.Unlock()
		return nil, errors.New("database is locked")
	}
	f.failMu.Unlock()
	return f.fakeMCP.Catalog(ctx, id)
}

func TestMCPSpecsDoNotCacheLookupFailures(t *testing.T) {
	f := &failingCatalog{fails: 1, fakeMCP: &fakeMCP{
		servers:  []mcp.ServerState{server("s", mcp.StatusConnected, true)},
		catalogs: map[string][]mcp.ToolInfo{"s": {{Name: "a"}}},
	}}
	tool := &MCPTools{Client: f}
	if got := specsOf(t, tool, "s"); len(got) != 0 {
		t.Fatalf("specs during a failed lookup = %v", specNames(got))
	}
	if got := specsOf(t, tool, "s"); len(got) != 1 {
		t.Fatalf("specs after the failure cleared = %v (a transient error was cached)", specNames(got))
	}
}

func TestMCPSpecsConcurrentWithChanges(t *testing.T) {
	f := &fakeMCP{
		servers:  []mcp.ServerState{server("s", mcp.StatusConnected, true)},
		catalogs: map[string][]mcp.ToolInfo{"s": {{Name: "a"}}},
	}
	tool := &MCPTools{Client: f}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				specsOf(t, tool, "s")
			}
		}()
	}
	for i := range 50 {
		f.change(func() { f.servers[0].LastError = strings.Repeat("x", i) })
	}
	wg.Wait()
}

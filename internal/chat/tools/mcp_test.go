package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

// fakeMCP is an MCPClient. Only InvokeChecked can run a tool, mirroring the real Manager's surface
// that MCPTools is allowed to use.
type fakeMCP struct {
	mu       sync.Mutex
	servers  []mcp.ServerState
	catalogs map[string][]mcp.ToolInfo
	invokeFn func(ctx context.Context, id, tool string, args map[string]any, opts mcp.CallOptions) (*mcp.CallResult, error)
	invoked  []invocation

	gen          atomic.Uint64
	perm         atomic.Uint64
	listCalls    atomic.Int32
	catalogCalls atomic.Int32
}

type invocation struct {
	Server, Tool string
	Args         map[string]any
	Opts         mcp.CallOptions
}

func (f *fakeMCP) List(context.Context) ([]mcp.ServerState, error) {
	f.listCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mcp.ServerState(nil), f.servers...), nil
}

func (f *fakeMCP) Catalog(_ context.Context, id string) (*mcp.Catalog, error) {
	f.catalogCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	tools, ok := f.catalogs[id]
	if !ok {
		return nil, mcp.ErrNotFound
	}
	return &mcp.Catalog{ServerID: id, Tools: tools}, nil
}

func (f *fakeMCP) Generation() uint64        { return f.gen.Load() }
func (f *fakeMCP) PermissionVersion() uint64 { return f.perm.Load() }

// change mutates the fake's state the way a real change does: state first, then the generation.
func (f *fakeMCP) change(fn func()) {
	f.mu.Lock()
	fn()
	f.mu.Unlock()
	f.gen.Add(1)
}

// changePermissions mutates the fake's state the way a per-tool permission write does: state first,
// then the permission version (the generation does not move).
func (f *fakeMCP) changePermissions(fn func()) {
	f.mu.Lock()
	fn()
	f.mu.Unlock()
	f.perm.Add(1)
}

func (f *fakeMCP) InvokeChecked(ctx context.Context, id, tool string, args map[string]any, opts mcp.CallOptions) (*mcp.CallResult, error) {
	f.mu.Lock()
	f.invoked = append(f.invoked, invocation{id, tool, args, opts})
	fn := f.invokeFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, id, tool, args, opts)
	}
	return &mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: "ok"}}}, nil
}

func (f *fakeMCP) calls() []invocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]invocation(nil), f.invoked...)
}

func server(id string, status mcp.Status, enabled bool) mcp.ServerState {
	return mcp.ServerState{ServerConfig: mcp.ServerConfig{ID: id, Name: "Server " + id, Enabled: enabled}, Status: status}
}

func mcpBot(id string, servers ...string) chat.Bot {
	b := chat.Bot{ID: id}
	b.Config.MCPServers = servers
	return b
}

// liveSearch is a connected server "live" that offers one tool, "search".
func liveSearch(f *fakeMCP) *fakeMCP {
	f.servers = []mcp.ServerState{server("live", mcp.StatusConnected, true)}
	f.catalogs = map[string][]mcp.ToolInfo{"live": {{Name: "search", Description: "Find things"}}}
	return f
}

func specNames(specs []llm.ToolSpec) []string {
	var out []string
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

const objSchema = `{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`

func TestMCPSpecsAreTheLiveServersToolsMinusDeniedOnes(t *testing.T) {
	f := &fakeMCP{
		servers: []mcp.ServerState{
			server("live", mcp.StatusConnected, true),
			server("down", mcp.StatusBackoff, true),
			server("off", mcp.StatusConnected, false),
		},
		catalogs: map[string][]mcp.ToolInfo{
			"live": {{Name: "search", Description: "Find things", InputSchema: json.RawMessage(objSchema)}, {Name: "secret", Description: "denied by the user", Permission: "deny"}},
			"down": {{Name: "t"}},
			"off":  {{Name: "t"}},
		},
	}
	tool := &MCPTools{Client: f}
	bot := mcpBot("b1", "live", "down", "off", "gone")
	specs := tool.Specs(bot, llm.Caps{Tools: true})
	if got := specNames(specs); len(got) != 1 || got[0] != "mcp__live__search" {
		t.Fatalf("spec names = %v, want only mcp__live__search", got)
	}
	if string(specs[0].Schema) != objSchema {
		t.Fatalf("schema = %s, want the server's input schema", specs[0].Schema)
	}
	if !strings.Contains(specs[0].Description, "Find things") {
		t.Fatalf("description = %q", specs[0].Description)
	}
	if got := tool.Specs(bot, llm.Caps{}); got != nil {
		t.Fatalf("specs for a model without tools = %v", got)
	}
	if got := tool.Specs(mcpBot("b2"), llm.Caps{Tools: true}); got != nil {
		t.Fatalf("specs for a bot with no integrations = %v", got)
	}
	if got := (&MCPTools{}).Specs(bot, llm.Caps{Tools: true}); got != nil {
		t.Fatalf("specs without a client = %v", got)
	}
}

func TestMCPSchemaFallsBackToObject(t *testing.T) {
	f := &fakeMCP{
		servers:  []mcp.ServerState{server("s", mcp.StatusConnected, true)},
		catalogs: map[string][]mcp.ToolInfo{"s": {{Name: "a", InputSchema: json.RawMessage(`{"type":"string"}`)}, {Name: "b"}}},
	}
	specs := (&MCPTools{Client: f}).Specs(mcpBot("b", "s"), llm.Caps{Tools: true})
	if len(specs) != 2 {
		t.Fatalf("specs = %v", specNames(specs))
	}
	for _, s := range specs {
		if !strings.Contains(string(s.Schema), `"type":"object"`) {
			t.Errorf("%s schema = %s, want an object schema", s.Name, s.Schema)
		}
	}
}

func TestMCPNamesSanitisedBoundedAndCollisionSafe(t *testing.T) {
	long := strings.Repeat("x", 80)
	var entries []mcpEntry
	for _, a := range []string{
		"my.server/do thing", // sanitised to my_server / do_thing
		"my_server/do_thing", // collides with the above after sanitising
		"s/" + long,
		"s/" + long + "y", // shares the 64-char prefix with the previous one
		"s/plain",
	} {
		server, tool, _ := strings.Cut(a, "/")
		entries = append(entries, mcpEntry{Server: server, Tool: tool})
	}
	nameEntries(entries)
	seen := map[string]bool{}
	for _, e := range entries {
		if len(e.Name) > maxFunctionLen {
			t.Errorf("%q is %d chars, max %d", e.Name, len(e.Name), maxFunctionLen)
		}
		if !strings.HasPrefix(e.Name, "mcp__") {
			t.Errorf("%q lacks the mcp__ prefix", e.Name)
		}
		for _, r := range e.Name {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				t.Errorf("%q contains %q", e.Name, r)
			}
		}
		if seen[e.Name] {
			t.Errorf("%q is not unique", e.Name)
		}
		seen[e.Name] = true
	}
	if entries[4].Name != "mcp__s__plain" {
		t.Errorf("an uncontended name must stay readable, got %q", entries[4].Name)
	}
	again := make([]mcpEntry, len(entries))
	for i, e := range entries {
		again[i] = mcpEntry{Server: e.Server, Tool: e.Tool}
	}
	nameEntries(again)
	for i := range entries {
		if entries[i].Name != again[i].Name {
			t.Fatalf("mapping is not deterministic: %v vs %v", entries[i], again[i])
		}
	}
}

func TestMCPCallInvokesCheckedWithBotActor(t *testing.T) {
	f := liveSearch(&fakeMCP{
		invokeFn: func(context.Context, string, string, map[string]any, mcp.CallOptions) (*mcp.CallResult, error) {
			return &mcp.CallResult{Content: []mcp.ContentBlock{
				{Type: "text", Text: "hello \x1b[31mworld\x1b[0m"},
				{Type: "image", Bytes: 1234},
				{Type: "resource_link", URI: "https://example.com/x"},
			}}, nil
		},
	})
	tool := &MCPTools{Client: f}
	bot := mcpBot("b1", "live")
	rc := chat.RunContext{BotID: "b1", Bot: bot, Actor: "bot:b1"}
	res, err := tool.Call(context.Background(), rc, "mcp__live__search", json.RawMessage(`{"q":"x"}`))
	if err != nil || res.IsError {
		t.Fatalf("call = %+v, %v", res, err)
	}
	if !res.Untrusted {
		t.Fatal("MCP results must be untrusted")
	}
	if strings.Contains(res.Text, "\x1b") {
		t.Fatalf("result still holds escape sequences: %q", res.Text)
	}
	for _, want := range []string{"hello", "image content, 1234 bytes, not shown", "https://example.com/x"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("result %q lacks %q", res.Text, want)
		}
	}
	calls := f.calls()
	if len(calls) != 1 {
		t.Fatalf("invocations = %d", len(calls))
	}
	c := calls[0]
	if c.Server != "live" || c.Tool != "search" || c.Opts.Actor != "bot:b1" || c.Args["q"] != "x" {
		t.Fatalf("invocation = %+v", c)
	}

	// An empty Actor is derived from the bot id; the actor is never empty.
	if _, err := tool.Call(context.Background(), chat.RunContext{BotID: "b1", Bot: bot}, "mcp__live__search", nil); err != nil {
		t.Fatal(err)
	}
	if got := f.calls()[1].Opts.Actor; got != "bot:b1" {
		t.Fatalf("derived actor = %q", got)
	}
}

func TestMCPCallRejectsToolsOutsideTheBotsServers(t *testing.T) {
	f := liveSearch(&fakeMCP{})
	f.catalogs["other"] = []mcp.ToolInfo{{Name: "search"}}
	f.servers = append(f.servers, server("other", mcp.StatusConnected, true))
	tool := &MCPTools{Client: f}
	rc := chat.RunContext{BotID: "b1", Bot: mcpBot("b1", "live"), Actor: "bot:b1"}
	for _, name := range []string{"mcp__live__other", "mcp__other__search", "search", ""} {
		res, err := tool.Call(context.Background(), rc, name, json.RawMessage(`{}`))
		if err != nil || !res.IsError {
			t.Errorf("%q: got %+v, %v; want an error result", name, res, err)
		}
	}
	// Another bot's servers do not authorise this bot's tool names either.
	other := chat.RunContext{BotID: "b2", Bot: mcpBot("b2", "other"), Actor: "bot:b2"}
	if res, _ := tool.Call(context.Background(), other, "mcp__live__search", nil); !res.IsError {
		t.Error("a tool of a server the calling bot was not given ran")
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("%d tool calls reached the manager", n)
	}
}

func TestMCPCallBadArguments(t *testing.T) {
	f := liveSearch(&fakeMCP{})
	tool := &MCPTools{Client: f}
	rc := chat.RunContext{BotID: "b1", Bot: mcpBot("b1", "live"), Actor: "bot:b1"}
	for _, args := range []string{`[1,2]`, `"text"`, `{`} {
		res, err := tool.Call(context.Background(), rc, "mcp__live__search", json.RawMessage(args))
		if err != nil || !res.IsError {
			t.Errorf("args %s: got %+v, %v; want an error result", args, res, err)
		}
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("%d calls ran with invalid arguments", n)
	}
}

func TestMCPCallPolicyOutcomes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"denied", fmt.Errorf("mcp: %w", policy.ErrDenied), "not approved"},
		{"approval timeout", policy.ErrApprovalTimeout, "not approved"},
		{"disconnected", mcp.ErrNotConnected, "not available"},
		{"disabled", mcp.ErrDisabled, "not available"},
		{"vanished tool", mcp.ErrNoSuchTool, "not available"},
		{"other failure", errors.New("boom"), "tool call failed: boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := liveSearch(&fakeMCP{invokeFn: func(context.Context, string, string, map[string]any, mcp.CallOptions) (*mcp.CallResult, error) {
				return nil, tc.err
			}})
			rc := chat.RunContext{BotID: "b1", Bot: mcpBot("b1", "live"), Actor: "bot:b1"}
			res, err := (&MCPTools{Client: f}).Call(context.Background(), rc, "mcp__live__search", json.RawMessage(`{}`))
			if err != nil {
				t.Fatalf("a policy/availability outcome must be an error result, got Go error %v", err)
			}
			if !res.IsError || !strings.Contains(res.Text, tc.want) {
				t.Fatalf("result = %+v, want error containing %q", res, tc.want)
			}
			if res.Untrusted {
				t.Error("daemon-authored errors must not be fenced as untrusted content")
			}
		})
	}
}

func TestMCPCallHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := liveSearch(&fakeMCP{invokeFn: func(ctx context.Context, _, _ string, _ map[string]any, _ mcp.CallOptions) (*mcp.CallResult, error) {
		cancel()
		return nil, ctx.Err()
	}})
	rc := chat.RunContext{BotID: "b1", Bot: mcpBot("b1", "live"), Actor: "bot:b1"}
	_, err := (&MCPTools{Client: f}).Call(ctx, rc, "mcp__live__search", json.RawMessage(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestMCPResultTextFallbacks(t *testing.T) {
	if got := resultText(&mcp.CallResult{}); got != "(the tool returned no content)" {
		t.Errorf("empty result = %q", got)
	}
	got := resultText(&mcp.CallResult{Structured: json.RawMessage(`{"n":1}`)})
	if got != `{"n":1}` {
		t.Errorf("structured-only result = %q", got)
	}
}

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

const (
	mcpToolPrefix  = "mcp__"
	maxFunctionLen = 64
	maxMCPResult   = 64 << 10
	mcpLookupLimit = 5 * time.Second
)

// MCPClient is the part of *mcp.Manager the bot MCP tools use.
type MCPClient interface {
	List(ctx context.Context) ([]mcp.ServerState, error)
	Catalog(ctx context.Context, id string) (*mcp.Catalog, error)
	InvokeChecked(ctx context.Context, id, tool string, args map[string]any, opts mcp.CallOptions) (*mcp.CallResult, error)
	// Generation changes whenever the server list, a server's status or a catalog changes; the
	// specs derived from them are cached until it does.
	Generation() uint64
}

// MCPTools exposes the MCP tools a bot's allow-list names (BotConfig.MCPTools, "<serverId>/<tool>").
// Calls go through mcp.Manager.InvokeChecked with the actor "bot:<id>", so Orole's policy engine
// decides every call and approvals appear in the normal approval UI; a denied call never reaches
// the server. Results are untrusted.
type MCPTools struct {
	Client MCPClient

	mu       sync.Mutex // guards the per-generation cache below
	gen      uint64
	have     bool
	live     map[string]mcp.ServerState // enabled, connected servers; nil until listed for gen
	cats     map[string]*mcp.Catalog
	resolved map[mcpKey]*mcpResolved // nil value: the tool is not in the server's catalog
}

type mcpKey struct{ server, tool string }

// mcpResolved is what a model is told about one tool, derived once per generation.
type mcpResolved struct {
	description string
	schema      json.RawMessage
}

// mcpEntry is one allow-list entry with its stable function name.
type mcpEntry struct {
	Server, Tool string
	Name         string
}

func (*MCPTools) ID() string { return "mcp" }

// mcpNames maps the allow-list to function names ("mcp__<server>__<tool>"). The mapping
// depends on the list alone, so Specs and Call always agree. Names are limited to [A-Za-z0-9_-] and
// 64 characters; names that would collide (or were shortened) get a short hash of "server/tool".
func mcpNames(allow []string) []mcpEntry {
	var entries []mcpEntry
	seen := map[string]bool{}
	for _, a := range allow {
		server, tool, ok := strings.Cut(a, "/")
		if !ok || server == "" || tool == "" || seen[a] {
			continue
		}
		seen[a] = true
		entries = append(entries, mcpEntry{Server: server, Tool: tool})
	}
	base := make([]string, len(entries))
	count := map[string]int{}
	for i, e := range entries {
		base[i] = mcpToolPrefix + sanitizeName(e.Server) + "__" + sanitizeName(e.Tool)
		count[base[i]]++
	}
	used := map[string]bool{}
	for i := range entries {
		name := base[i]
		if count[name] > 1 || len(name) > maxFunctionLen {
			sum := sha256.Sum256([]byte(entries[i].Server + "/" + entries[i].Tool))
			suffix := "_" + hex.EncodeToString(sum[:4])
			name = name[:min(len(name), maxFunctionLen-len(suffix))] + suffix
		}
		// A shortened name can still land on another entry's plain name; extend the hash.
		for n := 8; used[name]; n += 2 {
			sum := sha256.Sum256([]byte(entries[i].Server + "/" + entries[i].Tool + "#" + name))
			suffix := "_" + hex.EncodeToString(sum[:n/2])
			name = name[:min(len(name), maxFunctionLen-len(suffix))] + suffix
		}
		used[name] = true
		entries[i].Name = name
	}
	return entries
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func (t *MCPTools) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if t.Client == nil || !caps.Tools || len(bot.Config.MCPTools) == 0 {
		return nil
	}
	entries := mcpNames(bot.Config.MCPTools)
	if len(entries) == 0 {
		return nil
	}
	// Specs has no context; the lookups read cached state and must never hang a run.
	ctx, cancel := context.WithTimeout(context.Background(), mcpLookupLimit)
	defer cancel()
	t.mu.Lock()
	defer t.mu.Unlock()
	// Read the generation first: a change during the lookups below leaves the cache stale-marked.
	if gen := t.Client.Generation(); !t.have || gen != t.gen {
		t.gen, t.have = gen, true
		t.live, t.cats, t.resolved = nil, map[string]*mcp.Catalog{}, map[mcpKey]*mcpResolved{}
	}
	if t.live == nil {
		servers, err := t.Client.List(ctx)
		if err != nil {
			return nil
		}
		t.live = map[string]mcp.ServerState{}
		for _, s := range servers {
			if s.Enabled && s.Status == mcp.StatusConnected {
				t.live[s.ID] = s
			}
		}
	}
	var specs []llm.ToolSpec
	for _, e := range entries {
		srv, ok := t.live[e.Server]
		if !ok {
			continue
		}
		key := mcpKey{e.Server, e.Tool}
		r, seen := t.resolved[key]
		if !seen {
			cat, ok := t.cats[e.Server]
			if !ok {
				var err error
				if cat, err = t.Client.Catalog(ctx, e.Server); err != nil {
					continue // not cached: retried on the next call
				}
				t.cats[e.Server] = cat
			}
			if cat != nil {
				for i := range cat.Tools {
					if ti := &cat.Tools[i]; ti.Name == e.Tool {
						r = &mcpResolved{description: mcpDescription(srv.Name, ti), schema: objectSchema(ti.InputSchema)}
						break
					}
				}
			}
			t.resolved[key] = r
		}
		if r != nil {
			specs = append(specs, llm.ToolSpec{Name: e.Name, Description: r.description, Schema: r.schema})
		}
	}
	return specs
}

func mcpDescription(server string, ti *mcp.ToolInfo) string {
	d := ti.Description
	if d == "" {
		d = ti.Title
	}
	d = strings.Join(strings.Fields(untrusted.Line(d, 1000)), " ")
	return "[" + untrusted.Line(server, 60) + " MCP] " + d
}

// objectSchema returns schema when it is a JSON object schema, else an empty object schema.
func objectSchema(schema json.RawMessage) json.RawMessage {
	var probe struct {
		Type string `json:"type"`
	}
	if len(schema) > 0 && json.Unmarshal(schema, &probe) == nil && probe.Type == "object" {
		return schema
	}
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *MCPTools) Call(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if t.Client == nil {
		return errResult("MCP is not available")
	}
	var entry *mcpEntry
	entries := mcpNames(rc.Bot.Config.MCPTools)
	for i := range entries {
		if entries[i].Name == name {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return errResult("this bot is not allowed to use that tool")
	}
	var argMap map[string]any
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &argMap); err != nil {
			return errResult("invalid arguments: the tool takes a JSON object")
		}
	}
	if argMap == nil {
		argMap = map[string]any{}
	}
	actor := rc.Actor
	if actor == "" {
		actor = "bot:" + rc.BotID
	}
	res, err := t.Client.InvokeChecked(ctx, entry.Server, entry.Tool, argMap, mcp.CallOptions{Actor: actor})
	if err != nil {
		if ctx.Err() != nil {
			return chat.ToolResult{}, ctx.Err()
		}
		switch {
		case errors.Is(err, policy.ErrDenied), errors.Is(err, policy.ErrApprovalTimeout):
			return errResult("This tool call was not approved, so it did not run.")
		case errors.Is(err, mcp.ErrNotConnected), errors.Is(err, mcp.ErrDisabled), errors.Is(err, mcp.ErrNotFound), errors.Is(err, mcp.ErrNoSuchTool):
			return errResult("The tool's server is not available right now.")
		}
		return errResult("tool call failed: %s", untrusted.Line(err.Error(), 300))
	}
	return chat.ToolResult{Text: resultText(res), IsError: res.IsError, Untrusted: true}, nil
}

// resultText flattens an MCP result. Binary content is summarised, never forwarded.
func resultText(res *mcp.CallResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		switch c.Type {
		case "text":
			b.WriteString(c.Text)
		case "resource_link":
			fmt.Fprintf(&b, "[resource link: %s]", c.URI)
		default:
			fmt.Fprintf(&b, "[%s content, %d bytes, not shown]", c.Type, c.Bytes)
		}
		b.WriteByte('\n')
	}
	if len(res.Structured) > 0 && b.Len() == 0 {
		b.Write(res.Structured)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = "(the tool returned no content)"
	}
	return untrusted.Text(out, maxMCPResult)
}

package agents

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Claude Code answers the stream-json control request get_context_usage with the same
// per-category accounting its /context command prints. "summary" uses the last response's
// usage plus local estimates (no token-count API calls), so it is cheap enough per turn.
// The request is fire-and-forget from the stdout reader: the reply arrives on that same
// reader and is decoded there, so no goroutine can outlive the process's event channel.
const claudeContextRequestPrefix = "orole-context-"

// claudeContextItemLimit bounds the contributors kept per category; the rest are folded into
// one "N more" row so a session with hundreds of MCP tools or skills stays a small event.
const claudeContextItemLimit = 12

type claudeNamedTokens struct {
	Name   string `json:"name"`
	Tokens int64  `json:"tokens"`
}

type claudeContextUsage struct {
	Categories []struct {
		Name       string `json:"name"`
		Tokens     int64  `json:"tokens"`
		Kind       string `json:"kind"`
		IsDeferred bool   `json:"isDeferred"`
	} `json:"categories"`
	TotalTokens          *int64 `json:"totalTokens"`
	MaxTokens            *int64 `json:"maxTokens"`
	RawMaxTokens         *int64 `json:"rawMaxTokens"`
	Model                string `json:"model"`
	AutoCompactThreshold *int64 `json:"autoCompactThreshold"`
	IsAutoCompactEnabled bool   `json:"isAutoCompactEnabled"`
	MemoryFiles          []struct {
		Path   string `json:"path"`
		Type   string `json:"type"`
		Tokens int64  `json:"tokens"`
	} `json:"memoryFiles"`
	MCPTools []struct {
		Name       string `json:"name"`
		ServerName string `json:"serverName"`
		Tokens     int64  `json:"tokens"`
		IsLoaded   *bool  `json:"isLoaded"`
	} `json:"mcpTools"`
	DeferredBuiltinTools []claudeNamedTokens `json:"deferredBuiltinTools"`
	SystemTools          []claudeNamedTokens `json:"systemTools"`
	SystemPromptSections []claudeNamedTokens `json:"systemPromptSections"`
	Agents               []struct {
		AgentType string `json:"agentType"`
		Source    string `json:"source"`
		Tokens    int64  `json:"tokens"`
	} `json:"agents"`
	Skills *struct {
		SkillFrontmatter []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
			Tokens int64  `json:"tokens"`
		} `json:"skillFrontmatter"`
	} `json:"skills"`
	MessageBreakdown *struct {
		ToolCallTokens          int64 `json:"toolCallTokens"`
		ToolResultTokens        int64 `json:"toolResultTokens"`
		AttachmentTokens        int64 `json:"attachmentTokens"`
		AssistantMessageTokens  int64 `json:"assistantMessageTokens"`
		UserMessageTokens       int64 `json:"userMessageTokens"`
		RedirectedContextTokens int64 `json:"redirectedContextTokens"`
		UnattributedTokens      int64 `json:"unattributedTokens"`
	} `json:"messageBreakdown"`
}

// requestContext asks Claude Code for its context breakdown. Errors are ignored: an older
// CLI without get_context_usage just leaves the breakdown unreported.
func (d *claudeDecoder) requestContext(p *pipeProc) {
	if p == nil {
		return
	}
	d.contextSent++
	b, _ := json.Marshal(map[string]any{"type": "control_request",
		"request_id": claudeContextRequestPrefix + strconv.FormatUint(d.contextSent, 10),
		"request":    map[string]any{"subtype": "get_context_usage", "detail": "summary"}})
	_ = p.write(append(b, '\n'))
}

// contextResponse decodes a reply to requestContext. ok is false for any other line. Replies
// older than one already applied are dropped, so a slow answer cannot overwrite a newer one.
func (d *claudeDecoder) contextResponse(line []byte) (events []Event, ok bool) {
	if !bytes.Contains(line, []byte(claudeContextRequestPrefix)) {
		return nil, false
	}
	var r struct {
		Type     string `json:"type"`
		Response struct {
			Subtype  string          `json:"subtype"`
			ID       string          `json:"request_id"`
			Response json.RawMessage `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &r) != nil || r.Type != "control_response" {
		return nil, false
	}
	seq, found := strings.CutPrefix(r.Response.ID, claudeContextRequestPrefix)
	if !found {
		return nil, false
	}
	n, err := strconv.ParseUint(seq, 10, 64)
	if err != nil || n <= d.contextSeen || r.Response.Subtype != "success" {
		return nil, true
	}
	var u claudeContextUsage
	if json.Unmarshal(r.Response.Response, &u) != nil {
		return nil, true
	}
	breakdown := claudeBreakdown(u)
	if breakdown == nil {
		return nil, true
	}
	d.contextSeen = n
	return usageEvent(ContextUsage{Source: "claude:get_context_usage", ContextTokens: &breakdown.UsedTokens,
		ContextWindow: &breakdown.WindowTokens, Breakdown: breakdown}), true
}

// claudeBreakdown maps Claude Code's report onto ContextBreakdown, attaching each category's
// contributors. Categories are matched by the display names /context prints.
func claudeBreakdown(u claudeContextUsage) *ContextBreakdown {
	window := u.RawMaxTokens
	if window == nil {
		window = u.MaxTokens
	}
	if u.TotalTokens == nil || *u.TotalTokens < 0 || window == nil || *window <= 0 || len(u.Categories) == 0 {
		return nil
	}
	b := &ContextBreakdown{Model: u.Model, UsedTokens: *u.TotalTokens, WindowTokens: *window, At: time.Now().UTC()}
	if u.IsAutoCompactEnabled && u.AutoCompactThreshold != nil && *u.AutoCompactThreshold > 0 {
		b.AutoCompactTokens = u.AutoCompactThreshold
	}
	for _, c := range u.Categories {
		if c.Tokens < 0 || strings.TrimSpace(c.Name) == "" {
			continue
		}
		kind := c.Kind
		switch {
		case kind == "used" || kind == "free" || kind == "buffer" || kind == "deferred":
		case c.IsDeferred:
			kind = "deferred"
		default:
			kind = "used"
		}
		b.Categories = append(b.Categories, ContextCategory{Name: c.Name, Tokens: c.Tokens, Kind: kind,
			Items: claudeContextItems(u, strings.ToLower(c.Name), kind == "deferred")})
	}
	return b
}

func claudeContextItems(u claudeContextUsage, name string, deferred bool) []ContextItem {
	var items []ContextItem
	add := func(name, detail string, tokens int64) {
		if tokens > 0 && name != "" {
			items = append(items, ContextItem{Name: name, Detail: detail, Tokens: tokens})
		}
	}
	switch {
	case name == "system prompt":
		for _, s := range u.SystemPromptSections {
			add(s.Name, "", s.Tokens)
		}
	case strings.HasPrefix(name, "mcp tools"):
		// Per server: one session can carry hundreds of MCP tool schemas.
		servers := map[string]int64{}
		var order []string
		for _, t := range u.MCPTools {
			if (t.IsLoaded == nil || *t.IsLoaded) == deferred {
				continue
			}
			if _, seen := servers[t.ServerName]; !seen {
				order = append(order, t.ServerName)
			}
			servers[t.ServerName] += t.Tokens
		}
		for _, server := range order {
			add(server, "MCP server", servers[server])
		}
	case strings.HasPrefix(name, "system tools"):
		if deferred {
			for _, t := range u.DeferredBuiltinTools {
				add(t.Name, "", t.Tokens)
			}
		} else {
			for _, t := range u.SystemTools {
				add(t.Name, "", t.Tokens)
			}
		}
	case name == "memory files":
		for _, f := range u.MemoryFiles {
			add(f.Path, f.Type, f.Tokens)
		}
	case name == "skills":
		if u.Skills != nil {
			for _, s := range u.Skills.SkillFrontmatter {
				add(s.Name, "", s.Tokens)
			}
		}
	case strings.Contains(name, "agent"):
		for _, a := range u.Agents {
			add(a.AgentType, "", a.Tokens)
		}
	case name == "messages":
		if m := u.MessageBreakdown; m != nil {
			add("User messages", "", m.UserMessageTokens)
			add("Assistant messages", "", m.AssistantMessageTokens)
			add("Tool calls", "", m.ToolCallTokens)
			add("Tool results", "", m.ToolResultTokens)
			add("Attachments", "", m.AttachmentTokens)
			add("Redirected context", "", m.RedirectedContextTokens)
			add("Unattributed", "", m.UnattributedTokens)
		}
	}
	slices.SortStableFunc(items, func(a, b ContextItem) int { return cmp.Compare(b.Tokens, a.Tokens) })
	if len(items) > claudeContextItemLimit {
		rest := items[claudeContextItemLimit-1:]
		var tokens int64
		for _, item := range rest {
			tokens += item.Tokens
		}
		items = append(items[:claudeContextItemLimit-1], ContextItem{Name: fmt.Sprintf("%d more", len(rest)), Tokens: tokens})
	}
	return items
}

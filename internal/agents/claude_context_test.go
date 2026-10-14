package agents

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestClaudeContextBreakdown(t *testing.T) {
	var frames []map[string]any
	pipe := &pipeProc{exited: make(chan struct{})}
	pipe.stdin = followupWireWriter(func(b []byte) (int, error) {
		var frame map[string]any
		if err := json.Unmarshal(b, &frame); err != nil {
			return 0, err
		}
		frames = append(frames, frame)
		return len(b), nil
	})
	d := &claudeDecoder{tools: map[string]ToolData{}}
	d.decode(pipe, []byte(`{"type":"result","subtype":"success","usage":{"input_tokens":1,"output_tokens":1}}`))
	d.decode(pipe, []byte(`{"type":"result","subtype":"success","usage":{"input_tokens":1,"output_tokens":1}}`))
	if len(frames) != 2 {
		t.Fatalf("each turn end asks for the breakdown once: %v", frames)
	}
	request, _ := frames[0]["request"].(map[string]any)
	if frames[0]["type"] != "control_request" || request["subtype"] != "get_context_usage" || request["detail"] != "summary" {
		t.Fatalf("wrong request: %v", frames[0])
	}

	reply := func(id any, payload string) []byte {
		return []byte(fmt.Sprintf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":%s}}`, id, payload))
	}
	tools := ""
	for i := range 30 {
		tools += fmt.Sprintf(`{"name":"mcp__s%d__t","serverName":"s%d","tokens":%d,"isLoaded":true},`, i, i, 100+i)
	}
	payload := `{"categories":[
		{"name":"System prompt","tokens":2000,"kind":"used"},
		{"name":"MCP tools","tokens":3435,"kind":"used"},
		{"name":"MCP tools (deferred)","tokens":500,"isDeferred":true},
		{"name":"Memory files","tokens":76,"kind":"used"},
		{"name":"Messages","tokens":900,"kind":"used"},
		{"name":"Autocompact buffer","tokens":33000,"kind":"buffer"},
		{"name":"Free space","tokens":160589,"kind":"free"}],
		"totalTokens":6411,"maxTokens":200000,"rawMaxTokens":200000,"model":"claude-x",
		"autoCompactThreshold":167000,"isAutoCompactEnabled":true,
		"memoryFiles":[{"path":"C:\\repo\\CLAUDE.md","type":"Project","tokens":76}],
		"mcpTools":[` + tools + `{"name":"mcp__lazy__t","serverName":"lazy","tokens":500,"isLoaded":false}],
		"messageBreakdown":{"toolCallTokens":100,"toolResultTokens":500,"attachmentTokens":0,"assistantMessageTokens":200,"userMessageTokens":100}}`

	u := chatEventData[ContextUsage](t, d.decode(pipe, reply(frames[1]["request_id"], payload)), EvUsage)
	b := u.Breakdown
	if b == nil || b.UsedTokens != 6411 || b.WindowTokens != 200000 || b.AutoCompactTokens == nil || *b.AutoCompactTokens != 167000 || b.Model != "claude-x" {
		t.Fatalf("breakdown totals: %+v", b)
	}
	if u.ContextTokens == nil || *u.ContextTokens != 6411 || u.ContextWindow == nil || *u.ContextWindow != 200000 {
		t.Fatalf("occupancy follows the harness report: %+v", u)
	}
	byName := map[string]ContextCategory{}
	for _, c := range b.Categories {
		byName[c.Name] = c
	}
	if byName["MCP tools (deferred)"].Kind != "deferred" || byName["Free space"].Kind != "free" || byName["Autocompact buffer"].Kind != "buffer" {
		t.Fatalf("kinds: %+v", b.Categories)
	}
	mcp := byName["MCP tools"].Items
	if len(mcp) != claudeContextItemLimit || mcp[0].Name != "s29" || mcp[len(mcp)-1].Name != "19 more" {
		t.Fatalf("loaded MCP tools grouped by server, largest first, overflow folded: %+v", mcp)
	}
	var folded int64
	for _, item := range mcp {
		folded += item.Tokens
	}
	if folded != byName["MCP tools"].Tokens {
		t.Fatalf("folding lost tokens: %d of %d", folded, byName["MCP tools"].Tokens)
	}
	if deferred := byName["MCP tools (deferred)"].Items; len(deferred) != 1 || deferred[0].Name != "lazy" {
		t.Fatalf("deferred MCP servers: %+v", deferred)
	}
	if files := byName["Memory files"].Items; len(files) != 1 || files[0].Detail != "Project" {
		t.Fatalf("memory files: %+v", files)
	}
	if messages := byName["Messages"].Items; len(messages) != 4 || messages[0].Name != "Tool results" {
		t.Fatalf("zero rows are dropped and the rest sorted: %+v", messages)
	}

	if events := d.decode(pipe, reply(frames[0]["request_id"], payload)); len(events) != 0 {
		t.Fatalf("an older reply overwrote a newer one: %+v", events)
	}
	if events := d.decode(pipe, []byte(`{"type":"control_response","response":{"subtype":"error","request_id":"orole-context-9","error":"unsupported"}}`)); len(events) != 0 {
		t.Fatalf("an unsupported CLI produced usage: %+v", events)
	}
}

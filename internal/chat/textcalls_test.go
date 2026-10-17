package chat

import (
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func TestParseTextCalls(t *testing.T) {
	specs := []llm.ToolSpec{{Name: "orole_get_skill"}, {Hosted: llm.HostedWebSearch}}
	good := []string{
		`[{"name": "orole_get_skill", "arguments": {"name": "timeedit-schedule"}}]`,
		"```json\n{\"name\":\"orole_get_skill\",\"parameters\":{\"name\":\"x\"}}\n```",
		`<tool_call>{"name":"orole_get_skill"}</tool_call>`,
	}
	for _, g := range good {
		if calls, ok := parseTextCalls(g, specs); !ok || len(calls) != 1 || calls[0].Name != "orole_get_skill" {
			t.Errorf("parseTextCalls(%q) = %v, %v", g, calls, ok)
		}
	}
	bad := []string{
		`[{"name": "unknown_tool", "arguments": {}}]`,
		`[{"name": "web_search", "arguments": {}}]`,
		`Here is JSON: {"name":"orole_get_skill"}`,
		`[{"name": "orole_get_skill", "arguments": "oops"}]`,
		`[1, 2, 3]`,
		``,
	}
	for _, b := range bad {
		if calls, ok := parseTextCalls(b, specs); ok {
			t.Errorf("parseTextCalls(%q) accepted: %v", b, calls)
		}
	}
}

func TestLooksLikeTextCall(t *testing.T) {
	for s, want := range map[string]bool{"": true, "[": true, ` [{"na`: true, "{": true, `{"name"`: true, "```json\n[{": true,
		"[Note] hi": false, "Hello": false, `{ not json`: false, "```go": false} {
		if got := looksLikeTextCall(s); got != want {
			t.Errorf("looksLikeTextCall(%q) = %v, want %v", s, got, want)
		}
	}
}

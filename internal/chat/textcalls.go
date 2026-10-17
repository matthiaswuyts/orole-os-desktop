package chat

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Some models (mostly open-weight ones behind a gateway) write a tool call as plain text instead of
// using the provider's tool-call channel, e.g. `[{"name": "x", "arguments": {...}}]`. Shown as it is,
// the user reads raw JSON and the tool never runs. looksLikeTextCall tells the stream to hold such
// text back; parseTextCalls turns it into real calls when every name is a tool this turn offered.

// looksLikeTextCall reports whether the text so far could still turn out to be a textual tool call.
func looksLikeTextCall(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true // nothing to judge yet: keep holding
	}
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "```json"), "```"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "<tool_call>"))
	switch {
	case s == "":
		return true
	case s[0] == '[':
		r := strings.TrimSpace(s[1:])
		return r == "" || r[0] == '{'
	case s[0] == '{':
		r := strings.TrimSpace(s[1:])
		return r == "" || r[0] == '"'
	}
	return false
}

// parseTextCalls extracts tool calls from text that consists of nothing else. ok is false when the
// text is anything but calls to tools in specs.
func parseTextCalls(text string, specs []llm.ToolSpec) (calls []llm.ToolCall, ok bool) {
	s := strings.TrimSpace(text)
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "```json"), "```")), "```"))
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(s, "<tool_call>")), "</tool_call>"))
	if s == "" {
		return nil, false
	}
	var items []json.RawMessage
	if s[0] == '[' {
		if json.Unmarshal([]byte(s), &items) != nil {
			return nil, false
		}
	} else {
		items = []json.RawMessage{json.RawMessage(s)}
	}
	if len(items) == 0 {
		return nil, false
	}
	for i, it := range items {
		var c struct {
			Name       string          `json:"name"`
			Arguments  json.RawMessage `json:"arguments"`
			Parameters json.RawMessage `json:"parameters"`
		}
		if json.Unmarshal(it, &c) != nil || c.Name == "" {
			return nil, false
		}
		known := false
		for _, sp := range specs {
			if sp.Hosted == "" && sp.Name == c.Name {
				known = true
				break
			}
		}
		if !known {
			return nil, false
		}
		args := c.Arguments
		if len(args) == 0 {
			args = c.Parameters
		}
		if len(args) == 0 || string(args) == "null" {
			args = json.RawMessage("{}")
		}
		var obj map[string]any
		if json.Unmarshal(args, &obj) != nil { // arguments must be an object
			return nil, false
		}
		calls = append(calls, llm.ToolCall{ID: "text_call_" + strconv.Itoa(i+1), Name: c.Name, Args: args})
	}
	return calls, true
}

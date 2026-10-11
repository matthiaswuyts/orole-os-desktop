// Package cli provides model providers that run a vendor's own CLI (Codex, Claude Code) with the user's account, so
// bots use that account's plan. This is the one place where Orole starts a harness for chat: the CLI signs in and keeps
// the credentials, Orole never reads them. The CLI is used as a plain model, not as an agent: it runs in an empty
// scratch directory with the strictest sandbox the CLI offers and is told to answer from the conversation alone.
// Orole's own tool loop stays in charge: tools are offered to the model in the prompt and a reply that asks for them is
// turned into tool-call deltas, so memory, limits, handoffs and structured components work like with any provider.
package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// maxToolResultPrompt bounds one tool result in a rendered prompt; the engine already clips results.
const maxToolResultPrompt = 100 << 10

const preamble = `You are the language model behind a chat assistant, not a coding agent. Do not run commands, read or edit files or use any tool of your own. Answer only from the conversation below and follow the system instructions in it.`

const toolProtocol = `# Tools
You may call the tools listed below. To call one or more, reply with exactly one fenced block labelled tool_calls holding a JSON array, and nothing after it:

` + "```tool_calls" + `
[{"name": "tool_name", "arguments": {"arg": "value"}}]
` + "```" + `

You will receive the results and can then answer or call more tools. When no tool is needed, just write the answer. Only call tools from this list, with arguments that match their schema.

`

// buildPrompt renders a request as one prompt for a CLI that takes text.
func buildPrompt(req llm.Request) string {
	var b strings.Builder
	b.WriteString(preamble)
	b.WriteString("\n\n# System instructions\n")
	b.WriteString(strings.TrimSpace(req.System))
	b.WriteString("\n\n")
	var client []llm.ToolSpec
	for _, t := range req.Tools {
		if t.Hosted == "" && t.Name != "" {
			client = append(client, t)
		}
	}
	if len(client) > 0 {
		b.WriteString(toolProtocol)
		for _, t := range client {
			fmt.Fprintf(&b, "## %s\n%s\nSchema: %s\n\n", t.Name, strings.TrimSpace(t.Description), compact(t.Schema))
		}
	}
	b.WriteString("# Conversation\n")
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			switch p.Kind {
			case llm.PartText:
				fmt.Fprintf(&b, "[%s]\n%s\n\n", label(m.Role), strings.TrimSpace(p.Text))
			case llm.PartImage, llm.PartFile:
				fmt.Fprintf(&b, "[%s attached a file the model cannot see: %s]\n\n", label(m.Role), p.Name)
			case llm.PartToolCall:
				fmt.Fprintf(&b, "[assistant called the tool %s with %s]\n\n", p.ToolName, compact(p.Args))
			case llm.PartToolResult:
				text := p.Text
				if len(text) > maxToolResultPrompt {
					text = text[:maxToolResultPrompt] + "\n[truncated]"
				}
				kind := "result"
				if p.IsError {
					kind = "error"
				}
				fmt.Fprintf(&b, "[tool %s of %s]\n%s\n\n", kind, p.ToolName, text)
			}
		}
	}
	b.WriteString("Write the assistant's next message now.")
	return b.String()
}

func label(r llm.Role) string {
	if r == llm.RoleAssistant {
		return "assistant"
	}
	return "user"
}

func compact(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

var toolFence = regexp.MustCompile("(?s)```tool_calls[ \t]*\r?\n(.*?)\r?\n```")

// splitReply separates a reply into the text before a tool_calls block and the calls it holds. A block that is not
// valid JSON, or that names no offered tool, is ordinary text: a model's slip must not become an action.
func splitReply(reply string, tools []llm.ToolSpec) (text string, calls []llm.ToolCall) {
	m := toolFence.FindStringSubmatchIndex(reply)
	if m == nil {
		return strings.TrimSpace(reply), nil
	}
	var raw []struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal([]byte(reply[m[2]:m[3]]), &raw) != nil {
		return strings.TrimSpace(reply), nil
	}
	offered := map[string]bool{}
	for _, t := range tools {
		if t.Hosted == "" {
			offered[t.Name] = true
		}
	}
	for i, c := range raw {
		if !offered[c.Name] {
			continue
		}
		args := c.Arguments
		if len(args) == 0 || string(args) == "null" {
			args = json.RawMessage("{}")
		}
		calls = append(calls, llm.ToolCall{ID: fmt.Sprintf("call_%d", i+1), Name: c.Name, Args: args})
	}
	if len(calls) == 0 {
		return strings.TrimSpace(reply), nil
	}
	return strings.TrimSpace(reply[:m[0]]), calls
}

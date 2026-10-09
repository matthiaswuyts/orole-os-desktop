package openai

import (
	"encoding/json"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// responsesRequest is the Responses API body (POST /responses).
type responsesRequest struct {
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions,omitempty"`
	Input           []any           `json:"input"`
	Stream          bool            `json:"stream"`
	Store           bool            `json:"store"` // always false: Orole owns conversation state
	Tools           []tool          `json:"tools,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	MaxToolCalls    int             `json:"max_tool_calls,omitempty"`
	Reasoning       *reasoningParam `json:"reasoning,omitempty"`
}

type reasoningParam struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type tool struct {
	Type              string          `json:"type"`
	Name              string          `json:"name,omitempty"`
	Description       string          `json:"description,omitempty"`
	Parameters        json.RawMessage `json:"parameters,omitempty"`
	Strict            *bool           `json:"strict,omitempty"`
	Filters           *searchFilters  `json:"filters,omitempty"`
	SearchContextSize string          `json:"search_context_size,omitempty"`
}

type searchFilters struct {
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
}

type messageItem struct {
	Type    string        `json:"type"`
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data,omitempty"`
}

type functionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type functionOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

var emptySchema = json.RawMessage(`{"type":"object","properties":{}}`)

// buildRequest maps an llm.Request onto the Responses body. withSummary requests
// reasoning summaries for reasoning-capable models (some organizations are not allowed
// them; the caller retries without).
func buildRequest(req llm.Request, withSummary bool) responsesRequest {
	body := responsesRequest{
		Model: req.Model, Instructions: req.System, Stream: true,
		MaxOutputTokens: req.MaxOutputTokens, Input: make([]any, 0, len(req.Messages)),
	}
	if m, ok := modelFor(req.Model); ok && m.Caps.Reasoning {
		r := &reasoningParam{Effort: req.Reasoning}
		if withSummary {
			r.Summary = "auto"
		}
		if r.Effort != "" || r.Summary != "" {
			body.Reasoning = r
		}
	} else if req.Reasoning != "" {
		body.Reasoning = &reasoningParam{Effort: req.Reasoning}
	}
	for _, m := range req.Messages {
		body.Input = appendMessage(body.Input, m)
	}
	hosted := false
	for _, t := range req.Tools {
		if t.Hosted != "" {
			if t.Hosted == llm.HostedWebSearch {
				hosted = true
				body.Tools = append(body.Tools, webSearchTool(t.Params))
			}
			continue // web_fetch and datetime are not OpenAI hosted tools
		}
		if t.Name == "" {
			continue
		}
		schema := t.Schema
		if len(schema) == 0 {
			schema = emptySchema
		}
		body.Tools = append(body.Tools, tool{Type: "function", Name: t.Name, Description: t.Description, Parameters: schema, Strict: new(false)})
	}
	if hosted && req.MaxHostedToolCalls > 0 {
		body.MaxToolCalls = req.MaxHostedToolCalls
	}
	return body
}

// webSearchTool maps llm.ToolSpec.Params. The Responses API accepts either allowed or
// blocked domains, so allowed wins when both are given.
func webSearchTool(params map[string]any) tool {
	t := tool{Type: "web_search"}
	if allowed := stringList(params["allowed_domains"]); len(allowed) > 0 {
		t.Filters = &searchFilters{AllowedDomains: allowed}
	} else if blocked := stringList(firstSet(params, "excluded_domains", "blocked_domains")); len(blocked) > 0 {
		t.Filters = &searchFilters{BlockedDomains: blocked}
	}
	switch s, _ := params["search_context_size"].(string); s {
	case "low", "medium", "high":
		t.SearchContextSize = s
	}
	return t
}

func firstSet(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func stringList(v any) []string {
	switch l := v.(type) {
	case []string:
		return l
	case []any:
		out := make([]string, 0, len(l))
		for _, e := range l {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func appendMessage(dst []any, m llm.Message) []any {
	switch m.Role {
	case llm.RoleTool:
		for _, p := range m.Parts {
			if p.Kind == llm.PartToolResult {
				dst = append(dst, functionOutputItem{Type: "function_call_output", CallID: p.ToolCallID, Output: p.Text})
			}
		}
		return dst
	case llm.RoleAssistant:
		var text strings.Builder
		flush := func() {
			if text.Len() > 0 {
				dst = append(dst, messageItem{Type: "message", Role: "assistant", Content: []contentPart{{Type: "output_text", Text: text.String()}}})
				text.Reset()
			}
		}
		for _, p := range m.Parts {
			switch p.Kind {
			case llm.PartText:
				text.WriteString(p.Text)
			case llm.PartToolCall:
				flush()
				args := string(p.Args)
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				// The original item id is deliberately omitted: with store=false the matching
				// reasoning item is not replayed, and call_id alone is sufficient.
				dst = append(dst, functionCallItem{Type: "function_call", CallID: p.ToolCallID, Name: p.ToolName, Arguments: args})
			}
		}
		flush()
		return dst
	}
	parts := make([]contentPart, 0, len(m.Parts))
	for _, p := range m.Parts {
		switch p.Kind {
		case llm.PartText:
			if p.Text != "" {
				parts = append(parts, contentPart{Type: "input_text", Text: p.Text})
			}
		case llm.PartImage:
			parts = append(parts, contentPart{Type: "input_image", ImageURL: llm.DataURL(p.MIME, p.Data)})
		case llm.PartFile:
			name := p.Name
			if name == "" {
				name = "document.pdf"
			}
			mime := p.MIME
			if mime == "" {
				mime = "application/pdf"
			}
			parts = append(parts, contentPart{Type: "input_file", Filename: name, FileData: llm.DataURL(mime, p.Data)})
		}
	}
	if len(parts) == 0 {
		parts = append(parts, contentPart{Type: "input_text", Text: ""})
	}
	return append(dst, messageItem{Type: "message", Role: "user", Content: parts})
}

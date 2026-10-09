package openrouter

import (
	"encoding/json"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// chatRequest is the Chat Completions body (POST /chat/completions).
type chatRequest struct {
	Model        string          `json:"model"`
	Messages     []message       `json:"messages"`
	Stream       bool            `json:"stream"`
	Tools        []tool          `json:"tools,omitempty"`
	MaxTokens    int             `json:"max_tokens,omitempty"`
	MaxToolCalls int             `json:"max_tool_calls,omitempty"`
	Reasoning    *reasoningParam `json:"reasoning,omitempty"`
	Plugins      []plugin        `json:"plugins,omitempty"`
}

type reasoningParam struct {
	Effort string `json:"effort"`
}

type plugin struct {
	ID  string     `json:"id"`
	PDF *pdfPlugin `json:"pdf,omitempty"`
}

type pdfPlugin struct {
	Engine string `json:"engine"`
}

type message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"` // string, []contentPart or nil
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
	File     *filePart `json:"file,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type filePart struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// tool is either a client function or an openrouter:* server tool.
type tool struct {
	Type       string         `json:"type"`
	Function   *functionSpec  `json:"function,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type functionSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

var emptySchema = json.RawMessage(`{"type":"object","properties":{}}`)

// Hosted tool type names (server tools; the deprecated web plugin and :online are not used).
var hostedType = map[string]string{
	llm.HostedWebSearch: "openrouter:web_search",
	llm.HostedWebFetch:  "openrouter:web_fetch",
	llm.HostedDatetime:  "openrouter:datetime",
}

// hostedParams lists the llm.ToolSpec.Params accepted per hosted tool, as [from, to]
// pairs (aliases map the engine-neutral names onto OpenRouter's).
var hostedParams = map[string][][2]string{
	llm.HostedWebSearch: {
		{"max_results", "max_results"}, {"max_uses", "max_uses"}, {"max_total_results", "max_total_results"},
		{"allowed_domains", "allowed_domains"}, {"excluded_domains", "excluded_domains"}, {"blocked_domains", "excluded_domains"},
	},
	llm.HostedWebFetch: {
		{"max_uses", "max_uses"}, {"max_content_tokens", "max_content_tokens"},
		{"allowed_domains", "allowed_domains"}, {"blocked_domains", "blocked_domains"}, {"excluded_domains", "blocked_domains"},
	},
	llm.HostedDatetime: {{"timezone", "timezone"}},
}

const defaultPDFEngine = "cloudflare-ai"

func pdfEngine(want string) string {
	switch want {
	case "cloudflare-ai", "mistral-ocr", "native":
		return want
	}
	return defaultPDFEngine
}

// buildRequest maps an llm.Request onto the Chat Completions body.
func buildRequest(req llm.Request) chatRequest {
	body := chatRequest{Model: req.Model, Stream: true, MaxTokens: req.MaxOutputTokens}
	if req.Reasoning != "" {
		body.Reasoning = &reasoningParam{Effort: req.Reasoning}
	}
	hasFile := false
	if req.System != "" {
		body.Messages = append(body.Messages, message{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		body.Messages = appendMessage(body.Messages, m, &hasFile)
	}
	hosted := false
	for _, t := range req.Tools {
		if t.Hosted != "" {
			ty, ok := hostedType[t.Hosted]
			if !ok {
				continue
			}
			hosted = true
			body.Tools = append(body.Tools, tool{Type: ty, Parameters: mapHostedParams(t.Hosted, t.Params)})
			continue
		}
		if t.Name == "" {
			continue
		}
		schema := t.Schema
		if len(schema) == 0 {
			schema = emptySchema
		}
		body.Tools = append(body.Tools, tool{Type: "function", Function: &functionSpec{Name: t.Name, Description: t.Description, Parameters: schema}})
	}
	if hosted && req.MaxHostedToolCalls > 0 {
		body.MaxToolCalls = req.MaxHostedToolCalls
	}
	if hasFile {
		body.Plugins = []plugin{{ID: "file-parser", PDF: &pdfPlugin{Engine: pdfEngine(req.PDFEngine)}}}
	}
	return body
}

func mapHostedParams(hostedTool string, in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	var out map[string]any
	for _, p := range hostedParams[hostedTool] {
		v, ok := in[p[0]]
		if !ok || v == nil || emptyList(v) {
			continue
		}
		if out == nil {
			out = map[string]any{}
		}
		if _, dup := out[p[1]]; !dup {
			out[p[1]] = v
		}
	}
	return out
}

func emptyList(v any) bool {
	switch l := v.(type) {
	case []string:
		return len(l) == 0
	case []any:
		return len(l) == 0
	}
	return false
}

func appendMessage(dst []message, m llm.Message, hasFile *bool) []message {
	switch m.Role {
	case llm.RoleTool:
		for _, p := range m.Parts {
			if p.Kind == llm.PartToolResult {
				dst = append(dst, message{Role: "tool", ToolCallID: p.ToolCallID, Content: p.Text})
			}
		}
		return dst
	case llm.RoleAssistant:
		var text strings.Builder
		var calls []toolCall
		for _, p := range m.Parts {
			switch p.Kind {
			case llm.PartText:
				text.WriteString(p.Text)
			case llm.PartToolCall:
				args := string(p.Args)
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				calls = append(calls, toolCall{ID: p.ToolCallID, Type: "function", Function: toolFunction{Name: p.ToolName, Arguments: args}})
			}
		}
		out := message{Role: "assistant", ToolCalls: calls}
		if text.Len() > 0 {
			out.Content = text.String()
		} else if len(calls) == 0 {
			out.Content = ""
		}
		return append(dst, out)
	}
	parts := make([]contentPart, 0, len(m.Parts))
	for _, p := range m.Parts {
		switch p.Kind {
		case llm.PartText:
			if p.Text != "" {
				parts = append(parts, contentPart{Type: "text", Text: p.Text})
			}
		case llm.PartImage:
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: llm.DataURL(p.MIME, p.Data)}})
		case llm.PartFile:
			name := p.Name
			if name == "" {
				name = "document.pdf"
			}
			mime := p.MIME
			if mime == "" {
				mime = "application/pdf"
			}
			*hasFile = true
			parts = append(parts, contentPart{Type: "file", File: &filePart{Filename: name, FileData: llm.DataURL(mime, p.Data)}})
		}
	}
	switch {
	case len(parts) == 1 && parts[0].Type == "text":
		return append(dst, message{Role: "user", Content: parts[0].Text})
	case len(parts) == 0:
		return append(dst, message{Role: "user", Content: ""})
	}
	return append(dst, message{Role: "user", Content: parts})
}

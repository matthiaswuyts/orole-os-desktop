package openaicompat

import (
	"encoding/json"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// chatRequest is the OpenAI Chat Completions body.
type chatRequest struct {
	Model           string         `json:"model"`
	Messages        []message      `json:"messages"`
	Stream          bool           `json:"stream"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	Tools           []tool         `json:"tools,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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

type tool struct {
	Type     string       `json:"type"`
	Function functionSpec `json:"function"`
}

type functionSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

var emptySchema = json.RawMessage(`{"type":"object","properties":{}}`)

// buildRequest maps an llm.Request onto the Chat Completions body. Hosted tools are never
// sent. Images and PDFs are only sent to models the manifest declares vision/pdf for.
func (p *Provider) buildRequest(req llm.Request) (chatRequest, error) {
	body := chatRequest{
		Model: req.Model, Stream: true, StreamOptions: &streamOptions{IncludeUsage: true},
		MaxTokens: req.MaxOutputTokens, ReasoningEffort: req.Reasoning,
	}
	declared, _ := p.declaredModel(req.Model)
	if req.System != "" {
		body.Messages = append(body.Messages, message{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		var err error
		if body.Messages, err = appendMessage(body.Messages, m, declared.Vision, declared.PDF); err != nil {
			return chatRequest{}, err
		}
	}
	for _, t := range req.Tools {
		if t.Hosted != "" || t.Name == "" {
			continue
		}
		schema := t.Schema
		if len(schema) == 0 {
			schema = emptySchema
		}
		body.Tools = append(body.Tools, tool{Type: "function", Function: functionSpec{Name: t.Name, Description: t.Description, Parameters: schema}})
	}
	return body, nil
}

func appendMessage(dst []message, m llm.Message, vision, pdf bool) ([]message, error) {
	switch m.Role {
	case llm.RoleTool:
		for _, p := range m.Parts {
			if p.Kind == llm.PartToolResult {
				dst = append(dst, message{Role: "tool", ToolCallID: p.ToolCallID, Content: p.Text})
			}
		}
		return dst, nil
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
		return append(dst, out), nil
	}
	parts := make([]contentPart, 0, len(m.Parts))
	for _, p := range m.Parts {
		switch p.Kind {
		case llm.PartText:
			if p.Text != "" {
				parts = append(parts, contentPart{Type: "text", Text: p.Text})
			}
		case llm.PartImage:
			if !vision {
				return nil, &llm.Error{Code: llm.ErrBadRequest, Message: "this model is not declared to accept images in the provider manifest"}
			}
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: llm.DataURL(p.MIME, p.Data)}})
		case llm.PartFile:
			if !pdf {
				return nil, &llm.Error{Code: llm.ErrBadRequest, Message: "this model is not declared to accept PDF files in the provider manifest"}
			}
			name := p.Name
			if name == "" {
				name = "document.pdf"
			}
			mime := p.MIME
			if mime == "" {
				mime = "application/pdf"
			}
			parts = append(parts, contentPart{Type: "file", File: &filePart{Filename: name, FileData: llm.DataURL(mime, p.Data)}})
		}
	}
	switch {
	case len(parts) == 1 && parts[0].Type == "text":
		return append(dst, message{Role: "user", Content: parts[0].Text}), nil
	case len(parts) == 0:
		return append(dst, message{Role: "user", Content: ""}), nil
	}
	return append(dst, message{Role: "user", Content: parts}), nil
}

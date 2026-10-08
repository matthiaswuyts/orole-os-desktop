package mcptest

import (
	"context"
	"encoding/json"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// AddJSONTool registers a tool whose handler receives decoded arguments and returns a
// JSON-serialisable value delivered both as text content and structuredContent.
func AddJSONTool(s *sdk.Server, name, desc string, ann *sdk.ToolAnnotations, props map[string]any, required []string, fn func(args map[string]any) (any, error)) {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	s.AddTool(&sdk.Tool{Name: name, Description: desc, Annotations: ann, InputSchema: schema},
		func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			args := map[string]any{}
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return nil, err
				}
			}
			out, err := fn(args)
			if err != nil {
				return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}}}, nil
			}
			b, _ := json.Marshal(out)
			res := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}
			var sc any
			if json.Unmarshal(b, &sc) == nil {
				if _, ok := sc.(map[string]any); ok {
					res.StructuredContent = sc
				}
			}
			return res, nil
		})
}

// ReadOnly / Destructive / Write are annotation presets.
func ReadOnly() *sdk.ToolAnnotations { return &sdk.ToolAnnotations{ReadOnlyHint: true} }
func Destructive() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{DestructiveHint: new(true)}
}

// DefaultTools registers three generic tools: echo (read-only), write_note, delete_note (destructive).
func DefaultTools(s *sdk.Server) {
	AddJSONTool(s, "echo", "Echo the message back.", ReadOnly(), map[string]any{"message": map[string]any{"type": "string"}}, []string{"message"},
		func(a map[string]any) (any, error) { return map[string]any{"echo": a["message"]}, nil })
	AddJSONTool(s, "write_note", "Write a note.", &sdk.ToolAnnotations{DestructiveHint: new(false)}, map[string]any{"text": map[string]any{"type": "string"}}, nil,
		func(a map[string]any) (any, error) { return map[string]any{"ok": true}, nil })
	AddJSONTool(s, "delete_note", "Delete a note.", Destructive(), map[string]any{"id": map[string]any{"type": "string"}}, nil,
		func(a map[string]any) (any, error) { return map[string]any{"deleted": a["id"]}, nil })
}

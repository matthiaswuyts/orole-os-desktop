// Package maildrive contains the shared, conservative MCP contract machinery for
// the typed mail and Drive adapters. Tool aliases are candidates, not claims
// about tools offered by a production server.
package maildrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

var (
	ErrNotConfigured = errors.New("NotConfigured: no MCP server assigned to this adapter")
	ErrUnsupported   = errors.New("Unsupported: discovered tools do not support this contract")
	ErrInvalidResult = errors.New("invalid MCP adapter result")
	ErrTool          = errors.New("MCP tool reported an error")
)

type Manager interface {
	ByAdapter(context.Context, string) (mcp.ServerState, bool)
	Tools(context.Context, string) ([]mcp.ToolInfo, error)
	CallTool(context.Context, string, string, map[string]any, mcp.CallOptions) (*mcp.CallResult, error)
}

type Policy interface {
	Require(context.Context, policy.Request) error
}

type Connection struct {
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	State      string `json:"state"`
	Error      string `json:"error"`
}

type Field struct {
	Name     string
	Aliases  []string
	Type     string
	Required bool
}

type Operation struct {
	Names  []string
	Fields []Field
	Effect policy.Effect
}

type Binding struct {
	Tool   string
	Fields map[string]string
	Types  map[string]string
	Schema *jsonschema.Resolved
	Effect policy.Effect
}

func Discover(ctx context.Context, manager Manager, adapter string, ops map[string]Operation) (Connection, map[string]Binding, error) {
	bindings := map[string]Binding{}
	if manager == nil {
		return Connection{}, bindings, nil
	}
	server, ok := manager.ByAdapter(ctx, adapter)
	if !ok {
		return Connection{}, bindings, nil
	}
	status := Connection{Configured: true, Connected: server.Status == mcp.StatusConnected, ServerID: server.ID, ServerName: untrusted.Line(server.Name, 200), State: string(server.Status), Error: untrusted.Line(server.LastError, 400)}
	if !status.Connected {
		return status, bindings, nil
	}
	tools, err := manager.Tools(ctx, server.ID)
	if err != nil {
		return status, bindings, err
	}
	for name, op := range ops {
		for _, alias := range op.Names {
			for _, tool := range tools {
				if tool.Name != alias {
					continue
				}
				if binding, ok := bind(tool, op); ok {
					bindings[name] = binding
					break
				}
			}
			if _, ok := bindings[name]; ok {
				break
			}
		}
	}
	return status, bindings, nil
}

// Reject reference/composition contracts rather than fetching schema URLs or
// guessing which branch, nested argument, account ID or default to manufacture.
func bind(tool mcp.ToolInfo, op Operation) (Binding, bool) {
	var root map[string]json.RawMessage
	if len(tool.InputSchema) > 64<<10 || json.Unmarshal(tool.InputSchema, &root) != nil {
		return Binding{}, false
	}
	for key := range root {
		switch key {
		case "type", "properties", "required", "additionalProperties", "description", "title", "$schema":
		default:
			return Binding{}, false
		}
	}
	var typ string
	if json.Unmarshal(root["type"], &typ) != nil || typ != "object" {
		return Binding{}, false
	}
	var props map[string]json.RawMessage
	if raw := root["properties"]; len(raw) > 0 && json.Unmarshal(raw, &props) != nil {
		return Binding{}, false
	}
	var required []string
	if raw := root["required"]; len(raw) > 0 && json.Unmarshal(raw, &required) != nil {
		return Binding{}, false
	}
	b := Binding{Tool: tool.Name, Fields: map[string]string{}, Types: map[string]string{}, Effect: op.Effect}
	for _, field := range op.Fields {
		for _, alias := range field.Aliases {
			raw, ok := props[alias]
			if !ok {
				continue
			}
			var p map[string]json.RawMessage
			if json.Unmarshal(raw, &p) != nil {
				continue
			}
			valid := true
			for key := range p {
				switch key {
				case "type", "description", "title", "default", "enum", "minLength", "maxLength", "pattern", "minimum", "maximum", "minItems", "maxItems", "items":
				default:
					valid = false
				}
			}
			var pt string
			if !valid || json.Unmarshal(p["type"], &pt) != nil {
				continue
			}
			if pt != field.Type && !(field.Type == "recipients" && (pt == "string" || pt == "array")) {
				continue
			}
			if pt == "array" {
				var item map[string]json.RawMessage
				var itemType string
				if json.Unmarshal(p["items"], &item) != nil || len(item) != 1 || json.Unmarshal(item["type"], &itemType) != nil || itemType != "string" {
					continue
				}
			}
			b.Fields[field.Name], b.Types[field.Name] = alias, pt
			break
		}
		if field.Required && b.Fields[field.Name] == "" {
			return Binding{}, false
		}
	}
	for _, key := range required {
		found := false
		for _, mapped := range b.Fields {
			if key == mapped {
				found = true
				break
			}
		}
		if !found {
			return Binding{}, false
		}
	}
	// Unmapped optional properties cannot contain references either.
	if strings.Contains(string(tool.InputSchema), "\"$ref\"") || strings.Contains(string(tool.InputSchema), "\"$dynamicRef\"") {
		return Binding{}, false
	}
	var schema jsonschema.Schema
	if json.Unmarshal(tool.InputSchema, &schema) != nil {
		return Binding{}, false
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return Binding{}, false
	}
	b.Schema = resolved
	// Never downgrade a discovered destructive/write annotation to a read.
	if tool.DestructiveHint != nil && *tool.DestructiveHint || tool.Effect == string(policy.EffectDestructive) {
		b.Effect = policy.EffectDestructive
	} else if op.Effect == policy.EffectRead && (tool.Effect == string(policy.EffectWrite) || tool.ReadOnlyHint != nil && !*tool.ReadOnlyHint) {
		b.Effect = policy.EffectWrite
	}
	return b, true
}

func Call(ctx context.Context, manager Manager, gate Policy, adapter, actor, operation string, ops map[string]Operation, values map[string]any) (json.RawMessage, error) {
	op, known := ops[operation]
	if !known {
		return nil, ErrUnsupported
	}
	status, bindings, err := Discover(ctx, manager, adapter, map[string]Operation{operation: op})
	if err != nil {
		return nil, err
	}
	if !status.Configured {
		return nil, ErrNotConfigured
	}
	if !status.Connected {
		return nil, mcp.ErrNotConnected
	}
	b, ok := bindings[operation]
	if !ok {
		return nil, ErrUnsupported
	}
	args := map[string]any{}
	for name, value := range values {
		key := b.Fields[name]
		if key == "" {
			return nil, fmt.Errorf("%w: argument %s", ErrUnsupported, name)
		}
		if recipients, ok := value.([]string); ok {
			if b.Types[name] == "string" {
				value = strings.Join(recipients, ", ")
			} else {
				items := make([]any, len(recipients))
				for i, v := range recipients {
					items[i] = v
				}
				value = items
			}
		}
		args[key] = value
	}
	if b.Schema.Validate(args) != nil {
		return nil, fmt.Errorf("%w: arguments do not satisfy discovered schema", ErrUnsupported)
	}
	if gate == nil {
		return nil, policy.ErrDenied
	}
	if actor == "" {
		actor = "user"
	}
	detail, _ := json.Marshal(args)
	if err := gate.Require(ctx, policy.Request{Actor: actor, Action: adapter + "." + operation, Resource: "mcp/" + status.ServerID + "/" + b.Tool, Detail: untrusted.Text(string(detail), 1800), Effect: b.Effect}); err != nil {
		return nil, err
	}
	result, err := manager.CallTool(ctx, status.ServerID, b.Tool, args, mcp.CallOptions{Actor: actor})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrInvalidResult
	}
	if result.IsError {
		return nil, ErrTool
	}
	if len(result.Structured) > 0 && string(result.Structured) != "null" {
		if len(result.Structured) > 1<<20 || !json.Valid(result.Structured) {
			return nil, ErrInvalidResult
		}
		return result.Structured, nil
	}
	var raw json.RawMessage
	for _, block := range result.Content {
		if block.Type != "text" {
			continue
		}
		if block.Truncated || len(block.Text) > 1<<20 || !json.Valid([]byte(block.Text)) || raw != nil {
			return nil, ErrInvalidResult
		}
		raw = json.RawMessage(block.Text)
	}
	if raw == nil {
		return nil, ErrInvalidResult
	}
	return raw, nil
}

func String(m map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var s string
		if json.Unmarshal(m[key], &s) == nil {
			return s
		}
	}
	return ""
}

func ID(s string) (string, error) {
	if s == "" || len(s) > 1024 || untrusted.Line(s, 0) != s {
		return "", ErrInvalidResult
	}
	return s, nil
}

func Object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, ErrInvalidResult
	}
	if e, ok := m["error"]; ok && string(e) != "null" && string(e) != "\"\"" {
		return nil, ErrTool
	}
	if success, ok := m["success"]; ok && string(success) != "true" {
		return nil, ErrTool
	}
	return m, nil
}

func Collection(raw json.RawMessage, keys ...string) ([]json.RawMessage, string, error) {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil && list != nil {
		return list, "", nil
	}
	m, err := Object(raw)
	if err != nil {
		return nil, "", err
	}
	if err := Strings(m, "nextPageToken", "next_page_token"); err != nil {
		return nil, "", err
	}
	for _, key := range keys {
		if value, ok := m[key]; ok && json.Unmarshal(value, &list) == nil && list != nil {
			token := String(m, "nextPageToken", "next_page_token")
			if token != "" {
				if _, err := ID(token); err != nil {
					return nil, "", err
				}
			}
			return list, token, nil
		}
	}
	return nil, "", ErrInvalidResult
}

// Confirmation requires an operation-specific state/flag and a matching identity
// for trash, or an allocated message ID for send/reply. Generic {ok:true} is not
// evidence that a mail was sent or a particular item moved to trash.
func Confirmation(raw json.RawMessage, operation, expected string) (string, error) {
	m, err := Object(raw)
	if err != nil {
		return "", err
	}
	if err := Strings(m, "id", "messageId", "message_id", "fileId", "file_id", "status"); err != nil {
		return "", err
	}
	id := String(m, "id", "messageId", "message_id", "fileId", "file_id")
	if _, err := ID(id); err != nil {
		return "", err
	}
	if expected != "" && id != expected {
		return "", ErrInvalidResult
	}
	flag := "sent"
	if operation == "trash" {
		flag = "trashed"
	}
	var yes bool
	if raw, ok := m[flag]; ok {
		if json.Unmarshal(raw, &yes) != nil || !yes {
			return "", ErrInvalidResult
		}
	}
	status := String(m, "status")
	if !yes && status != flag {
		return "", ErrInvalidResult
	}
	return id, nil
}

func TextField(m map[string]json.RawMessage, limit int, keys ...string) string {
	return untrusted.Line(String(m, keys...), limit)
}

// Strings validates optional string fields rather than silently turning a
// provider's nested/unsupported shape into an empty display value.
func Strings(m map[string]json.RawMessage, keys ...string) error {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			var value string
			if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
				return ErrInvalidResult
			}
		}
	}
	return nil
}

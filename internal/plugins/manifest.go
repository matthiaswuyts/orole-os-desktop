package plugins

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

const maxManifestBytes = 64 << 10

var (
	pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)+$`)
	localIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	toolIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func invalid(detail string) error { return fmt.Errorf("%w: %s", ErrInvalid, detail) }

// UnmarshalJSON rejects unknown executable/permission fields and duplicate JSON
// keys, rather than silently discarding data the importer thought it reviewed.
func (m *PluginManifest) UnmarshalJSON(raw []byte) error {
	if len(raw) > maxManifestBytes {
		return invalid("manifest exceeds 64 KiB")
	}
	if _, err := decodeJSON(raw); err != nil {
		return err
	}
	type wire PluginManifest
	var out wire
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return invalid("manifest contains unknown fields or invalid field types")
	}
	*m = PluginManifest(out)
	return nil
}

// decodeJSON bounds nesting and rejects repeated keys at every object depth.
// UseNumber preserves exact argument numbers instead of rounding through float64.
func decodeJSON(raw []byte) (any, error) {
	if len(raw) == 0 || len(raw) > maxManifestBytes || !utf8.Valid(raw) {
		return nil, invalid("JSON must be valid UTF-8 and at most 64 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value func(int) (any, error)
	value = func(depth int) (any, error) {
		if depth > 64 {
			return nil, invalid("JSON nesting exceeds 64 levels")
		}
		token, err := dec.Token()
		if err != nil {
			return nil, invalid("malformed JSON")
		}
		switch token {
		case json.Delim('{'):
			out := make(map[string]any)
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return nil, invalid("malformed JSON object")
				}
				name, ok := key.(string)
				if !ok {
					return nil, invalid("invalid JSON object key")
				}
				if _, exists := out[name]; exists {
					return nil, invalid("duplicate JSON object key")
				}
				item, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				out[name] = item
			}
			if end, err := dec.Token(); err != nil || end != json.Delim('}') {
				return nil, invalid("malformed JSON object")
			}
			return out, nil
		case json.Delim('['):
			out := []any{}
			for dec.More() {
				item, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
			if end, err := dec.Token(); err != nil || end != json.Delim(']') {
				return nil, invalid("malformed JSON array")
			}
			return out, nil
		case json.Delim('}'), json.Delim(']'):
			return nil, invalid("unexpected JSON delimiter")
		default:
			return token, nil
		}
	}
	out, err := value(0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, invalid("JSON must contain exactly one value")
	}
	return out, nil
}

func validText(s string, limit int, required bool) bool {
	return len(s) <= limit && utf8.ValidString(s) && (!required || strings.TrimSpace(s) != "") &&
		!strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) &&
		untrusted.Line(s, 0) == s
}

// Leave room for "plugin:" inside policy's 128-byte actor identity limit.
func validPluginID(id string) bool { return len(id) <= 120 && pluginIDPattern.MatchString(id) }
func validLocalID(id string) bool  { return len(id) <= 64 && localIDPattern.MatchString(id) }
func validToolID(id string) bool   { return len(id) <= 128 && toolIDPattern.MatchString(id) }

func validRef(id, digest string) bool {
	return validPluginID(id) && digestPattern.MatchString(digest)
}

func validateManifest(in PluginManifest) (PluginManifest, []byte, string, error) {
	if in.SchemaVersion != 1 || !validPluginID(in.ID) {
		return in, nil, "", invalid("schemaVersion must be 1 and id must be a namespaced lowercase identifier")
	}
	if !validText(in.Name, 160, true) || !validText(in.Version, 64, true) || !validText(in.Description, 1024, false) {
		return in, nil, "", invalid("invalid name, version or description")
	}
	if len(in.Capabilities) < 1 || len(in.Capabilities) > 16 || len(in.Actions) < 1 || len(in.Actions) > 32 ||
		len(in.Commands) > 32 || len(in.Widgets) > 16 || len(in.Commands)+len(in.Widgets) == 0 {
		return in, nil, "", invalid("requires 1–16 capabilities, 1–32 actions and at least one contribution (up to 32 commands and 16 widgets)")
	}
	out := in
	out.Capabilities = append([]PluginCapability{}, in.Capabilities...)
	out.Actions = append([]PluginAction{}, in.Actions...)
	out.Commands = append([]PluginCommand{}, in.Commands...)
	out.Widgets = append([]PluginWidget{}, in.Widgets...)
	capabilities := make(map[PluginCapability]bool, len(out.Capabilities))
	for _, capability := range out.Capabilities {
		if !validToolID(capability.ServerID) || !validToolID(capability.Tool) || capabilities[capability] {
			return out, nil, "", invalid("capabilities must contain unique, valid server/tool pairs")
		}
		capabilities[capability] = true
	}
	actions := make(map[string]bool, len(out.Actions))
	for i, action := range out.Actions {
		if !validLocalID(action.ID) || actions[action.ID] || !validText(action.Title, 160, true) || !validText(action.Description, 1024, false) {
			return out, nil, "", invalid("actions must have unique valid IDs and plain-text metadata")
		}
		if !capabilities[PluginCapability{ServerID: action.ServerID, Tool: action.Tool}] {
			return out, nil, "", invalid("action uses an undeclared server/tool capability")
		}
		value, err := decodeJSON(action.Arguments)
		if err != nil {
			return out, nil, "", err
		}
		if _, ok := value.(map[string]any); !ok {
			return out, nil, "", invalid("action arguments must be a JSON object")
		}
		out.Actions[i].Arguments, err = json.Marshal(value)
		if err != nil {
			return out, nil, "", invalid("action arguments cannot be encoded")
		}
		actions[action.ID] = true
	}
	commands := make(map[string]bool, len(out.Commands))
	for _, command := range out.Commands {
		if !validLocalID(command.ID) || commands[command.ID] || !actions[command.ActionID] ||
			!validText(command.Title, 160, true) || !validText(command.Description, 1024, false) {
			return out, nil, "", invalid("commands require unique valid IDs, plain-text metadata and a known action")
		}
		commands[command.ID] = true
	}
	widgets := make(map[string]bool, len(out.Widgets))
	for _, widget := range out.Widgets {
		if !validLocalID(widget.ID) || widgets[widget.ID] || !actions[widget.ActionID] ||
			!validText(widget.Title, 160, true) || !validText(widget.Description, 1024, false) {
			return out, nil, "", invalid("widgets require unique valid IDs, plain-text metadata and a known action")
		}
		widgets[widget.ID] = true
	}
	// Keep every declared capability visible inside policy's 2000-byte prompt;
	// the approval can never authorize a capability hidden by truncation.
	if len(trustDetail(out, strings.Repeat("0", 64))) > 1900 {
		return out, nil, "", invalid("capability list is too long to display completely in the approval prompt")
	}
	canonical, err := json.Marshal(out)
	if err != nil || len(canonical) > maxManifestBytes {
		return out, nil, "", invalid("manifest exceeds 64 KiB or cannot be encoded")
	}
	sum := sha256.Sum256(canonical)
	return out, canonical, hex.EncodeToString(sum[:]), nil
}

func trustDetail(manifest PluginManifest, digest string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Enable declarative plugin %s (%s).\nSHA-256: %s\nDeclared MCP capabilities:", manifest.ID, manifest.Name, digest)
	for _, capability := range manifest.Capabilities {
		fmt.Fprintf(&out, "\n- %s / %s", capability.ServerID, capability.Tool)
	}
	out.WriteString("\nThis trusts only this manifest. Every tool call still requires MCP authorization. No plugin code or credentials are loaded.")
	return out.String()
}

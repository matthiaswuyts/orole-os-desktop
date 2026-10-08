package plugins

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func manifestFixture() PluginManifest {
	return PluginManifest{
		SchemaVersion: 1, ID: "example.status", Name: "Status", Version: "1.0.0",
		Capabilities: []PluginCapability{{ServerID: "local-server", Tool: "status"}},
		Actions:      []PluginAction{{ID: "read", Title: "Read status", ServerID: "local-server", Tool: "status", Arguments: json.RawMessage(`{"scope":"current"}`)}},
		Commands:     []PluginCommand{{ID: "show", Title: "Show status", ActionID: "read"}},
	}
}

func TestManifestRejectsUnsafeOrUnusableDefinitions(t *testing.T) {
	cases := map[string]func(*PluginManifest){
		"schema":                  func(m *PluginManifest) { m.SchemaVersion = 2 },
		"unqualified id":          func(m *PluginManifest) { m.ID = "status" },
		"policy actor truncation": func(m *PluginManifest) { m.ID = "example." + strings.Repeat("a", 113) },
		"control metadata":        func(m *PluginManifest) { m.Name = "Visible\nHidden" },
		"invisible metadata":      func(m *PluginManifest) { m.Actions[0].Title = "Safe\u202eevil" },
		"empty manifest":          func(m *PluginManifest) { m.Commands = nil },
		"undeclared tool":         func(m *PluginManifest) { m.Actions[0].Tool = "delete" },
		"undeclared server":       func(m *PluginManifest) { m.Actions[0].ServerID = "other-server" },
		"duplicate capability":    func(m *PluginManifest) { m.Capabilities = append(m.Capabilities, m.Capabilities[0]) },
		"duplicate action":        func(m *PluginManifest) { m.Actions = append(m.Actions, m.Actions[0]) },
		"duplicate command":       func(m *PluginManifest) { m.Commands = append(m.Commands, m.Commands[0]) },
		"duplicate widget": func(m *PluginManifest) {
			w := PluginWidget{ID: "status", Title: "Status", ActionID: "read"}
			m.Widgets = []PluginWidget{w, w}
		},
		"unknown command action": func(m *PluginManifest) { m.Commands[0].ActionID = "missing" },
		"unknown widget action": func(m *PluginManifest) {
			m.Widgets = []PluginWidget{{ID: "status", Title: "Status", ActionID: "missing"}}
		},
		"array arguments":           func(m *PluginManifest) { m.Actions[0].Arguments = json.RawMessage(`[]`) },
		"null arguments":            func(m *PluginManifest) { m.Actions[0].Arguments = json.RawMessage(`null`) },
		"trailing JSON":             func(m *PluginManifest) { m.Actions[0].Arguments = json.RawMessage(`{} {}`) },
		"duplicate nested argument": func(m *PluginManifest) { m.Actions[0].Arguments = json.RawMessage(`{"filter":{"safe":1,"safe":2}}`) },
		"excessive nesting": func(m *PluginManifest) {
			m.Actions[0].Arguments = json.RawMessage(`{"nested":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`)
		},
		"oversized manifest": func(m *PluginManifest) {
			m.Actions[0].Arguments = json.RawMessage(`{"text":"` + strings.Repeat("a", maxManifestBytes-10) + `"}`)
		},
		"incomplete approval prompt": func(m *PluginManifest) {
			for i := range 15 {
				m.Capabilities = append(m.Capabilities, PluginCapability{ServerID: strings.Repeat("s", 127) + string(rune('a'+i)), Tool: strings.Repeat("t", 128)})
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := manifestFixture()
			mutate(&manifest)
			if _, _, _, err := validateManifest(manifest); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid manifest accepted: %v", err)
			}
		})
	}
}

func TestCanonicalIdentityPreservesExactArguments(t *testing.T) {
	first := manifestFixture()
	first.Actions[0].Arguments = json.RawMessage(`{ "z":9007199254740993, "a":{"y":2,"x":1} }`)
	second := manifestFixture()
	second.Actions[0].Arguments = json.RawMessage(`{"a":{"x":1,"y":2},"z":9007199254740993}`)
	one, raw, digest, err := validateManifest(first)
	if err != nil {
		t.Fatal(err)
	}
	_, otherRaw, otherDigest, err := validateManifest(second)
	if err != nil || digest != otherDigest || string(raw) != string(otherRaw) {
		t.Fatalf("equivalent object order/whitespace changed identity: %v", err)
	}
	if one.Widgets == nil || !strings.Contains(string(one.Actions[0].Arguments), "9007199254740993") {
		t.Fatal("canonicalization dropped empty array or rounded an integer")
	}
	second.Actions[0].Arguments = json.RawMessage(`{"a":{"x":1,"y":2},"z":9007199254740992}`)
	_, _, changedDigest, err := validateManifest(second)
	if err != nil || digest == changedDigest {
		t.Fatalf("changed integer did not change trust identity: %v", err)
	}
}

func TestManifestJSONRejectsHiddenUnknownAndDuplicateFields(t *testing.T) {
	for _, raw := range []string{
		`{"id":"example.safe","id":"example.other"}`,
		`{"schemaVersion":1,"code":"execute arbitrary code"}`,
		`{"capabilities":[{"serverId":"server","tool":"tool","readOnly":true}]}`,
		`{"actions":[{"id":"a","arguments":{"mode":"read","mode":"delete"}}]}`,
	} {
		var manifest PluginManifest
		if err := json.Unmarshal([]byte(raw), &manifest); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ambiguous/unknown JSON accepted: %v", err)
		}
	}
}

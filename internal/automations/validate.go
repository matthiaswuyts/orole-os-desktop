package automations

import (
	"encoding/json"
	"fmt"
	"github.com/rayorole/orole-os-desktop/internal/automations/expr"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var stepID = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

func field(k, label, kind string, required bool) Field {
	return Field{Key: k, Label: label, Kind: kind, Required: required, Options: []string{}}
}
func choice(k, label string, opts ...string) Field {
	return Field{Key: k, Label: label, Kind: "enum", Options: opts, Required: true}
}
func catalog() []StepType {
	command := []Field{field("executable", "Executable", "string", true), field("args", "Arguments (one per line)", "list", false), field("dir", "Absolute working directory", "string", true), field("env", "Additional environment", "map", false)}
	return []StepType{
		{Type: "command.run", Label: "Run command", Available: true, SideEffect: true, Fields: command},
		{Type: "test.run", Label: "Run tests", Available: true, SideEffect: true, Fields: command},
		{Type: "mcp.tool", Label: "Invoke MCP tool", SideEffect: true, Fields: []Field{field("server", "Server", "string", true), field("tool", "Tool", "string", true), field("args", "Arguments", "json", false)}},
		{Type: "agent.start", Label: "Start agent", SideEffect: true, KeySupported: true, Fields: []Field{field("projectId", "Project ID", "string", true), field("adapter", "Adapter", "string", true), field("prompt", "Prompt", "text", true)}},
		{Type: "agent.prompt", Label: "Send agent prompt", SideEffect: true, KeySupported: true, Fields: []Field{field("agentId", "Agent ID", "string", true), field("prompt", "Prompt", "text", true)}},
		{Type: "agent.wait", Label: "Wait for agent", Fields: []Field{field("agentId", "Agent ID", "string", true)}},
		{Type: "github.query", Label: "Query GitHub", Fields: []Field{choice("kind", "Kind", "issues", "pulls", "checks", "search"), field("repo", "Owner/repository", "string", true), field("query", "Query", "string", false), field("limit", "Maximum results", "number", false)}},
		{Type: "notify", Label: "Send notification", SideEffect: true, Fields: []Field{field("title", "Title", "string", true), field("body", "Body", "text", false), choice("level", "Level", "info", "success", "warning", "error")}},
		{Type: "branch", Label: "Conditional branch", Available: true, Fields: []Field{field("condition", "Condition", "expr", true), field("then", "If true: step ID or end", "string", true), field("else", "If false: step ID or end", "string", true)}},
		{Type: "approval", Label: "Human approval", Available: true, Fields: []Field{field("message", "Approval message", "text", true), field("expiresHours", "Expires after hours (0: no expiry)", "number", false)}},
		{Type: "http.request", Label: "HTTP request", Available: true, SideEffect: true, KeySupported: true, Fields: []Field{choice("method", "Method", "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"), field("url", "URL", "string", true), field("headers", "Headers", "map", false), field("body", "Body", "text", false)}},
	}
}
func spec(kind string) (StepType, bool) {
	for _, s := range catalog() {
		if s.Type == kind {
			return s, true
		}
	}
	return StepType{}, false
}
func Normalize(d Definition) Definition {
	if d.AllowedExecutables == nil {
		d.AllowedExecutables = []string{}
	}
	if d.AllowedHosts == nil {
		d.AllowedHosts = []string{}
	}
	if d.Steps == nil {
		d.Steps = []Step{}
	}
	if d.SchemaVersion == 0 {
		d.SchemaVersion = SchemaVersion
	}
	if d.TimeoutSec == 0 {
		d.TimeoutSec = 3600
	}
	if d.Trigger.Type == "" {
		d.Trigger.Type = "manual"
	}
	if d.Trigger.CatchUp == "" {
		d.Trigger.CatchUp = "skip"
	}
	for i := range d.Steps {
		s := &d.Steps[i]
		if s.ID == "" {
			s.ID = fmt.Sprintf("s%d", i+1)
		}
		if s.TimeoutSec == 0 {
			s.TimeoutSec = 300
		}
		if s.BackoffMs == 0 {
			s.BackoffMs = 1000
		}
		if s.Strategy == "" {
			s.Strategy = "at-most-once"
		}
		if s.OnError == "" {
			s.OnError = "stop"
		}
		if len(s.Params) == 0 {
			s.Params = json.RawMessage(`{}`)
		}
	}
	return d
}
func Validate(d Definition) []Issue {
	out := []Issue{}
	add := func(p, m string) { out = append(out, Issue{p, m}) }
	if d.SchemaVersion != SchemaVersion {
		add("schemaVersion", "Unsupported schema version")
	}
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > 120 {
		add("name", "Name is required (maximum 120 bytes)")
	}
	if len(d.Steps) == 0 || len(d.Steps) > 50 {
		add("steps", "Use between 1 and 50 steps")
	}
	if d.TimeoutSec < 1 || d.TimeoutSec > 86400 {
		add("timeoutSec", "Use 1–86400 seconds")
	}
	if d.Concurrency < 0 || d.Concurrency > 32 {
		add("concurrency", "Use 0 (default) or 1–32")
	}
	switch d.Trigger.Type {
	case "manual", "webhook":
	case "schedule":
		if _, err := ParseSchedule(d.Trigger.Schedule, nil); err != nil {
			add("trigger.schedule", err.Error())
		}
	case "event":
		if !events.ValidPattern(d.Trigger.Event) {
			add("trigger.event", "Use a topic or prefix.*")
		}
	default:
		add("trigger.type", "Unknown trigger")
	}
	if d.Trigger.CatchUp != "skip" && d.Trigger.CatchUp != "run-once" {
		add("trigger.catchUp", "Use skip or run-once")
	}
	if d.Trigger.Filter != "" {
		if _, err := expr.Parse(d.Trigger.Filter); err != nil {
			add("trigger.filter", err.Error())
		}
	}
	ids := map[string]int{}
	for i, s := range d.Steps {
		if _, ok := ids[s.ID]; ok {
			add(fmt.Sprintf("steps.%d.id", i), "Duplicate step ID")
		}
		ids[s.ID] = i
	}
	for i, s := range d.Steps {
		p := fmt.Sprintf("steps.%d", i)
		if !stepID.MatchString(s.ID) {
			add(p+".id", "Use a letter followed by letters, digits, underscore or hyphen")
		}
		sp, ok := spec(s.Type)
		if !ok {
			add(p+".type", "Unknown step type")
			continue
		}
		if s.TimeoutSec < 1 || s.TimeoutSec > 86400 {
			add(p+".timeoutSec", "Use 1–86400 seconds")
		}
		if s.Retries < 0 || s.Retries > 8 {
			add(p+".retries", "Use 0–8 retries")
		}
		if s.BackoffMs < 0 || s.BackoffMs > 60000 {
			add(p+".backoffMs", "Use 0–60000 milliseconds")
		}
		if s.OnError != "stop" && s.OnError != "continue" {
			add(p+".onError", "Use stop or continue")
		}
		switch s.Strategy {
		case "at-most-once":
			if sp.SideEffect && s.Retries > 0 {
				add(p+".strategy", "Retries require key or an explicit repeatable strategy")
			}
		case "key":
			if !sp.KeySupported {
				add(p+".strategy", "This executor does not support idempotency keys")
			}
		case "repeatable":
		default:
			add(p+".strategy", "Choose at-most-once, key, or repeatable")
		}
		if s.When != "" {
			if _, err := expr.Parse(s.When); err != nil {
				add(p+".when", err.Error())
			}
		}
		var m map[string]any
		if err := json.Unmarshal(s.Params, &m); err != nil || m == nil {
			add(p+".params", "Parameters must be an object")
			continue
		}
		for _, issue := range validateParams(sp, m, false) {
			add(p+".params."+issue.Path, issue.Message)
		}
		if s.Type == "branch" {
			for _, key := range []string{"then", "else"} {
				target, _ := m[key].(string)
				if target != "end" {
					j, ok := ids[target]
					if !ok || j <= i {
						add(p+".params."+key, "Branch targets must be a later step ID or end")
					}
				}
			}
		}
		if s.Type == "http.request" {
			rawURL, _ := m["url"].(string)
			if !strings.Contains(rawURL, "{{") {
				u, err := url.Parse(rawURL)
				if err == nil {
					err = validateHTTPURL(u, HTTPPolicy{AllowedHosts: d.AllowedHosts, AllowHTTP: d.AllowHTTP})
				}
				if err != nil {
					add(p+".params.url", err.Error())
				}
			}
			for name := range stringsMap(m["headers"]) {
				if credentialHTTPHeader(name) || strings.EqualFold(name, "Host") {
					add(p+".params.headers", "Credential and Host headers are not allowed")
				}
			}
		}
		if s.Type == "command.run" || s.Type == "test.run" {
			x, _ := m["executable"].(string)
			found := false
			for _, a := range d.AllowedExecutables {
				if a == x {
					found = true
				}
			}
			if !found {
				add(p+".params.executable", "Executable must be in this automation's allowlist")
			}
			dir, _ := m["dir"].(string)
			if !strings.Contains(dir, "{{") && !filepath.IsAbs(dir) {
				add(p+".params.dir", "Working directory must be absolute")
			}
			if strings.Contains(x, "{{") {
				add(p+".params.executable", "Executable cannot be templated")
			}
		}
	}
	return out
}
func validateParams(sp StepType, m map[string]any, rendered bool) []Issue {
	issues := []Issue{}
	known := map[string]bool{}
	add := func(k, s string) { issues = append(issues, Issue{k, s}) }
	for _, f := range sp.Fields {
		known[f.Key] = true
		v, ok := m[f.Key]
		if !ok || v == nil {
			if f.Required {
				add(f.Key, "Required")
			}
			continue
		}
		switch f.Kind {
		case "number":
			n, ok := v.(float64)
			if !ok || n < 0 || n > 86400 {
				add(f.Key, "Expected a non-negative number (maximum 86400)")
			}
		case "list":
			a, ok := v.([]any)
			if !ok {
				add(f.Key, "Expected a list of strings")
			} else {
				for _, x := range a {
					if _, ok := x.(string); !ok {
						add(f.Key, "Every item must be a string")
					}
				}
			}
		case "map":
			a, ok := v.(map[string]any)
			if !ok {
				add(f.Key, "Expected an object of strings")
			} else {
				for _, x := range a {
					if _, ok := x.(string); !ok {
						add(f.Key, "Every value must be a string")
					}
				}
			}
		case "json":
			if _, ok := v.(map[string]any); !ok {
				add(f.Key, "Expected an object")
			}
		default:
			s, ok := v.(string)
			if !ok {
				add(f.Key, "Expected text")
				continue
			}
			if f.Required && strings.TrimSpace(s) == "" {
				add(f.Key, "Required")
			}
			if f.Kind == "expr" {
				if _, err := expr.Parse(s); err != nil {
					add(f.Key, err.Error())
				}
			} else if !rendered {
				if _, err := expr.ParseTemplate(s); err != nil {
					add(f.Key, err.Error())
				}
			}
			if f.Kind == "enum" {
				found := false
				for _, o := range f.Options {
					if s == o {
						found = true
					}
				}
				if !found {
					add(f.Key, "Choose "+strings.Join(f.Options, ", "))
				}
			}
		}
	}
	for k := range m {
		if !known[k] {
			add(k, "Unknown parameter")
		}
	}
	if !rendered {
		if _, err := renderValue(m, nil, true, 0); err != nil {
			add("", err.Error())
		}
	}
	return issues
}
func renderValue(v any, env map[string]any, check bool, depth int) (any, error) {
	if depth > 20 {
		return nil, fmt.Errorf("parameters nested too deeply")
	}
	switch x := v.(type) {
	case string:
		t, err := expr.ParseTemplate(x)
		if err != nil {
			return nil, err
		}
		if check {
			return x, nil
		}
		return t.Value(env)
	case []any:
		r := make([]any, len(x))
		for i, v := range x {
			w, err := renderValue(v, env, check, depth+1)
			if err != nil {
				return nil, err
			}
			r[i] = w
		}
		return r, nil
	case map[string]any:
		r := make(map[string]any, len(x))
		for k, v := range x {
			w, err := renderValue(v, env, check, depth+1)
			if err != nil {
				return nil, err
			}
			r[k] = w
		}
		return r, nil
	}
	return v, nil
}

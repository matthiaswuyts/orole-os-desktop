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
func hint(f Field, text string) Field { f.Hint = text; return f }

// catalog lists every step type. Field kinds beyond the primitive ones (string, text,
// expr, number, boolean, list, map, json, enum) are pickers the builder fills with real
// choices: bot, project, harness. They validate as text, so a template may stand in.
// "branch" is the compiled form of a condition (flow.go); it is never offered.
func catalog() []StepType {
	command := []Field{field("executable", "Executable", "string", true), field("args", "Arguments (one per line)", "list", false), field("dir", "Absolute working directory", "string", true), field("env", "Additional environment", "map", false)}
	return []StepType{
		{Type: "bot.tell", Label: "Ask a bot", Group: "Bots", Description: "Send a message to a bot's conversation. It answers with its own instructions, memory and tools.", SideEffect: true, Fields: []Field{
			field("botId", "Bot", "bot", true),
			hint(field("message", "Message", "text", true), "Use {{ event.data.title }} and other event fields. Event text reaches the bot as data, not instructions."),
			hint(field("wait", "Wait for the reply", "boolean", false), "Later blocks can use the reply as steps.<id>.output.reply."),
		}},
		{Type: "task.start", Label: "Start a code task", Group: "Tasks", Description: "Launch a coding agent in a project. Always asks before it starts; never in YOLO mode.", SideEffect: true, KeySupported: true, Fields: []Field{
			field("projectId", "Project", "project", true), field("harness", "Agent", "harness", true), field("prompt", "Task", "text", true),
			field("title", "Title", "string", false), field("model", "Model", "string", false),
			choice("permissionMode", "Permissions", "manual", "auto"), hint(field("newWorktree", "Work in a new worktree", "boolean", false), "Keeps the project's checkout untouched."),
		}},
		{Type: "task.prompt", Label: "Message a code task", Group: "Tasks", Description: "Send a follow-up to a running task.", SideEffect: true, KeySupported: true, Fields: []Field{
			hint(field("taskId", "Task ID", "string", true), "Usually {{ steps.<start>.output.id }} or {{ trigger.data.id }}."), field("prompt", "Message", "text", true),
		}},
		{Type: "task.wait", Label: "Wait for a code task", Group: "Tasks", Description: "Continue when the task finishes, or also when it needs you.", Fields: []Field{
			hint(field("taskId", "Task ID", "string", true), "Usually {{ steps.<start>.output.id }}."), choice("until", "Until", "finished", "attention"),
		}},
		{Type: "notify", Label: "Send notification", Group: "Notify", Description: "Show an Orole notification.", SideEffect: true, Fields: []Field{field("title", "Title", "string", true), field("body", "Body", "text", false), choice("level", "Level", "info", "success", "warning", "error")}},
		{Type: "http.request", Label: "Call webhook", Group: "Notify", Description: "Send an HTTP request to an allowed public host.", Available: true, SideEffect: true, KeySupported: true, Fields: []Field{choice("method", "Method", "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"), field("url", "URL", "string", true), field("headers", "Headers", "map", false), field("body", "Body", "text", false)}},
		{Type: "github.query", Label: "Query GitHub", Group: "Integrations", Description: "Read issues, pull requests, checks or search results.", Fields: []Field{choice("kind", "Kind", "issues", "pulls", "checks", "search"), field("repo", "Owner/repository", "string", true), field("query", "Query", "string", false), field("limit", "Maximum results", "number", false)}},
		{Type: "mcp.tool", Label: "Use an integration tool", Group: "Integrations", Description: "Call a tool of a connected MCP integration (Vercel, Railway, Linear…).", SideEffect: true, Fields: []Field{field("server", "Server", "string", true), field("tool", "Tool", "string", true), field("args", "Arguments", "json", false)}},
		{Type: "condition", Label: "Condition", Group: "Logic", Description: "Run one lane or the other, then continue.", Available: true, Fields: []Field{hint(field("condition", "Condition", "expr", true), "For example event.data.status == 'failed'.")}},
		{Type: "delay", Label: "Wait", Group: "Logic", Description: "Pause the flow for a number of minutes.", Available: true, Fields: []Field{field("minutes", "Minutes", "number", true)}},
		{Type: "approval", Label: "Ask me first", Group: "Logic", Description: "Pause until you approve or reject in Orole.", Available: true, Fields: []Field{field("message", "Approval message", "text", true), field("expiresHours", "Expires after hours (0: no expiry)", "number", false)}},
		{Type: "command.run", Label: "Run command", Group: "Run", Description: "Run an allowlisted executable.", Available: true, SideEffect: true, Fields: command},
		{Type: "test.run", Label: "Run tests", Group: "Run", Description: "Run an allowlisted test command; failure fails the step.", Available: true, SideEffect: true, Fields: command},
		{Type: "branch", Label: "Jump", Group: "Logic", Available: true, Fields: []Field{field("condition", "Condition", "expr", true), field("then", "If true: step ID or end", "string", true), field("else", "If false: step ID or end", "string", true)}},
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
	used := map[string]bool{}
	walk(d.Steps, "steps", 0, func(s Step, _ string, _ int) { used[s.ID] = s.ID != "" })
	n := 0
	d.Steps = normalizeSteps(d.Steps, used, &n)
	return d
}

// normalizeSteps fills defaults in a copy of steps (lanes included) and gives every
// step without an ID a fresh one ("s1", "s2"...) unused anywhere in the flow.
func normalizeSteps(steps []Step, used map[string]bool, n *int) []Step {
	if steps == nil {
		return nil
	}
	out := make([]Step, len(steps))
	for i, s := range steps {
		for s.ID == "" {
			*n++
			if id := fmt.Sprintf("s%d", *n); !used[id] {
				s.ID, used[id] = id, true
			}
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
		if s.OnError == "" || s.Type == "condition" {
			// A failed condition must not fall into a lane: it always stops the run.
			s.OnError = "stop"
		}
		if len(s.Params) == 0 {
			s.Params = json.RawMessage(`{}`)
		}
		if s.Type == "delay" {
			var p struct {
				Minutes float64 `json:"minutes"`
			}
			// The step timeout bounds the wait; keep it just above the requested delay.
			if json.Unmarshal(s.Params, &p) == nil && p.Minutes > 0 {
				if need := int(p.Minutes*60) + 60; s.TimeoutSec < need {
					s.TimeoutSec = need
				}
			}
		}
		s.Then = normalizeSteps(s.Then, used, n)
		s.Else = normalizeSteps(s.Else, used, n)
		out[i] = s
	}
	return out
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
	count := 0
	walk(d.Steps, "steps", 0, func(Step, string, int) { count++ })
	if count == 0 || count > MaxSteps {
		add("steps", fmt.Sprintf("Use between 1 and %d blocks, lanes included", MaxSteps))
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
	ids := map[string]bool{}
	walk(d.Steps, "steps", 0, func(s Step, p string, _ int) {
		if ids[s.ID] {
			add(p+".id", "Duplicate step ID")
		}
		ids[s.ID] = true
	})
	walk(d.Steps, "steps", 0, func(s Step, p string, depth int) {
		if depth > MaxLaneDepth {
			add(p, fmt.Sprintf("Conditions can nest at most %d deep", MaxLaneDepth))
		}
		if !stepID.MatchString(s.ID) {
			add(p+".id", "Use a letter followed by letters, digits, underscore or hyphen")
		}
		sp, ok := spec(s.Type)
		if !ok {
			add(p+".type", "Unknown step type")
			return
		}
		if s.Type == "branch" {
			add(p+".type", "This jump came from an older automation and has no lane equivalent. Replace it with a Condition block.")
			return
		}
		if s.Type == "condition" {
			if s.When != "" {
				add(p+".when", "A condition is its own guard; put the test in its condition")
			}
			if len(s.Then)+len(s.Else) == 0 {
				add(p, "Add a block to at least one lane")
			}
		} else if len(s.Then)+len(s.Else) > 0 {
			add(p, "Only a condition has lanes")
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
			return
		}
		for _, issue := range validateParams(sp, m, false) {
			add(p+".params."+issue.Path, issue.Message)
		}
		if s.Type == "delay" {
			if minutes, _ := m["minutes"].(float64); minutes < 1 || minutes > 1440 {
				add(p+".params.minutes", "Use 1–1440 minutes")
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
	})
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
		case "boolean":
			if _, ok := v.(bool); !ok {
				add(f.Key, "Expected true or false")
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

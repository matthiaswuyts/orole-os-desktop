package automations

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func cond(id, expr string, then, els []Step) Step {
	return Step{ID: id, Type: "condition", Params: mustJSON(map[string]any{"condition": expr}), Then: then, Else: els}
}

// recordTools records the step IDs the MCP adapter was asked to run, in order.
func recordTools(e *Engine) func() []string {
	var mu sync.Mutex
	var seen []string
	e.SetToolInvoker(toolFunc(func(_ context.Context, r ToolRequest) (any, error) {
		mu.Lock()
		seen = append(seen, r.Args["step"].(string))
		mu.Unlock()
		return map[string]any{"ok": true}, nil
	}))
	return func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(seen) }
}

func tool(id string) Step {
	return Step{ID: id, Type: "mcp.tool", Params: mustJSON(map[string]any{"server": "fake", "tool": "t", "args": map[string]any{"step": id}})}
}

func TestConditionLanesRunTheChosenLaneThenContinue(t *testing.T) {
	flow := func(x string) []Step {
		return []Step{
			tool("first"),
			cond("pick", "event.data.x == '"+x+"'", []Step{
				tool("yesA"),
				cond("inner", "steps.yesA.status == 'succeeded'", []Step{tool("innerYes")}, []Step{tool("innerNo")}),
			}, []Step{tool("noA")}),
			tool("last"),
		}
	}
	cases := []struct {
		name string
		x    string
		want []string
	}{
		{"then lane with nested condition", "a", []string{"first", "yesA", "innerYes", "last"}},
		{"else lane", "b", []string{"first", "noA", "last"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
			seen := recordTools(e)
			d := saveDef(t, e, Definition{Name: "lanes", Enabled: true, Trigger: Trigger{Type: "event", Event: "x.go"}, Steps: flow("a")})
			r, err := e.trigger(context.Background(), d.ID, "event", map[string]any{"topic": "x.go", "data": map[string]any{"x": c.x}}, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			detail := awaitStatus(t, e, r.ID, "succeeded")
			if got := seen(); !slices.Equal(got, c.want) {
				t.Fatalf("ran %v, want %v", got, c.want)
			}
			for _, j := range detail.Steps {
				if j.StepID == "pick" && j.Type != "branch" {
					t.Fatalf("condition journal type %q", j.Type)
				}
			}
		})
	}
}

func TestEmptyThenLaneSkipsToTheNextStep(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	seen := recordTools(e)
	r := runDef(t, e, Definition{Name: "else only", Steps: []Step{cond("c", "true", nil, []Step{tool("no")}), tool("after")}})
	awaitStatus(t, e, r.ID, "succeeded")
	if got := seen(); !slices.Equal(got, []string{"after"}) {
		t.Fatalf("ran %v", got)
	}
}

func TestUpgradeV1(t *testing.T) {
	branch := func(id, then, els string) Step {
		return Step{ID: id, Type: "branch", Params: mustJSON(map[string]any{"condition": "x", "then": then, "else": els})}
	}
	ids := func(steps []Step) string {
		var b strings.Builder
		walk(steps, "s", 0, func(s Step, _ string, depth int) {
			b.WriteString(strings.Repeat(">", depth) + s.ID + ":" + s.Type + " ")
		})
		return strings.TrimSpace(b.String())
	}
	cases := []struct {
		name  string
		steps []Step
		want  string
	}{
		{"then next, else end", []Step{tool("a"), branch("b", "c", "end"), tool("c"), tool("d")}, "a:mcp.tool b:condition >c:mcp.tool >d:mcp.tool"},
		{"then next, else later", []Step{branch("b", "c", "e"), tool("c"), tool("d"), tool("e")}, "b:condition >c:mcp.tool >d:mcp.tool e:mcp.tool"},
		{"else next, then later", []Step{branch("b", "e", "c"), tool("c"), tool("e")}, "b:condition >c:mcp.tool e:mcp.tool"},
		{"two distant targets stay a jump", []Step{branch("b", "d", "e"), tool("c"), tool("d"), tool("e")}, "b:branch c:mcp.tool d:mcp.tool e:mcp.tool"},
		{"end from a lane that is not the tail stays a jump", []Step{branch("b", "c", "e"), branch("c", "d", "end"), tool("d"), tool("e")}, "b:condition >c:branch >d:mcp.tool e:mcp.tool"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Upgrade(Definition{SchemaVersion: 1, Name: "v1", Steps: c.steps})
			if d.SchemaVersion != SchemaVersion || ids(d.Steps) != c.want {
				t.Fatalf("got v%d %q, want %q", d.SchemaVersion, ids(d.Steps), c.want)
			}
		})
	}
	agent := Upgrade(Definition{SchemaVersion: 1, Steps: []Step{{ID: "w", Type: "agent.wait", Params: json.RawMessage(`{"agentId":"{{ steps.s.output.id }}"}`)}}})
	var p map[string]any
	_ = json.Unmarshal(agent.Steps[0].Params, &p)
	if agent.Steps[0].Type != "task.wait" || p["taskId"] != "{{ steps.s.output.id }}" || p["until"] != "finished" || p["agentId"] != nil {
		t.Fatalf("agent step upgrade: %s %v", agent.Steps[0].Type, p)
	}
}

func TestUpgradedRunKeepsJournalTargets(t *testing.T) {
	// A v1 run snapshot upgraded mid-flight: the compiled branch must still jump to the
	// same step IDs the old journal recorded.
	v1 := []Step{tool("a"), {ID: "b", Type: "branch", Params: mustJSON(map[string]any{"condition": "x", "then": "c", "else": "e"})}, tool("c"), tool("d"), tool("e")}
	flat := flatten(Upgrade(Definition{SchemaVersion: 1, Steps: v1}).Steps)
	var got []string
	for _, s := range flat {
		got = append(got, s.ID)
	}
	if !slices.Equal(got, []string{"a", "b", "c", "d", "e"}) {
		t.Fatalf("flattened %v", got)
	}
	var p map[string]any
	_ = json.Unmarshal(flat[1].Params, &p)
	if p["then"] != "c" || p["else"] != "e" {
		t.Fatalf("branch targets %v", p)
	}
}

func TestValidateFlowShape(t *testing.T) {
	deep := tool("leaf")
	for i := range MaxLaneDepth + 1 {
		deep = cond("c"+string(rune('a'+i)), "true", []Step{deep}, nil)
	}
	cases := []struct {
		name  string
		steps []Step
		path  string
	}{
		{"lanes on a non-condition", []Step{{ID: "a", Type: "notify", Params: json.RawMessage(`{"title":"x","level":"info"}`), Then: []Step{tool("b")}}}, "steps.0"},
		{"condition with empty lanes", []Step{cond("c", "true", nil, nil)}, "steps.0"},
		{"duplicate ID inside a lane", []Step{tool("a"), cond("c", "true", []Step{tool("a")}, nil)}, "steps.1.then.0.id"},
		{"nesting too deep", []Step{deep}, "steps.0.then.0.then.0.then.0.then.0"},
		{"leftover v1 jump", []Step{{ID: "j", Type: "branch", Params: json.RawMessage(`{"condition":"true","then":"end","else":"end"}`)}}, "steps.0.type"},
		{"delay out of range", []Step{{ID: "d", Type: "delay", Params: json.RawMessage(`{"minutes":0}`)}}, "steps.0.params.minutes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := Validate(Normalize(Definition{Name: "x", Steps: c.steps}))
			if !slices.ContainsFunc(issues, func(i Issue) bool { return i.Path == c.path }) {
				t.Fatalf("no issue at %s: %+v", c.path, issues)
			}
		})
	}
	ok := Validate(Normalize(Definition{Name: "fine", Steps: []Step{cond("c", "true", []Step{tool("a")}, []Step{tool("b")}), tool("z")}}))
	if len(ok) != 0 {
		t.Fatalf("valid flow reported %+v", ok)
	}
}

func TestNormalizeGivesLaneStepsUniqueIDs(t *testing.T) {
	d := Normalize(Definition{Steps: []Step{{Type: "notify"}, cond("s1", "true", []Step{{Type: "notify"}}, []Step{{Type: "notify"}})}})
	seen := map[string]bool{}
	walk(d.Steps, "steps", 0, func(s Step, _ string, _ int) {
		if s.ID == "" || seen[s.ID] {
			t.Fatalf("id %q repeated or empty", s.ID)
		}
		seen[s.ID] = true
	})
}

func TestTemplatesAreValidOnceFilledIn(t *testing.T) {
	for _, tpl := range Templates() {
		for _, issue := range Validate(tpl) {
			// Only the picker fields a user must choose may be missing.
			if !strings.HasSuffix(issue.Path, ".repo") && !strings.HasSuffix(issue.Path, ".projectId") && !strings.HasSuffix(issue.Path, ".botId") && !strings.HasSuffix(issue.Path, ".prompt") {
				t.Errorf("%s: %s %s", tpl.Name, issue.Path, issue.Message)
			}
		}
	}
}

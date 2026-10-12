package automations

import (
	"encoding/json"
	"fmt"
)

// A v2 definition is a tree: a "condition" step owns two lanes (Then, Else) and the
// flow continues after it once the chosen lane ends. The runner executes a flat list,
// so flatten compiles the tree into v1-style forward jumps:
//
//	condition c {then A B} {else D}; E   =>   branch c(then A, else D), A, B, c:join(→E), D, E
//
// The branch keeps the condition's step ID, so expressions read steps.<id>.output.condition
// and the approval/journal keys stay stable. A join exists only when both lanes have
// steps; its ID contains ':' which an authored step ID cannot, and it is hidden in the UI.

// MaxSteps bounds every step in a flow, lanes included; MaxLaneDepth bounds nesting.
const (
	MaxSteps     = 50
	MaxLaneDepth = 3
	joinSuffix   = ":join"
)

// flatten compiles a step tree into the runner's flat, forward-jumping list.
func flatten(steps []Step) []Step { return compile(steps, "end") }

func compile(steps []Step, after string) []Step {
	out := make([]Step, 0, len(steps))
	for i, s := range steps {
		next := after
		if i+1 < len(steps) {
			next = steps[i+1].ID
		}
		if s.Type != "condition" {
			s.Then, s.Else = nil, nil
			out = append(out, s)
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(s.Params, &p)
		thenStart, elseStart := next, next
		if len(s.Then) > 0 {
			thenStart = s.Then[0].ID
		}
		if len(s.Else) > 0 {
			elseStart = s.Else[0].ID
		}
		b := s
		b.Type, b.Then, b.Else = "branch", nil, nil
		b.Params, _ = json.Marshal(map[string]any{"condition": str(p, "condition"), "then": thenStart, "else": elseStart})
		out = append(out, b)
		out = append(out, compile(s.Then, next)...)
		if len(s.Else) > 0 {
			if len(s.Then) > 0 {
				out = append(out, Step{ID: s.ID + joinSuffix, Type: "branch", TimeoutSec: 300, BackoffMs: 1000, OnError: "stop", Strategy: "at-most-once",
					Params: mustJSON(map[string]any{"condition": "true", "then": next, "else": next})})
			}
			out = append(out, compile(s.Else, next)...)
		}
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// walk visits every step depth-first with its issue path ("steps.2.then.0") and lane depth.
func walk(steps []Step, path string, depth int, fn func(s Step, path string, depth int)) {
	for i, s := range steps {
		p := fmt.Sprintf("%s.%d", path, i)
		fn(s, p, depth)
		walk(s.Then, p+".then", depth+1, fn)
		walk(s.Else, p+".else", depth+1, fn)
	}
}

// Upgrade converts a stored schema v1 definition to v2. Agent steps become task steps
// with renamed parameters. A v1 branch whose "then" (or "else") is the very next step is
// exactly a condition whose lane runs up to the other target; it is converted, lanes
// recursively. Any other jump pattern (two distant targets, a jump out of a lane, "end"
// from inside a lane that is not the flow's tail) has no lane equivalent: the branch is
// kept as is, Validate reports it, and the flow cannot run until it is rebuilt. Nothing
// is silently reinterpreted.
func Upgrade(d Definition) Definition {
	if d.SchemaVersion != 1 {
		return d
	}
	d.Steps = structure(renameV1(d.Steps), true)
	d.SchemaVersion = SchemaVersion
	return d
}

var v1Renames = map[string]struct {
	to     string
	params map[string]string
}{
	"agent.start":  {"task.start", map[string]string{"adapter": "harness"}},
	"agent.prompt": {"task.prompt", map[string]string{"agentId": "taskId"}},
	"agent.wait":   {"task.wait", map[string]string{"agentId": "taskId"}},
}

func renameV1(steps []Step) []Step {
	out := make([]Step, len(steps))
	for i, s := range steps {
		if r, ok := v1Renames[s.Type]; ok {
			var p map[string]any
			if json.Unmarshal(s.Params, &p) == nil && p != nil {
				for from, to := range r.params {
					if v, ok := p[from]; ok {
						p[to] = v
						delete(p, from)
					}
				}
				if r.to == "task.wait" {
					p["until"] = "finished"
				}
				s.Params = mustJSON(p)
			}
			s.Type = r.to
		}
		out[i] = s
	}
	return out
}

// structure turns v1 forward jumps within steps into condition lanes. tail reports that
// the end of steps is the end of the flow, so a jump to "end" may become "run to the end".
func structure(steps []Step, tail bool) []Step {
	index := func(from int, id string) int {
		if id == "end" {
			if tail {
				return len(steps)
			}
			return -1
		}
		for j := from + 1; j < len(steps); j++ {
			if steps[j].ID == id {
				return j
			}
		}
		return -1
	}
	out := []Step{}
	for i := 0; i < len(steps); i++ {
		s := steps[i]
		if s.Type != "branch" {
			out = append(out, s)
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(s.Params, &p)
		t, e := index(i, str(p, "then")), index(i, str(p, "else"))
		cond := s
		cond.Type = "condition"
		cond.Params = mustJSON(map[string]any{"condition": str(p, "condition")})
		switch {
		case t == i+1 && e == i+1:
			continue // both ways continue with the next step: the jump does nothing
		case t == i+1 && e > i:
			cond.Then = structure(steps[i+1:e], tail && e == len(steps))
			i = e - 1
		case e == i+1 && t > i:
			cond.Else = structure(steps[i+1:t], tail && t == len(steps))
			i = t - 1
		default:
			out = append(out, s) // no lane equivalent; Validate reports it
			continue
		}
		out = append(out, cond)
	}
	return out
}

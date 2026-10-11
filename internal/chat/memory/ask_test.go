package memory

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

type askCall struct {
	from   chat.RunContext
	callee string
	task   string
}

type fakeDelegator struct {
	mu    sync.Mutex
	calls []askCall
	fn    func(ctx context.Context, from chat.RunContext, callee, task string) error
}

func (f *fakeDelegator) Handoff(ctx context.Context, from chat.RunContext, callee, task string) error {
	f.mu.Lock()
	f.calls = append(f.calls, askCall{from, callee, task})
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(ctx, from, callee, task)
	}
	return nil
}

func (f *fakeDelegator) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func newAskEnv(t *testing.T) (*env, *fakeDelegator) {
	d := &fakeDelegator{}
	e := newEnv(t, func(c *Config) { c.Delegator = d })
	return e, d
}

func TestAskBotHandsOffAtOnceAndACLHolds(t *testing.T) {
	e, d := newAskEnv(t)
	e.bot("b", "Beta Bot")
	e.bot("c", "Gamma")
	a := e.bot("a", "Alpha", executive)
	r := newRun(a)
	r.Depth = 1

	res := e.call("ask_bot", r, map[string]any{"bot": "b", "question": "  research grok bot "})
	if res.IsError {
		t.Fatal(res.Text)
	}
	contains(t, res.Text, "Handed off to Beta Bot", "report back", "do not wait")
	if d.n() != 1 || d.calls[0].callee != "b" || d.calls[0].task != "research grok bot" || d.calls[0].from.BotID != "a" || d.calls[0].from.Depth != 1 {
		t.Fatalf("delegator calls = %+v", d.calls)
	}
	if got := d.calls[0].from.Chain; !slices.Equal(got, []string{"a"}) {
		t.Fatalf("the delegated run must be told who handed it off: chain = %v", got)
	}
	if len(r.Chain) != 0 {
		t.Fatalf("the asking run's own chain changed: %v", r.Chain)
	}
	parts := r.emitted()
	if len(parts) != 1 || parts[0].Type != chat.PartDelegation || parts[0].Status != "done" || parts[0].Name != "Beta Bot" || parts[0].BotID != "b" {
		t.Fatalf("parts = %+v", parts)
	}

	// By name, case-insensitively.
	if res := e.call("ask_bot", r, map[string]any{"bot": "beta bot", "question": "hi"}); res.IsError {
		t.Fatalf("by name: %s", res.Text)
	}
	// Unknown targets and itself: refused before the delegator is touched, naming the bots it may ask.
	n := d.n()
	for _, target := range []string{"ghost", "a", "Alpha", "../b"} {
		res := e.call("ask_bot", r, map[string]any{"bot": target, "question": "hi"})
		if !res.IsError {
			t.Errorf("asked %q: %s", target, res.Text)
		}
		if !strings.HasSuffix(res.Text, "Allowed: Beta Bot, Gamma") {
			t.Errorf("error should list the other bots: %s", res.Text)
		}
	}
	if d.n() != n {
		t.Error("delegator reached for a forbidden callee")
	}
	// Executive status is read fresh from the config: dropping it takes effect immediately.
	e.setConfig("a", func(c *chat.BotConfig) { c.Executive = false })
	if res := e.call("ask_bot", r, map[string]any{"bot": "b", "question": "hi"}); !res.IsError {
		t.Error("a bot that is no longer an executive assistant can still hand off")
	}
	// Archived callee is unreachable.
	e.setConfig("a", executive)
	e.db.Exec(`UPDATE bots SET archived_at = 1 WHERE id = 'b'`)
	if res := e.call("ask_bot", r, map[string]any{"bot": "b", "question": "hi"}); !res.IsError {
		t.Error("archived callee reachable")
	}
	// Argument validation.
	for _, args := range []map[string]any{{"bot": "b"}, {"bot": "b", "question": "  "}, {"question": "x"}, {"bot": "b", "question": strings.Repeat("x", maxQuestionChars+1)}} {
		if res := e.call("ask_bot", r, args); !res.IsError {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestAskBotReportsAHandoffThatCouldNotStart(t *testing.T) {
	e, d := newAskEnv(t)
	e.bot("b", "Beta")
	a := e.bot("a", "Alpha", executive)
	d.fn = func(context.Context, chat.RunContext, string, string) error {
		return errors.New("Beta is not reachable")
	}
	r := newRun(a)
	res := e.call("ask_bot", r, map[string]any{"bot": "b", "question": "hi"})
	if !res.IsError || !strings.Contains(res.Text, "Beta is not reachable") {
		t.Fatalf("res = %+v", res)
	}
	if parts := r.emitted(); len(parts) != 1 || parts[0].Status != "error" {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestAskBotDepthCap(t *testing.T) {
	e, d := newAskEnv(t)
	e.bot("b", "Beta")
	a := e.bot("a", "Alpha", executive)
	for depth := range maxAskDepth {
		r := newRun(a)
		r.Depth = depth
		if res := e.call("ask_bot", r, map[string]any{"bot": "b", "question": "hi"}); res.IsError {
			t.Errorf("depth %d refused: %s", depth, res.Text)
		}
	}
	r := newRun(a)
	r.Depth = maxAskDepth
	n := d.n()
	res := e.call("ask_bot", r, map[string]any{"bot": "b", "question": "hi"})
	if !res.IsError || !strings.Contains(res.Text, "levels deep") || d.n() != n {
		t.Errorf("depth %d: %+v", maxAskDepth, res)
	}
}

func TestAskBotRefusesACycleThroughTheChain(t *testing.T) {
	e, d := newAskEnv(t)
	e.bot("b", "Beta", executive)
	a := e.bot("a", "Alpha", executive)
	// B, running a task A handed off, tries to hand work back to A.
	rc := newRun(e.loadBot("b"))
	rc.Depth, rc.Chain = 1, []string{a.ID}
	raw, _ := json.Marshal(map[string]any{"bot": "a", "question": "back to you"})
	res, err := e.tool("ask_bot").Call(context.Background(), rc.RunContext, "ask_bot", raw)
	if err != nil || !res.IsError || !strings.Contains(res.Text, "cycle") || d.n() != 0 {
		t.Fatalf("B handing back to A: %+v %v (delegator calls %d)", res, err, d.n())
	}
	// Direct self-delegation, even if configured, is refused and A is never listed as its own target.
	self := e.setConfig("a", executive)
	if res := e.call("ask_bot", newRun(self), map[string]any{"bot": "a", "question": "hi"}); !res.IsError || d.n() != 0 {
		t.Error("bot handed off to itself")
	}
	if specs := e.tool("ask_bot").Specs(self, toolCaps); len(specs) != 1 || strings.Contains(specs[0].Description, "Alpha") {
		t.Errorf("self listed as a target: %+v", specs)
	}
}

func TestAskBotSpecs(t *testing.T) {
	e, _ := newAskEnv(t)
	e.bot("b", "Beta")
	e.bot("c", "Gamma")
	e.bot("z", "Zed")
	e.db.Exec(`UPDATE bots SET archived_at = 1 WHERE id = 'z'`)
	tool := e.tool("ask_bot")

	plain := e.bot("n", "Plain")
	if got := tool.Specs(plain, toolCaps); got != nil {
		t.Errorf("offered to a bot that is not an executive assistant: %+v", got)
	}
	a := e.bot("a", "Alpha", executive)
	specs := tool.Specs(a, toolCaps)
	if len(specs) != 1 || specs[0].Name != "ask_bot" {
		t.Fatalf("specs = %+v", specs)
	}
	contains(t, specs[0].Description, "Beta (id b)", "Gamma (id c)", "Plain (id n)")
	excludes(t, specs[0].Description, "Zed", "Alpha")
	if got := tool.Specs(a, llm.Caps{}); got != nil {
		t.Error("offered to a model without tools")
	}
	// Nobody else to ask: not offered ("a tool must never be offered if it cannot run").
	e3, _ := newAskEnv(t)
	alone := e3.bot("a", "Alone", executive)
	if got := e3.tool("ask_bot").Specs(alone, toolCaps); got != nil {
		t.Errorf("offered with no other bot: %+v", got)
	}
	// No engine attached: not offered either.
	e2 := newEnv(t)
	e2.bot("b", "Beta")
	a2 := e2.bot("a", "Alpha", executive)
	if got := e2.tool("ask_bot").Specs(a2, toolCaps); got != nil {
		t.Errorf("offered without a delegator: %+v", got)
	}
	e2.s.SetDelegator(&fakeDelegator{})
	if got := e2.tool("ask_bot").Specs(a2, toolCaps); got == nil {
		t.Error("not offered after the delegator was attached")
	}
}

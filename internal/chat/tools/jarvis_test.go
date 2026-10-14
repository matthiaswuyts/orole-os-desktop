package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

type fakeJarvis struct {
	added    []JarvisRule
	called   []string
	switched []string
	ended    []string
}

func (f *fakeJarvis) AddRule(_ context.Context, botID string, r JarvisRule) (JarvisRule, error) {
	r.ID = "r1"
	f.added = append(f.added, r)
	return r, nil
}
func (f *fakeJarvis) Rules(context.Context, string) ([]JarvisRule, error) { return f.added, nil }
func (f *fakeJarvis) DeleteRule(context.Context, string, string) error    { return nil }
func (f *fakeJarvis) CallOwner(_ context.Context, botID, reason string, urgent bool) (string, error) {
	f.called = append(f.called, reason)
	return "Calling now.", nil
}
func (f *fakeJarvis) SwitchBot(_ context.Context, callID, name string) (string, error) {
	f.switched = append(f.switched, name)
	return "Switched to Nova.", nil
}

func (f *fakeJarvis) EndCall(_ context.Context, callID string) error {
	f.ended = append(f.ended, callID)
	return nil
}

func names(specs []llm.ToolSpec) []string {
	var out []string
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

func TestJarvisSpecsGating(t *testing.T) {
	j := Jarvis{Runtime: &fakeJarvis{}}
	off := chat.Bot{ID: "b1"}
	if specs := j.Specs(off, llm.Caps{Tools: true}); specs != nil {
		t.Fatalf("offered to a bot without Jarvis: %v", names(specs))
	}
	on := chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}
	got := strings.Join(names(j.Specs(on, llm.Caps{Tools: true})), ",")
	if got != "jarvis_rule_add,jarvis_rule_list,jarvis_rule_delete,call_owner,switch_bot,end_call" {
		t.Fatalf("specs %s", got)
	}
}

func TestJarvisRuleAddParsesArgs(t *testing.T) {
	f := &fakeJarvis{}
	j := Jarvis{Runtime: f}
	rc := chat.RunContext{BotID: "b1", Bot: chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}}
	args, _ := json.Marshal(map[string]any{"trigger": "task_finished", "target": "t1", "note": "deploy", "repeat": false})
	res, err := j.Call(context.Background(), rc, "jarvis_rule_add", args)
	if err != nil || res.IsError || len(f.added) != 1 || f.added[0].Target != "t1" {
		t.Fatalf("res=%+v err=%v added=%+v", res, err, f.added)
	}
}

func TestSwitchBotOnlyOnCalls(t *testing.T) {
	f := &fakeJarvis{}
	j := Jarvis{Runtime: f}
	rc := chat.RunContext{BotID: "b1", Bot: chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}}
	args := json.RawMessage(`{"name":"Nova"}`)
	res, _ := j.Call(context.Background(), rc, "switch_bot", args)
	if !res.IsError || len(f.switched) != 0 {
		t.Fatal("switch_bot worked outside a call")
	}
	rc.CallID = "c1"
	res, _ = j.Call(context.Background(), rc, "switch_bot", args)
	if res.IsError || len(f.switched) != 1 {
		t.Fatalf("switch_bot on a call failed: %+v", res)
	}
}

func TestCallOwnerRefusedDuringCall(t *testing.T) {
	f := &fakeJarvis{}
	j := Jarvis{Runtime: f}
	rc := chat.RunContext{BotID: "b1", CallID: "c1", Bot: chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}}
	res, _ := j.Call(context.Background(), rc, "call_owner", json.RawMessage(`{"reason":"x"}`))
	if !res.IsError || len(f.called) != 0 {
		t.Fatal("call_owner placed a call while already on one")
	}
}

func TestJarvisPromptOnlyWhenEnabled(t *testing.T) {
	if got := JarvisPrompt(chat.Bot{ID: "b1"}); got != "" {
		t.Fatalf("prompt for a bot without Jarvis: %q", got)
	}
	on := chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true, Policy: "only failures"}}}
	got := JarvisPrompt(on)
	if !strings.Contains(got, "only failures") || !strings.Contains(got, `<untrusted source="owner policy">`) {
		t.Fatalf("policy not fenced in prompt: %q", got)
	}
}

func TestJarvisEndCallOnlyOnACall(t *testing.T) {
	f := &fakeJarvis{}
	j := Jarvis{Runtime: f}
	bot := chat.Bot{ID: "b1", Config: chat.BotConfig{Jarvis: chat.BotJarvis{Enabled: true}}}
	if res, _ := j.Call(context.Background(), chat.RunContext{BotID: "b1", Bot: bot}, "end_call", json.RawMessage(`{}`)); !res.IsError {
		t.Fatalf("end_call outside a call: %+v", res)
	}
	if res, _ := j.Call(context.Background(), chat.RunContext{BotID: "b1", Bot: bot, CallID: "c1"}, "end_call", json.RawMessage(`{}`)); res.IsError {
		t.Fatalf("end_call on a call: %+v", res)
	}
	if len(f.ended) != 1 || f.ended[0] != "c1" {
		t.Fatalf("ended %v", f.ended)
	}
}

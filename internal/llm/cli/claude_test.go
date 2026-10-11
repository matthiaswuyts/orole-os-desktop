package cli

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func claudeLine(text string) string {
	return `{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":` + jsonString(text) + `}}}` + "\n"
}

func jsonString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

const claudeResult = `{"type":"result","subtype":"success","is_error":false,"result":"ok","usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":20,"output_tokens":7},"total_cost_usd":0.001}` + "\n"

func TestClaudeStreamsTextAsItArrivesAndRunsAsAPlainModel(t *testing.T) {
	r := &run{out: `{"type":"system","subtype":"init"}` + "\n" +
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":""}}}` + "\n" +
		claudeLine("Hel") + claudeLine("lo there") + claudeResult}
	p := NewClaude(fakeRuntime{path: "claude", ready: true}, r.start)
	ch, err := p.Stream(context.Background(), llm.Request{
		Model: "sonnet", Reasoning: "high", System: "Be brief.",
		Messages: []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: "hi"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ds := collect(t, ch)
	var text string
	for _, d := range ds {
		if d.Kind == llm.DeltaText {
			text += d.Text
		}
	}
	if text != "Hello there" || ds[len(ds)-1].Kind != llm.DeltaStop || ds[len(ds)-1].Stop != llm.StopEnd {
		t.Fatalf("deltas: %+v", ds)
	}
	var usage *llm.Usage
	for _, d := range ds {
		if d.Kind == llm.DeltaUsage {
			usage = d.Usage
		}
	}
	if usage == nil || *usage.InputTokens != 35 || *usage.CachedTokens != 20 || *usage.OutputTokens != 7 {
		t.Fatalf("usage: %+v", usage)
	}
	for _, want := range []string{"-p", "stream-json", "--no-session-persistence", "--strict-mcp-config", "--disable-slash-commands", "--model", "sonnet", "--effort", "high"} {
		if !slices.Contains(r.args, want) {
			t.Errorf("args lack %q: %v", want, r.args)
		}
	}
	// Every built-in tool and setting source is switched off: the flag is followed by an empty value.
	for _, flag := range []string{"--tools", "--setting-sources"} {
		i := slices.Index(r.args, flag)
		if i < 0 || r.args[i+1] != "" {
			t.Errorf("%s must be empty: %v", flag, r.args)
		}
	}
	if !strings.Contains(r.stdin, "Be brief.") || !strings.Contains(r.stdin, "[user]\nhi") {
		t.Errorf("the system instructions and conversation travel on stdin: %q", r.stdin)
	}
	for _, a := range r.args {
		if strings.Contains(a, "Be brief.") {
			t.Error("bot instructions must not be on the command line")
		}
	}
}

func TestClaudeNeverShowsAToolBlocksFenceAndTurnsItIntoCalls(t *testing.T) {
	r := &run{out: claudeLine("Let me ") + claudeLine("check.\n\n``") + claudeLine("`tool_calls\n[{\"name\":\"web_search\",") + claudeLine("\"arguments\":{\"q\":\"oslo\"}}]\n```") + claudeResult}
	p := NewClaude(fakeRuntime{path: "claude", ready: true}, r.start)
	ch, err := p.Stream(context.Background(), llm.Request{Tools: []llm.ToolSpec{{Name: "web_search"}}})
	if err != nil {
		t.Fatal(err)
	}
	var shown string
	var calls []llm.ToolCall
	var stop llm.StopReason
	for _, d := range collect(t, ch) {
		switch d.Kind {
		case llm.DeltaText:
			shown += d.Text
		case llm.DeltaToolCall:
			calls = append(calls, *d.Call)
		case llm.DeltaStop:
			stop = d.Stop
		}
	}
	if strings.Contains(shown, "`") || strings.TrimSpace(shown) != "Let me check." {
		t.Fatalf("shown text: %q", shown)
	}
	if len(calls) != 1 || calls[0].Name != "web_search" || string(calls[0].Args) != `{"q":"oslo"}` || stop != llm.StopToolCalls {
		t.Fatalf("calls %+v stop %s", calls, stop)
	}
}

func TestReplyStreamKeepsMultiByteTextWholeAndFlushesAMarkerLookalike(t *testing.T) {
	var s replyStream
	var shown strings.Builder
	for _, part := range []string{"日本", "語です ``", "` not a tool block"} {
		shown.WriteString(s.Push(part))
	}
	rest, calls := s.Finish(nil)
	shown.WriteString(rest)
	if shown.String() != "日本語です ``` not a tool block" || len(calls) != 0 {
		t.Fatalf("shown %q calls %v", shown.String(), calls)
	}
}

func TestClaudeReportsFailuresAndMissingInstall(t *testing.T) {
	for _, tc := range []struct {
		out  string
		code llm.ErrorCode
	}{
		{`{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}` + "\n", llm.ErrAuth},
		{`{"type":"result","is_error":true,"result":"You've hit your usage limit"}` + "\n", llm.ErrRateLimit},
		{`{"type":"result","is_error":true,"result":"The model 'x' was not found"}` + "\n", llm.ErrBadRequest},
	} {
		ch, err := NewClaude(fakeRuntime{path: "claude", ready: true}, (&run{out: tc.out}).start).Stream(context.Background(), llm.Request{})
		if err != nil {
			t.Fatal(err)
		}
		ds := collect(t, ch)
		if len(ds) != 1 || ds[0].Err == nil || ds[0].Err.Code != tc.code {
			t.Errorf("%s: %+v", tc.out, ds)
		}
	}
	if _, err := NewClaude(fakeRuntime{}, (&run{}).start).Stream(context.Background(), llm.Request{}); err == nil || !strings.Contains(err.Error(), "Settings > AI providers") {
		t.Fatalf("not installed: %v", err)
	}
}

func TestClaudeOffersTheCLIsModelAliasesAndItsDescriptor(t *testing.T) {
	models, err := NewClaude(fakeRuntime{path: "claude", ready: true}, nil).Models(context.Background())
	if err != nil || len(models) != 3 || models[0].ID != "opus" || models[2].Caps.Reasoning || !slices.Contains(models[1].Reasoning, "xhigh") {
		t.Fatalf("models: %+v %v", models, err)
	}
	d := ClaudeDescriptor(fakeRuntime{path: "claude", ready: true})
	if d.ID != "claude" || d.Kind != "account" || !d.NoKey {
		t.Fatalf("descriptor: %+v", d)
	}
	if ok, _ := d.Ready(); !ok {
		t.Fatal("a signed-in CLI must be ready")
	}
}

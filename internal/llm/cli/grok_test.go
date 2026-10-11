package cli

import (
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func TestGrokStreamsInTheSameFormatAndKeepsThePromptOffTheCommandLine(t *testing.T) {
	r := &run{out: claudeLine("po") + claudeLine("ng") + claudeResult}
	var promptFile, prompt string
	p := NewGrok(fakeRuntime{path: "grok", ready: true}, func(ctx context.Context, path, dir, stdin string, args ...string) (io.ReadCloser, func() error, error) {
		i := slices.Index(args, "--prompt-file")
		promptFile = args[i+1]
		data, err := os.ReadFile(promptFile)
		if err != nil {
			t.Errorf("the prompt file must exist while Grok runs: %v", err)
		}
		prompt = string(data)
		return r.start(ctx, path, dir, stdin, args...)
	})
	ch, err := p.Stream(context.Background(), llm.Request{
		Model: "grok-4.7", System: "Be brief.",
		Messages: []llm.Message{{Role: llm.RoleUser, Parts: []llm.Part{{Kind: llm.PartText, Text: "ping?"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, d := range collect(t, ch) {
		if d.Kind == llm.DeltaText {
			text += d.Text
		}
	}
	if text != "pong" {
		t.Fatalf("text %q", text)
	}
	if !strings.Contains(prompt, "Be brief.") || !strings.Contains(prompt, "[user]\nping?") {
		t.Errorf("prompt file: %q", prompt)
	}
	for _, want := range []string{"streaming-messages-json", "--no-subagents", "--no-plan", "-m", "grok-4.7", "--disallowed-tools"} {
		if !slices.Contains(r.args, want) {
			t.Errorf("args lack %q: %v", want, r.args)
		}
	}
	for _, a := range r.args {
		if strings.Contains(a, "Be brief.") || strings.Contains(a, "ping?") {
			t.Error("the conversation must not be on the command line")
		}
	}
	if _, err := os.Stat(promptFile); !os.IsNotExist(err) {
		t.Error("the scratch directory must be removed after the turn")
	}
}

func TestGrokModelsAreParsedFromItsListing(t *testing.T) {
	r := &run{out: "You are logged in with grok.com.\n\nDefault model: grok-4.7\n\nAvailable models:\n  * grok-4.7 (default)\n  - grok-4.6\n"}
	models, err := NewGrok(fakeRuntime{path: "grok", ready: true}, r.start).Models(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "grok-4.7" || models[1].ID != "grok-4.6" || !models[0].Caps.Tools {
		t.Fatalf("models: %+v %v", models, err)
	}
	if !slices.Equal(r.args, []string{"models"}) {
		t.Fatalf("args: %v", r.args)
	}
	if _, err := NewGrok(fakeRuntime{path: "grok", ready: true}, (&run{out: "You are not authenticated.\n"}).start).Models(context.Background()); err == nil || !strings.Contains(err.Error(), "Sign in") {
		t.Fatalf("signed out: %v", err)
	}
}

func TestGrokErrorsAndDescriptor(t *testing.T) {
	if e := grokError("You are not authenticated.", nil); e.Code != llm.ErrAuth {
		t.Errorf("auth: %+v", e)
	}
	if e := grokError("", os.ErrClosed); e.Code != llm.ErrServer || !strings.Contains(e.Message, "Grok stopped") {
		t.Errorf("exit: %+v", e)
	}
	d := GrokDescriptor(fakeRuntime{path: "grok", ready: true})
	if d.ID != "grok" || d.Kind != "account" || !d.NoKey {
		t.Fatalf("descriptor: %+v", d)
	}
}

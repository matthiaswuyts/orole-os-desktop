package agents_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/agents"
)

func TestScopedPromptRevalidatesAfterAuthorization(t *testing.T) {
	var onPrompt func() error
	e := newEnv(t, func(c *envCfg) {
		c.authorize = func(_ context.Context, action, _, _ string) error {
			if action == "agent.prompt" && onPrompt != nil {
				return onPrompt()
			}
			return nil
		}
	})
	s := e.start(t, agents.StartSpec{Prompt: "first task"})
	s = e.wait(t, s.ID, "done", status(agents.StatusDone))
	if !e.m.CanPromptScoped(s) {
		t.Fatal("completed structured session not eligible")
	}
	revision := agents.PromptRevision(s)
	denied := errors.New("source sharing revoked")
	if err := e.m.PromptScoped(ctx10(t), s.ID, "private snapshot", revision, func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("source guard: %v", err)
	}
	before, err := e.m.Get(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Prompts != s.Prompts {
		t.Fatal("guard denial sent a prompt")
	}
	onPrompt = func() error { _, err := e.m.Stop(ctx10(t), s.ID); return err }
	if err := e.m.PromptScoped(ctx10(t), s.ID, "private snapshot", revision, func(context.Context) error { return nil }); err == nil {
		t.Fatal("recipient stopped across approval but received snapshot")
	}
	onPrompt = nil
	s, err = e.m.Get(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !e.m.CanPromptScoped(s) {
		t.Fatal("true local continuation not eligible")
	}
	if err := e.m.PromptScoped(ctx10(t), s.ID, "approved snapshot", agents.PromptRevision(s), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	s = e.wait(t, s.ID, "continued turn", status(agents.StatusDone))
	events, err := e.m.Events(s.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var delivered int
	for _, event := range events {
		if event.Kind == agents.EvPrompt && event.Text == "private snapshot" {
			t.Fatal("denied content persisted")
		}
		if event.Kind == agents.EvPrompt && event.Text == "approved snapshot" {
			delivered++
		}
	}
	if delivered != 1 {
		t.Fatalf("delivered %d times", delivered)
	}
}

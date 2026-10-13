package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
)

const ciCheck = `{"repo":"acme/app","branch":"main"}`

// ask records the bot's CI check in a run the user started.
func (e *env) ask(b chat.Bot, args string) {
	r := newRun(b)
	r.Author = "user"
	e.s.ObserveTool(context.Background(), r.RunContext, "mcp__github__workflow_runs", json.RawMessage(args))
}

func (e *env) signals(botID string) []chat.AutomationSignal {
	e.t.Helper()
	out, err := e.s.Signals(context.Background(), botID)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func TestRepeatedRequestsOnSeparateDaysBecomeAnIdea(t *testing.T) {
	e := newEnv(t)
	b := e.bot("a", "Alpha")
	for range 3 {
		e.ask(b, ciCheck)
	}
	if got := e.signals(b.ID); len(got) != 1 || got[0].Count != 3 || got[0].Days != 1 || got[0].Ready {
		t.Fatalf("three calls on one day: %+v", got)
	}
	if p := e.s.SuggestionsPrompt(b); p != "" {
		t.Fatalf("suggested after one day: %q", p)
	}
	e.advance(26 * time.Hour)
	e.ask(b, `{"branch":"main","repo":"acme/app"}`) // same call, keys in another order
	got := e.signals(b.ID)
	if len(got) != 1 || got[0].Count != 4 || got[0].Days != 2 || !got[0].Ready || !strings.Contains(got[0].Label, "acme/app") {
		t.Fatalf("signal: %+v", got)
	}
	p := e.s.SuggestionsPrompt(b)
	if !strings.Contains(p, got[0].Key) || !strings.Contains(p, "4 times on 2 days") || !strings.Contains(p, "propose_automation") {
		t.Fatalf("prompt: %q", p)
	}
	if again := e.s.SuggestionsPrompt(b); again != "" {
		t.Fatalf("offered twice in a day: %q", again)
	}
	e.advance(24 * time.Hour)
	if again := e.s.SuggestionsPrompt(b); again != "" {
		t.Fatalf("the same idea came back within a week: %q", again)
	}
	e.advance(7 * 24 * time.Hour)
	if again := e.s.SuggestionsPrompt(b); again == "" {
		t.Fatal("an unanswered idea was never offered a second time")
	}
	e.advance(8 * 24 * time.Hour)
	if again := e.s.SuggestionsPrompt(b); again != "" {
		t.Fatalf("offered more than twice: %q", again)
	}
}

// Only calls in turns the user typed count: automation messages, task and bot reports and
// hand-offs carry outside content. Calls that are not integration checks (memory, drafting, display) never count.
func TestOnlyTheUsersOwnRequestsCount(t *testing.T) {
	e := newEnv(t)
	b := e.bot("a", "Alpha")
	for _, author := range []string{"automation:a1", "task:t1", "report:b2", "bot:b2", ""} {
		r := newRun(b)
		r.Author = author
		e.s.ObserveTool(context.Background(), r.RunContext, "mcp__github__workflow_runs", json.RawMessage(ciCheck))
	}
	r := newRun(b)
	r.Author = "user"
	for _, name := range []string{"memory", "propose_automation", "show_table", "start_task"} {
		e.s.ObserveTool(context.Background(), r.RunContext, name, json.RawMessage(`{}`))
	}
	if got := e.signals(b.ID); len(got) != 0 {
		t.Fatalf("recorded: %+v", got)
	}
}

func TestDecliningOrDeletingAnIdeaSilencesIt(t *testing.T) {
	e := newEnv(t)
	b := e.bot("a", "Alpha")
	for range 2 {
		e.ask(b, ciCheck)
		e.ask(b, ciCheck)
		e.advance(25 * time.Hour)
	}
	key := e.signals(b.ID)[0].Key
	res, err := e.tool("automation_suggestion").Call(context.Background(), newRun(b).RunContext, "automation_suggestion", json.RawMessage(`{"key":"`+key+`","decision":"declined"}`))
	if err != nil || res.IsError {
		t.Fatalf("decline: %+v %v", res, err)
	}
	if p := e.s.SuggestionsPrompt(b); p != "" {
		t.Fatalf("declined idea suggested: %q", p)
	}
	if got := e.signals(b.ID); got[0].Ready || !got[0].Answered {
		t.Fatalf("declined idea: %+v", got)
	}
	if err := e.s.DeleteSignal(context.Background(), chat.SignalRef{BotID: b.ID, Key: key}); err != nil {
		t.Fatal(err)
	}
	if got := e.signals(b.ID); len(got) != 0 {
		t.Fatalf("deleted idea listed: %+v", got)
	}
	if err := e.s.DeleteSignal(context.Background(), chat.SignalRef{BotID: b.ID, Key: key}); err == nil {
		t.Fatal("deleting a missing idea succeeded")
	}
}

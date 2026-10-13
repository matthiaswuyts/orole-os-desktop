package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func automationEngine(t *testing.T) *automations.Engine {
	t.Helper()
	db, err := database.Open(filepath.ToSlash(filepath.Join(t.TempDir(), "db")), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := automations.New(automations.Options{DB: db.DB, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close(); db.Close() })
	return e
}

func automator() chat.Bot { return chat.Bot{ID: "b1"} }

func userRun(b chat.Bot) chat.RunContext { return chat.RunContext{BotID: b.ID, Bot: b, Author: "user"} }

const watchCI = `{"definition":{"name":"Watch CI","trigger":{"type":"watch","watch":{"source":"github","kind":"workflow_failed","repo":"acme/app","branch":"main"}},"steps":[{"id":"tell","type":"bot.tell","params":{"botId":"b1","message":"{{ event.data.name }} failed"}}]},"rationale":"Tells you when CI on main fails."}`

func TestAutomationToolsAreOfferedToEveryBot(t *testing.T) {
	tool := Automations{Runtime: automationEngine(t)}
	if got := tool.Specs(automator(), llm.Caps{Tools: true}); len(got) != 4 {
		t.Fatalf("specs = %d", len(got))
	}
	if got := tool.Specs(automator(), llm.Caps{}); got != nil {
		t.Fatalf("offered to a model without tools: %d specs", len(got))
	}
}

func TestProposeAutomationShowsACardAndCreatesNothing(t *testing.T) {
	e := automationEngine(t)
	tool := Automations{Runtime: e}
	res, err := tool.Call(context.Background(), userRun(automator()), toolProposeAutomation, json.RawMessage(watchCI))
	if err != nil || res.IsError || len(res.Parts) != 1 || res.Parts[0].Name != "automation" {
		t.Fatalf("result: %+v %v", res, err)
	}
	var spec struct{ ProposalID, BotID, Name string }
	if err := json.Unmarshal([]byte(res.Parts[0].Spec), &spec); err != nil || spec.BotID != "b1" || spec.Name != "Watch CI" {
		t.Fatalf("card spec %q: %v", res.Parts[0].Spec, err)
	}
	p, err := e.Proposal(context.Background(), spec.ProposalID)
	if err != nil || p.Status != "pending" || p.Definition.Trigger.Watch == nil || p.Definition.Trigger.Watch.Repo != "acme/app" {
		t.Fatalf("stored draft: %+v %v", p, err)
	}
	if list, _ := e.List(context.Background()); len(list) != 0 {
		t.Fatalf("drafting created an automation: %+v", list)
	}
}

func TestProposeAutomationReturnsIssuesForTheBotToFix(t *testing.T) {
	tool := Automations{Runtime: automationEngine(t)}
	bad := strings.Replace(watchCI, `"kind":"workflow_failed"`, `"kind":"workflow_exploded"`, 1)
	res, _ := tool.Call(context.Background(), userRun(automator()), toolProposeAutomation, json.RawMessage(bad))
	if !res.IsError || len(res.Parts) != 0 || !strings.Contains(res.Text, "trigger.watch") {
		t.Fatalf("result: %+v", res)
	}
	typo := strings.Replace(watchCI, `"trigger"`, `"trigge"`, 1)
	if res, _ := tool.Call(context.Background(), userRun(automator()), toolProposeAutomation, json.RawMessage(typo)); !res.IsError || !strings.Contains(res.Text, "trigge") {
		t.Fatalf("unknown field: %+v", res)
	}
}

// Content the user did not type (automation messages, task and bot reports, hand-offs) must
// never lead to a persistent automation, even as a draft.
func TestOnlyTheUsersOwnTurnsCanDraftAutomations(t *testing.T) {
	e := automationEngine(t)
	tool := Automations{Runtime: e}
	for _, author := range []string{"automation:a1", "task:t1", "report:b2", "bot:b2"} {
		rc := userRun(automator())
		rc.Author = author
		res, _ := tool.Call(context.Background(), rc, toolProposeAutomation, json.RawMessage(watchCI))
		if !res.IsError || len(res.Parts) != 0 {
			t.Fatalf("%s drafted an automation: %+v", author, res)
		}
	}
}

func TestAutomationReferenceListsOnlyAvailableSteps(t *testing.T) {
	tool := Automations{Runtime: automationEngine(t)} // no bots, tasks, GitHub or MCP wired in
	res, _ := tool.Call(context.Background(), userRun(automator()), toolAutomationReference, json.RawMessage(`{}`))
	var ref struct {
		YourBotID string                   `json:"yourBotId"`
		StepTypes []automations.StepType   `json:"stepTypes"`
		Templates []automations.Definition `json:"templates"`
	}
	if err := json.Unmarshal([]byte(res.Text), &ref); err != nil || ref.YourBotID != "b1" || len(ref.Templates) == 0 {
		t.Fatalf("reference: %v %s", err, res.Text)
	}
	for _, s := range ref.StepTypes {
		if !s.Available || s.Type == "bot.tell" {
			t.Fatalf("unavailable step offered: %+v", s)
		}
	}
}

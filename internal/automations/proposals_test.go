package automations

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func notifyDraft(name string) Definition {
	return Definition{Name: name, Trigger: Trigger{Type: "event", Event: "agent.failed"}, Steps: []Step{
		{ID: "ping", Type: "notify", Params: json.RawMessage(`{"title":"{{ event.data.title }} failed","level":"error"}`)},
	}}
}

func commandDraft() Definition {
	return Definition{Name: "Nightly tests", AllowedExecutables: []string{"go"}, Trigger: Trigger{Type: "schedule", Schedule: "0 2 * * *"}, Steps: []Step{
		{ID: "test", Type: "test.run", Params: json.RawMessage(`{"executable":"go","args":["test","./..."],"dir":"C:\\code\\app"}`)},
	}}
}

func propose(t *testing.T, e *Engine, bot string, d Definition, replaces string) Proposal {
	t.Helper()
	res, err := e.Propose(context.Background(), bot, d, "Keeps you posted.", replaces)
	if err != nil || len(res.Issues) > 0 {
		t.Fatalf("propose: %v %+v", err, res.Issues)
	}
	return res.Proposal
}

func summaryOf(t *testing.T, e *Engine, id string) Summary {
	t.Helper()
	list, err := e.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Definition.ID == id {
			return s
		}
	}
	t.Fatalf("automation %s not listed", id)
	return Summary{}
}

func TestInvalidDraftsComeBackAsIssuesAndStoreNothing(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	ctx := context.Background()
	bad := notifyDraft("Broken")
	bad.Trigger.Event = "not a topic!"
	res, err := e.Propose(ctx, "b1", bad, "", "")
	if err != nil || len(res.Issues) == 0 || res.Issues[0].Path != "trigger.event" || res.Proposal.ID != "" {
		t.Fatalf("invalid draft: %+v %v", res, err)
	}
	secret := notifyDraft("Leaky")
	secret.Steps[0].Params = json.RawMessage(`{"title":"token ghp_abcdefghijklmnopqrstuvwx","level":"info"}`)
	if res, err := e.Propose(ctx, "b1", secret, "", ""); err != nil || len(res.Issues) == 0 || !strings.Contains(res.Issues[0].Message, "secrets") {
		t.Fatalf("secret draft: %+v %v", res, err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM automation_proposals`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("stored %d drafts (%v)", n, err)
	}
	if list, _ := e.List(ctx); len(list) != 0 {
		t.Fatalf("a draft became an automation: %+v", list)
	}
}

func TestApprovingADraftSavesItAsTheBotsAutomation(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	ctx := context.Background()
	draft := notifyDraft("Failed tasks")
	draft.Enabled = true // a bot cannot switch its own draft on
	p := propose(t, e, "b1", draft, "")
	if p.Status != "pending" || p.Definition.Enabled || len(p.Risks) != 0 {
		t.Fatalf("pending draft: %+v", p)
	}
	if list, _ := e.List(ctx); len(list) != 0 {
		t.Fatalf("a pending draft exists as an automation: %+v", list)
	}
	got, err := e.ResolveProposal(ctx, ProposalDecision{ID: p.ID, Approve: true, Enable: true})
	if err != nil || got.Status != "approved" || got.AutomationID == "" {
		t.Fatalf("approve: %+v %v", got, err)
	}
	s := summaryOf(t, e, got.AutomationID)
	if !s.Definition.Enabled || s.CreatedBy != "bot:b1" || s.Definition.Name != "Failed tasks" {
		t.Fatalf("saved automation: %+v", s)
	}
	if _, err := e.ResolveProposal(ctx, ProposalDecision{ID: p.ID, Approve: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second decision = %v, want ErrInvalid", err)
	}
	if again, _ := e.Proposal(ctx, p.ID); again.Status != "approved" || again.AutomationID != got.AutomationID {
		t.Fatalf("card state: %+v", again)
	}
}

func TestRiskyDraftsCanOnlyBeAddedTurnedOff(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	ctx := context.Background()
	p := propose(t, e, "b1", commandDraft(), "")
	if len(p.Risks) != 2 || !strings.Contains(p.Risks[1], "Runs go test ./...") {
		t.Fatalf("risks = %q", p.Risks)
	}
	if _, err := e.ResolveProposal(ctx, ProposalDecision{ID: p.ID, Approve: true, Enable: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("enabling a risky draft = %v", err)
	}
	if list, _ := e.List(ctx); len(list) != 0 {
		t.Fatal("a refused approval saved the automation")
	}
	got, err := e.ResolveProposal(ctx, ProposalDecision{ID: p.ID, Approve: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := summaryOf(t, e, got.AutomationID); s.Definition.Enabled {
		t.Fatal("risky draft saved turned on")
	}
}

func TestDismissingADraftSavesNothing(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	ctx := context.Background()
	p := propose(t, e, "b1", notifyDraft("Nope"), "")
	got, err := e.ResolveProposal(ctx, ProposalDecision{ID: p.ID, Approve: false, Enable: true})
	if err != nil || got.Status != "rejected" {
		t.Fatalf("dismiss: %+v %v", got, err)
	}
	if list, _ := e.List(ctx); len(list) != 0 {
		t.Fatal("a dismissed draft was saved")
	}
}

func TestEditsApplyOnlyToTheVersionTheBotSaw(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	ctx := context.Background()
	current := notifyDraft("Mine")
	current.Enabled = true
	current = saveDef(t, e, current)

	edit := notifyDraft("")
	edit.Steps[0].Params = json.RawMessage(`{"title":"Edited","level":"warning"}`)
	p := propose(t, e, "b1", edit, current.ID)
	if p.Definition.Name != "Mine" || !p.ReplacesEnabled || p.Replaces != current.ID {
		t.Fatalf("edit draft: %+v", p)
	}
	stale := propose(t, e, "b2", edit, current.ID)

	got, err := e.ResolveProposal(ctx, ProposalDecision{ID: p.ID, Approve: true, Enable: true})
	if err != nil || got.AutomationID != current.ID {
		t.Fatalf("apply edit: %+v %v", got, err)
	}
	s := summaryOf(t, e, current.ID)
	if s.CreatedBy != "user" || !strings.Contains(string(s.Definition.Steps[0].Params), "Edited") {
		t.Fatalf("edited automation: %+v", s)
	}
	if _, err := e.ResolveProposal(ctx, ProposalDecision{ID: stale.ID, Approve: true, Enable: true}); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale edit = %v, want ErrChanged", err)
	}
	if _, err := e.Propose(ctx, "b1", edit, "", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit of a missing automation = %v", err)
	}
}

func TestANewerDraftReplacesTheBotsEarlierOneAndPendingIsBounded(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	ctx := context.Background()
	first := propose(t, e, "b1", notifyDraft("Same"), "")
	second := propose(t, e, "b1", notifyDraft("Same"), "")
	if p, _ := e.Proposal(ctx, first.ID); p.Status != "replaced" {
		t.Fatalf("earlier draft = %s, want replaced", p.Status)
	}
	if _, err := e.ResolveProposal(ctx, ProposalDecision{ID: first.ID, Approve: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("approving a replaced draft = %v", err)
	}
	other := propose(t, e, "b2", notifyDraft("Same"), "")
	if p, _ := e.Proposal(ctx, second.ID); p.Status != "pending" || other.Status != "pending" {
		t.Fatal("another bot's draft replaced this bot's")
	}
	for i := range maxPendingProposals - 1 {
		propose(t, e, "b1", notifyDraft("Draft "+string(rune('a'+i))), "")
	}
	if _, err := e.Propose(ctx, "b1", notifyDraft("One too many"), "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("draft over the pending limit = %v", err)
	}
}

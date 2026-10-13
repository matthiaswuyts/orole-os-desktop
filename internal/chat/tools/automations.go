package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Automation tool names.
const (
	toolAutomationReference = "automation_reference"
	toolListAutomations     = "list_automations"
	toolAutomationDetail    = "automation_detail"
	toolProposeAutomation   = "propose_automation"
)

// AutomationRuntime is the daemon's automation engine as bots may use it: read what exists and
// draft. There is deliberately no save, enable, run or delete: a draft becomes an automation
// only when the user approves its card.
type AutomationRuntime interface {
	List(ctx context.Context) ([]automations.Summary, error)
	Runs(ctx context.Context, id string) ([]automations.Run, error)
	StepTypes() []automations.StepType
	Propose(ctx context.Context, botID string, d automations.Definition, rationale, replaces string) (automations.ProposeResult, error)
}

// Automations offers the automation tools to bots whose configuration allows it.
type Automations struct{ Runtime AutomationRuntime }

func (Automations) ID() string { return "automations" }

const definitionHelp = `Automation definition (schemaVersion 2). Shape: {"name","description","trigger":{"type":"manual|schedule|event|watch|webhook","schedule":"cron or 'every 15m'","event":"topic or prefix.*","filter":"expression over event.data","watch":{"source":"github|mcp","kind":"...","repo":"owner/name","branch":"..."}},"steps":[{"id":"letters-digits","type":"<step type>","params":{...},"then":[...],"else":[...]}]}. Call automation_reference first for every step type's params, the triggers, event topics and templates. Templates: {{ event.data.<field> }}, {{ steps.<id>.output... }}. Do not set id or enabled.`

func (a Automations) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if !caps.Tools || a.Runtime == nil {
		return nil
	}
	return []llm.ToolSpec{
		{Name: toolAutomationReference, Description: "How to write an Orole automation: triggers, every step type with its params, the app events a flow can start on, ready-made templates, and your own bot id for bot.tell steps. Read it before drafting.",
			Schema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
		{Name: toolListAutomations, Description: "List the user's automations: id, name, trigger, whether it is on, who created it and how its last run went.",
			Schema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
		{Name: toolAutomationDetail, Description: "Read one automation's full definition and its recent runs (status, errors). Use it to explain a failure or before drafting an edit.",
			Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`)},
		{Name: toolProposeAutomation, Description: "Draft an automation (or an edit to one with replaces) for the user. Orole validates it: if it has problems you get them back to fix. A valid draft appears as a card in the chat; nothing exists or runs until the user approves it there. " +
			"Use it when the user asks for something to happen automatically or on a schedule, or agreed to your suggestion. Never include secrets, tokens or passwords.",
			Schema: json.RawMessage(`{"type":"object","properties":{"definition":{"type":"object","description":` + quote(definitionHelp) + `},"rationale":{"type":"string","description":"One sentence for the user: what it does and why it helps them."},"replaces":{"type":"string","description":"Id of the automation this draft edits; omit for a new automation."}},"required":["definition","rationale"],"additionalProperties":false}`)},
	}
}

func (a Automations) Call(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if a.Runtime == nil || rc.BotID == "" {
		return errResult("automations need a bot")
	}
	switch name {
	case toolAutomationReference:
		return a.reference(rc.BotID)
	case toolListAutomations:
		list, err := a.Runtime.List(ctx)
		if err != nil {
			return errResult("cannot list automations: %s", err)
		}
		type row struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			On         bool   `json:"on"`
			Trigger    string `json:"trigger"`
			CreatedBy  string `json:"createdBy"`
			LastStatus string `json:"lastStatus,omitempty"`
			LastRunAt  int64  `json:"lastRunAt,omitempty"`
			Problems   int    `json:"problems,omitempty"`
		}
		rows := make([]row, 0, len(list))
		for _, s := range list {
			rows = append(rows, row{ID: s.Definition.ID, Name: s.Definition.Name, On: s.Definition.Enabled, Trigger: triggerLine(s.Definition.Trigger),
				CreatedBy: s.CreatedBy, LastStatus: s.LastStatus, LastRunAt: s.LastRunAt, Problems: len(s.Issues)})
		}
		b, _ := json.Marshal(rows)
		return chat.ToolResult{Text: string(b), Untrusted: true}, nil
	case toolAutomationDetail:
		var in struct {
			ID string `json:"id"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("invalid arguments: %s", err)
		}
		list, err := a.Runtime.List(ctx)
		if err != nil {
			return errResult("cannot read automations: %s", err)
		}
		for _, s := range list {
			if s.Definition.ID != in.ID {
				continue
			}
			runs, err := a.Runtime.Runs(ctx, in.ID)
			if err != nil {
				return errResult("cannot read runs: %s", err)
			}
			type run struct {
				Status    string `json:"status"`
				Trigger   string `json:"trigger"`
				Error     string `json:"error,omitempty"`
				CreatedAt int64  `json:"createdAt"`
			}
			recent := []run{}
			for _, r := range runs[:min(len(runs), 5)] {
				recent = append(recent, run{Status: r.Status, Trigger: r.TriggerType, Error: oneLine(r.Error, 300), CreatedAt: r.CreatedAt})
			}
			b, _ := json.Marshal(map[string]any{"definition": s.Definition, "createdBy": s.CreatedBy, "issues": s.Issues, "recentRuns": recent})
			return chat.ToolResult{Text: string(b), Untrusted: true}, nil
		}
		return errResult("no automation has id %q; call list_automations", in.ID)
	case toolProposeAutomation:
		// Only the user's own words may lead to a persistent automation: a turn started by an
		// automation, a task report or another bot carries outside content.
		if rc.Author != "user" {
			return errResult("you can only draft automations while the user is talking to you; mention the idea in your reply instead")
		}
		var in struct {
			Definition automations.Definition `json:"definition"`
			Rationale  string                 `json:"rationale"`
			Replaces   string                 `json:"replaces"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("invalid arguments: %s", err)
		}
		if err := text("rationale", in.Rationale, 500, true); err != nil {
			return errResult("%s", err)
		}
		res, err := a.Runtime.Propose(ctx, rc.BotID, in.Definition, in.Rationale, in.Replaces)
		if err != nil {
			return errResult("the draft was not stored: %s", err)
		}
		if len(res.Issues) > 0 {
			var b strings.Builder
			b.WriteString("The draft does not validate yet; fix these and call propose_automation again:\n")
			for _, is := range res.Issues {
				fmt.Fprintf(&b, "- %s: %s\n", is.Path, is.Message)
			}
			return chat.ToolResult{Text: b.String(), IsError: true}, nil
		}
		p := res.Proposal
		spec, _ := json.Marshal(map[string]string{"proposalId": p.ID, "botId": p.BotID, "name": p.Definition.Name})
		note := "The user can turn it on from the card."
		if len(p.Risks) > 0 {
			note = "It runs programs or sends requests, so the user can only add it turned off and review it in Automations first."
		}
		return chat.ToolResult{
			Text: fmt.Sprintf("The user now sees a card for the automation %q. Nothing exists until they approve it, so do not say it is set up. %s "+
				"Keep your reply short. If they want changes, call propose_automation again; the new draft replaces this one.", p.Definition.Name, note),
			Parts: []chat.MsgPart{{Type: chat.PartUI, ID: toolProposeAutomation, Name: "automation", Spec: string(spec)}},
		}, nil
	}
	return errResult("unknown tool")
}

// reference is the authoring guide: the live step catalogue (with availability), triggers,
// event topics and templates.
func (a Automations) reference(botID string) (chat.ToolResult, error) {
	steps := []automations.StepType{}
	for _, s := range a.Runtime.StepTypes() {
		if s.Available {
			steps = append(steps, s)
		}
	}
	b, err := json.Marshal(map[string]any{
		"yourBotId": botID,
		"triggers": map[string]string{
			"manual":   "Only when the user presses Run.",
			"schedule": `trigger.schedule: 5-field cron in local time ("0 9 * * 1-5") or "every 15m"; catchUp skip|run-once.`,
			"event":    "trigger.event: a topic below or prefix.* ; trigger.filter: optional expression such as event.data.status == 'failed'.",
			"watch":    `trigger.watch: {"source":"github","kind":"push|pr_opened|pr_merged|workflow_failed|issue_opened","repo":"owner/name","branch":"main"} or {"source":"mcp","kind":"deployment","server":"<integration id>","tool":"<list deployments tool>","args":{},"states":["ERROR"]}. Polls every minute or two; runs once per new item.`,
			"webhook":  "A signed request to the local loopback listener; rarely what the user wants.",
		},
		"stepTypes": steps,
		"events":    events.Catalog(),
		"templates": automations.Templates(),
		"rules": []string{
			"Step ids: a letter, then letters, digits, _ or -.",
			"A condition step has params.condition and runs then[] or else[].",
			"bot.tell sends a message to a bot (yourBotId to report to yourself); event text reaches it as data.",
			"Pickers (bot, project, harness) must be real ids.",
			"command.run, test.run and http.request need allowedExecutables / allowedHosts; the user must review such drafts before they can run.",
		},
	})
	if err != nil {
		return errResult("cannot build the reference")
	}
	return chat.ToolResult{Text: string(b)}, nil
}

// triggerLine describes a trigger in a few words for listings.
func triggerLine(t automations.Trigger) string {
	switch t.Type {
	case "schedule":
		return "schedule " + t.Schedule
	case "event":
		return "event " + t.Event
	case "watch":
		if t.Watch != nil {
			return strings.TrimSpace(fmt.Sprintf("watch %s %s %s %s", t.Watch.Source, t.Watch.Kind, t.Watch.Repo, t.Watch.Branch))
		}
	}
	return t.Type
}

var _ chat.Tool = Automations{}

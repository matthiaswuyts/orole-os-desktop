package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

type JarvisRule struct {
	ID            string `json:"id,omitempty"`
	Trigger       string `json:"trigger"`
	Target        string `json:"target"`
	Note          string `json:"note,omitempty"`
	Repeat        bool   `json:"repeat,omitempty"`
	Urgent        bool   `json:"urgent,omitempty"`
	QuietOverride bool   `json:"quietOverride,omitempty"`
	ExpiresAt     int64  `json:"expiresAt,omitempty"` // unix ms
}

type JarvisRuntime interface {
	AddRule(ctx context.Context, botID string, r JarvisRule) (JarvisRule, error)
	Rules(ctx context.Context, botID string) ([]JarvisRule, error)
	DeleteRule(ctx context.Context, botID, id string) error
	CallOwner(ctx context.Context, botID, reason string, urgent bool) (string, error)
	SwitchBot(ctx context.Context, callID, name string) (string, error)
}

// Jarvis offers phone tools to bots with Jarvis mode on. The daemon re-checks permissions on every call.
type Jarvis struct{ Runtime JarvisRuntime }

func (Jarvis) ID() string { return "jarvis" }

const ruleSchema = `{"type":"object","properties":{
"trigger":{"type":"string","enum":["task_finished","task_failed","automation_finished","automation_failed","notification","at_time"]},
"target":{"type":"string","description":"Task or automation id, \"any\" (for tasks: any task you started), a notification kind like \"failed\" or \"failed:github\", or HH:MM for at_time."},
"note":{"type":"string","maxLength":280,"description":"What to tell the owner when it fires."},
"repeat":{"type":"boolean","description":"Keep the rule after it fires. Default: delete after the first call."},
"urgent":{"type":"boolean","description":"Retry once after 5 minutes if unanswered."},
"quietOverride":{"type":"boolean","description":"Call even during quiet hours. Only when the owner says so."},
"expiresAt":{"type":"integer","description":"Unix milliseconds after which the rule no longer fires (e.g. \"tonight\")."}},
"required":["trigger","target"]}`

func (j Jarvis) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if !caps.Tools || !bot.Config.Jarvis.Enabled || j.Runtime == nil {
		return nil
	}
	return []llm.ToolSpec{
		{Name: "jarvis_rule_add", Description: "Save a rule for when to phone the owner, after they asked for it (\"call me when the deploy finishes\"). Confirm the saved rule in your reply.", Schema: json.RawMessage(ruleSchema)},
		{Name: "jarvis_rule_list", Description: "List your call rules.", Schema: json.RawMessage(`{"type":"object","properties":{}}`)},
		{Name: "jarvis_rule_delete", Description: "Delete one of your call rules.", Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)},
		{Name: "call_owner", Description: "Phone the owner now because something matches their \"When to call me\" policy. The app may refuse (quiet hours, limits).", Schema: json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string","maxLength":200},"urgent":{"type":"boolean"}},"required":["reason"]}`)},
		{Name: "switch_bot", Description: "On a phone call only: hand the call to another bot by name when the owner asks for it.", Schema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)},
	}
}

func (j Jarvis) Call(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if !rc.Bot.Config.Jarvis.Enabled {
		return errResult("Jarvis mode is off for this bot.")
	}
	switch name {
	case "jarvis_rule_add":
		var in JarvisRule
		if err := decode(args, &in); err != nil {
			return errResult("%s", err)
		}
		r, err := j.Runtime.AddRule(ctx, rc.BotID, in)
		if err != nil {
			return errResult("%s", err)
		}
		return chat.ToolResult{Text: fmt.Sprintf("Saved rule %s: %s %s%s.", r.ID, r.Trigger, r.Target, map[bool]string{true: " (repeats)", false: " (once)"}[r.Repeat])}, nil
	case "jarvis_rule_list":
		rules, err := j.Runtime.Rules(ctx, rc.BotID)
		if err != nil {
			return errResult("%s", err)
		}
		if len(rules) == 0 {
			return chat.ToolResult{Text: "No call rules."}, nil
		}
		var b strings.Builder
		for _, r := range rules {
			fmt.Fprintf(&b, "- %s: %s %s once=%v note=%q\n", r.ID, r.Trigger, r.Target, !r.Repeat, oneLine(r.Note, 120))
		}
		return chat.ToolResult{Text: b.String()}, nil
	case "jarvis_rule_delete":
		var in struct {
			ID string `json:"id"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("%s", err)
		}
		if err := j.Runtime.DeleteRule(ctx, rc.BotID, in.ID); err != nil {
			return errResult("%s", err)
		}
		return chat.ToolResult{Text: "Deleted."}, nil
	case "call_owner":
		if rc.CallID != "" {
			return errResult("You are already on a call with the owner.")
		}
		var in struct {
			Reason string `json:"reason"`
			Urgent bool   `json:"urgent"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("%s", err)
		}
		msg, err := j.Runtime.CallOwner(ctx, rc.BotID, oneLine(in.Reason, 200), in.Urgent)
		if err != nil {
			return errResult("%s", err)
		}
		return chat.ToolResult{Text: msg}, nil
	case "switch_bot":
		if rc.CallID == "" {
			return errResult("switch_bot only works on a phone call.")
		}
		var in struct {
			Name string `json:"name"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("%s", err)
		}
		msg, err := j.Runtime.SwitchBot(ctx, rc.CallID, in.Name)
		if err != nil {
			return errResult("%s", err)
		}
		return chat.ToolResult{Text: msg}, nil
	}
	return errResult("Unknown tool %s.", name)
}

// JarvisPrompt adds the owner's call policy to Jarvis-enabled bots.
func JarvisPrompt(bot chat.Bot) string {
	if !bot.Config.Jarvis.Enabled {
		return ""
	}
	policy := strings.TrimSpace(bot.Config.Jarvis.Policy)
	if policy == "" {
		policy = "Only call for failures or when you need a decision."
	}
	return "## Phone calls (Jarvis)\nYou can phone the owner. When they tell you when to call, save it with jarvis_rule_add and confirm it. " +
		"Use call_owner only when something matches their policy below; the app enforces quiet hours and limits.\n" +
		"Owner's policy, in their words:\n" + chat.Fence("owner policy", policy)
}

var _ chat.Tool = Jarvis{}

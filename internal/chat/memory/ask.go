package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// maxAskDepth is the deepest ask_bot nesting: a user-initiated run is depth 0, its callee runs at 1,
// and so on; a run at this depth may not ask anyone.
const maxAskDepth = 3

const maxQuestionChars = 8000

type askTool struct{ s *Store }

func (askTool) ID() string { return "ask_bot" }

// maxCallees bounds how many bots an executive assistant is told about.
const maxCallees = 20

// delegates lists the other active bots an executive assistant may ask; every other bot gets none.
func (s *Store) delegates(ctx context.Context, bot chat.Bot) []chat.Bot {
	if !bot.Config.Executive {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM bots WHERE archived_at IS NULL AND id <> ? ORDER BY name COLLATE NOCASE LIMIT ?`, bot.ID, maxCallees)
	if err != nil {
		return nil
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var out []chat.Bot
	for _, id := range ids {
		if b, err := s.loadBot(ctx, s.db, id); err == nil {
			out = append(out, b)
		}
	}
	return out
}

func (t askTool) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if !caps.Tools || !bot.Config.Executive || t.s.getDelegator() == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	callees := t.s.delegates(ctx, bot)
	if len(callees) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("Hand a self-contained task or question to another bot. It works in the background and reports back here when done; this call returns at once, so tell the user in a short line that it has started and carry on talking with them. The other bot has its own memory and cannot see this conversation, so include all needed context. Bots you may hand off to:\n")
	for _, c := range callees {
		fmt.Fprintf(&b, "- %s (id %s)", cleanLabel(c.Name), c.ID)
		if d := oneLine(runeTrunc(c.Description, 120)); d != "" {
			b.WriteString(": " + cleanLabel(d))
		}
		b.WriteString("\n")
	}
	return []llm.ToolSpec{{
		Name:        "ask_bot",
		Description: strings.TrimRight(b.String(), "\n"),
		Schema:      json.RawMessage(`{"type":"object","properties":{"bot":{"type":"string","description":"Name or id of the bot to ask."},"question":{"type":"string"}},"required":["bot","question"]}`),
	}}
}

func (t askTool) Call(ctx context.Context, rc chat.RunContext, _ string, raw json.RawMessage) (chat.ToolResult, error) {
	s := t.s
	var a struct {
		Bot      string `json:"bot"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolFail("invalid arguments: " + err.Error()), nil
	}
	question := strings.TrimSpace(a.Question)
	switch {
	case question == "":
		return toolFail("question is empty"), nil
	case len([]rune(question)) > maxQuestionChars:
		return toolFail(fmt.Sprintf("question is longer than %d characters", maxQuestionChars)), nil
	case strings.TrimSpace(a.Bot) == "":
		return toolFail("bot is required"), nil
	}
	d := s.getDelegator()
	if d == nil {
		return toolFail("delegation is not available"), nil
	}
	if rc.Depth >= maxAskDepth {
		return toolFail(fmt.Sprintf("delegation is nested %d levels deep already; answer with what you have", rc.Depth)), nil
	}
	caller, err := s.loadBot(ctx, s.db, runBotID(rc))
	if err != nil {
		return userToolResult(err)
	}
	callees := s.delegates(ctx, caller)
	want := strings.TrimSpace(a.Bot)
	var callee *chat.Bot
	for i := range callees {
		if callees[i].ID == want {
			callee = &callees[i]
			break
		}
	}
	if callee == nil {
		for i := range callees {
			if strings.EqualFold(callees[i].Name, want) {
				callee = &callees[i]
				break
			}
		}
	}
	if callee == nil {
		names := make([]string, len(callees))
		for i, c := range callees {
			names[i] = cleanLabel(c.Name)
		}
		return toolFail(fmt.Sprintf("%q is not a bot you may ask. Allowed: %s", runeTrunc(want, 60), strings.Join(names, ", "))), nil
	}
	if callee.ID == caller.ID || slices.Contains(rc.Chain, callee.ID) {
		return toolFail(fmt.Sprintf("asking %s would create a delegation cycle", cleanLabel(callee.Name))), nil
	}
	// The delegated run inherits the runs waiting on it: this run's own chain plus the caller.
	rc.Chain = append(slices.Clone(rc.Chain), caller.ID)

	request := runeTrunc(strings.TrimSpace(question), 1000)
	if err := d.Handoff(ctx, rc, callee.ID, question); err != nil {
		if rc.Emit != nil {
			rc.Emit(chat.MsgPart{Type: chat.PartDelegation, ID: newID(), Name: callee.Name, BotID: callee.ID, Status: "error", Summary: runeTrunc(oneLine(err.Error()), 160), Text: request})
		}
		return toolFail(fmt.Sprintf("could not hand off to %s: %s", cleanLabel(callee.Name), err.Error())), nil
	}
	if rc.Emit != nil {
		rc.Emit(chat.MsgPart{Type: chat.PartDelegation, ID: newID(), Name: callee.Name, BotID: callee.ID, Status: "done", Summary: "Handed off", Text: request})
	}
	return chat.ToolResult{Text: fmt.Sprintf("Handed off to %s. It is working on it in the background and will report back in this conversation when it is done. Tell the user in one short line that it has started, then stop: do not wait, poll or do the work yourself.", cleanLabel(callee.Name))}, nil
}

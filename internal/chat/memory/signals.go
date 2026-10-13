package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Automation signals: a bot that sets up automations notices requests the user keeps making of it
// (the same tool call, same arguments, on separate days) and may offer once to automate one. The
// signal is deterministic: only successful tool calls in runs the user started count, so text from
// automations, task reports, hand-offs or tool results cannot fabricate a pattern.

const (
	signalMinCount   = 3                     // calls before a signal is suggested
	signalMinDays    = 2                     // on at least this many distinct local days
	signalMaxOffers  = 2                     // times one signal is offered at most
	signalReoffer    = 7 * 24 * time.Hour    // wait before the same signal is offered again
	signalDailyLimit = 24 * time.Hour        // at most one offer per bot per day
	signalDeclined   = 90 * 24 * time.Hour   // silence after the user declined
	signalAccepted   = 3650 * 24 * time.Hour // the user automated it
	signalLabelMax   = 140
	signalPromptMax  = 2 // candidates in one prompt
)

// signalTool reports whether repeated calls of a tool are something a flow could do instead:
// integration tools and code-task overviews. Memory, drafting, display and search tools are not.
func signalTool(name string) bool {
	return strings.HasPrefix(name, "mcp__") || name == "fetch_url" || name == "list_tasks"
}

// signalKey identifies a tool call by its name and canonical arguments (object keys sorted).
func signalKey(name string, args json.RawMessage) (key, canonical string) {
	var v any
	if json.Unmarshal(args, &v) == nil {
		if b, err := json.Marshal(v); err == nil {
			canonical = string(b)
		}
	}
	if canonical == "{}" || canonical == "null" {
		canonical = ""
	}
	sum := sha256.Sum256([]byte(name + "\x00" + canonical))
	return name + ":" + hex.EncodeToString(sum[:6]), canonical
}

// signalLabel is the one-line description shown to the bot and the user.
func signalLabel(name, canonical string) string {
	label := name
	if canonical != "" {
		label += " " + canonical
	}
	label = strings.NewReplacer("`", "'", "<", "‹", ">", "›").Replace(oneLine(label))
	if r := []rune(label); len(r) > signalLabelMax {
		label = string(r[:signalLabelMax-1]) + "…"
	}
	return label
}

// ObserveTool records a successful tool call of a bot, in a run the user
// started. It implements chat.ToolObserver; failures are logged, never surfaced to the turn.
func (s *Store) ObserveTool(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) {
	if rc.BotID == "" || rc.Author != "user" || !signalTool(name) {
		return
	}
	key, canonical := signalKey(name, args)
	now := s.now()
	day := now.Format("2006-01-02")
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO automation_signals(bot_id, key, label, count, days, last_day, first_at, last_at)
		VALUES (?,?,?,1,1,?,?,?)
		ON CONFLICT(bot_id, key) DO UPDATE SET count = count + 1,
			days = days + CASE WHEN last_day = excluded.last_day THEN 0 ELSE 1 END,
			last_day = excluded.last_day, last_at = excluded.last_at`,
		rc.BotID, key, signalLabel(name, canonical), day, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		s.log.Warn("chat: automation signal not recorded", "bot", rc.BotID, "err", err)
	}
}

// Signals lists a bot's signals, most repeated first.
func (s *Store) Signals(ctx context.Context, botID string) ([]chat.AutomationSignal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, label, count, days, offered_at, offers, dismissed_until, first_at, last_at
		FROM automation_signals WHERE bot_id = ? ORDER BY count DESC, last_at DESC`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := s.nowMs()
	out := []chat.AutomationSignal{}
	for rows.Next() {
		sig := chat.AutomationSignal{BotID: botID}
		var offeredAt, dismissedUntil int64
		var offers int
		if err := rows.Scan(&sig.Key, &sig.Label, &sig.Count, &sig.Days, &offeredAt, &offers, &dismissedUntil, &sig.FirstAt, &sig.LastAt); err != nil {
			return nil, err
		}
		sig.Ready = ready(sig.Count, sig.Days, offers, offeredAt, dismissedUntil, now)
		sig.Answered = dismissedUntil > now
		out = append(out, sig)
	}
	return out, rows.Err()
}

// DeleteSignal forgets one signal; it starts again from zero if the request comes back.
func (s *Store) DeleteSignal(ctx context.Context, ref chat.SignalRef) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM automation_signals WHERE bot_id = ? AND key = ?`, ref.BotID, ref.Key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFoundf("signal not found")
	}
	s.audit("chat.signals.delete", ref.BotID, "allowed", ref.Key)
	return nil
}

func ready(count, days, offers int, offeredAt, dismissedUntil, now int64) bool {
	return count >= signalMinCount && days >= signalMinDays && offers < signalMaxOffers &&
		dismissedUntil <= now && now-offeredAt >= signalReoffer.Milliseconds()
}

// SuggestionsPrompt is the user-turn prompt section of a bot: at most two
// ready signals, at most once a day per bot. Rendering marks them offered, which is the rate limit.
func (s *Store) SuggestionsPrompt(bot chat.Bot) string {
	if bot.ID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	now := s.nowMs()
	var recent int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM automation_signals WHERE bot_id = ? AND offered_at > ?`,
		bot.ID, now-signalDailyLimit.Milliseconds()).Scan(&recent); err != nil || recent > 0 {
		return ""
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key, label, count, days FROM automation_signals
		WHERE bot_id = ? AND count >= ? AND days >= ? AND offers < ? AND dismissed_until <= ? AND offered_at <= ?
		ORDER BY count DESC, last_at DESC LIMIT ?`,
		bot.ID, signalMinCount, signalMinDays, signalMaxOffers, now, now-signalReoffer.Milliseconds(), signalPromptMax)
	if err != nil {
		return ""
	}
	type candidate struct {
		key, label  string
		count, days int
	}
	var list []candidate
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.key, &c.label, &c.count, &c.days) == nil {
			list = append(list, c)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Automation ideas\nThe user keeps asking you for the same thing (your own tool calls, on separate days):\n")
	for _, c := range list {
		if _, err := s.db.ExecContext(ctx, `UPDATE automation_signals SET offered_at = ?, offers = offers + 1 WHERE bot_id = ? AND key = ?`, now, bot.ID, c.key); err != nil {
			return ""
		}
		fmt.Fprintf(&b, "- `%s`: %s (%d times on %d days)\n", c.key, c.label, c.count, c.days)
	}
	b.WriteString("If it fits this conversation, offer once, in one sentence, to set up an automation that does it for them " +
		"(on a schedule, or when something changes). Draft it with propose_automation only after they agree, then call " +
		"automation_suggestion with decision accepted. If they decline, call automation_suggestion with decision declined. " +
		"Do not bring this list up otherwise.")
	return b.String()
}

// decideSignal records the user's answer to a suggestion.
func (s *Store) decideSignal(ctx context.Context, botID, key, decision string) error {
	var until time.Duration
	switch decision {
	case "declined":
		until = signalDeclined
	case "accepted":
		until = signalAccepted
	default:
		return invalidf("decision must be declined or accepted")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE automation_signals SET dismissed_until = ? WHERE bot_id = ? AND key = ?`,
		s.now().Add(until).UnixMilli(), botID, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return invalidf("no automation idea has key %q", key)
	}
	return nil
}

type suggestionTool struct{ s *Store }

func (suggestionTool) ID() string { return "automation_suggestion" }

func (suggestionTool) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if !caps.Tools {
		return nil
	}
	return []llm.ToolSpec{{
		Name:        "automation_suggestion",
		Description: "Record the user's answer to an automation idea from your prompt, so it is not suggested again: accepted once you drafted it, declined if they do not want it.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"},"decision":{"type":"string","enum":["accepted","declined"]}},"required":["key","decision"],"additionalProperties":false}`),
	}}
}

func (t suggestionTool) Call(ctx context.Context, rc chat.RunContext, _ string, raw json.RawMessage) (chat.ToolResult, error) {
	var a struct {
		Key      string `json:"key"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolFail("invalid arguments: " + err.Error()), nil
	}
	if rc.BotID == "" {
		return toolFail("automation suggestions need a bot"), nil
	}
	if err := t.s.decideSignal(ctx, rc.BotID, a.Key, a.Decision); err != nil {
		return userToolResult(err)
	}
	return chat.ToolResult{Text: "Noted."}, nil
}

var _ chat.ToolObserver = (*Store)(nil)

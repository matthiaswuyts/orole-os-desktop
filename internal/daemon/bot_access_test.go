package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type fakeBots map[string]chat.Bot

func (f fakeBots) Bot(_ context.Context, id string) (chat.Bot, error) {
	b, ok := f[id]
	if !ok {
		return chat.Bot{}, chat.ErrNotFound
	}
	return b, nil
}

// A bot's own tool access beats the global "Bot tools" setting; unknown levels, unknown bots, the
// builder draft and non-bot actors always get the policy defaults (ask).
func TestBotToolAccessResolution(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	d := &Daemon{DB: db}
	bots := fakeBots{
		"inherit": {ID: "inherit"},
		"reader":  {ID: "reader", Config: chat.BotConfig{ToolAccess: chat.ToolAccessRead}},
		"all":     {ID: "all", Config: chat.BotConfig{ToolAccess: chat.ToolAccessAll}},
		"asker":   {ID: "asker", Config: chat.BotConfig{ToolAccess: chat.ToolAccessAsk}},
		"bogus":   {ID: "bogus", Config: chat.BotConfig{ToolAccess: "everything"}},
		"draft":   {ID: "draft", Config: chat.BotConfig{ToolAccess: chat.ToolAccessAll}},
	}
	hook := d.botAutoApproveHook(bots)
	save := func(raw string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('agents',?,0) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, raw); err != nil {
			t.Fatal(err)
		}
	}
	global := func(level string) string {
		raw, _ := json.Marshal(map[string]any{"botToolAccess": level})
		return string(raw)
	}
	tests := []struct {
		name    string
		setting string // "" = nothing saved
		actor   string
		want    policy.Effect
		reason  string
	}{
		{"nothing saved", "", "bot:inherit", policy.EffectUnknown, ""},
		{"global write inherited", global("write"), "bot:inherit", policy.EffectWrite, "global"},
		{"bot read beats global write", global("write"), "bot:reader", policy.EffectRead, "this bot's"},
		{"bot all beats global ask", global("ask"), "bot:all", policy.EffectDestructive, "this bot's"},
		{"bot ask beats global all", global("all"), "bot:asker", policy.EffectUnknown, ""},
		{"unknown bot level fails closed", global("all"), "bot:bogus", policy.EffectUnknown, ""},
		{"unknown global level fails closed", global("everything"), "bot:inherit", policy.EffectUnknown, ""},
		{"unreadable setting fails closed", "{not json", "bot:inherit", policy.EffectUnknown, ""},
		{"draft never inherits", global("all"), "bot:draft", policy.EffectUnknown, ""},
		{"unknown bot", global("all"), "bot:ghost", policy.EffectUnknown, ""},
		{"user unaffected", global("all"), "user", policy.EffectUnknown, ""},
		{"agent unaffected", global("all"), "agent:x", policy.EffectUnknown, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := db.Exec(`DELETE FROM settings WHERE key='agents'`); err != nil {
				t.Fatal(err)
			}
			if tt.setting != "" {
				save(tt.setting)
			}
			got, reason := hook(context.Background(), tt.actor)
			if got != tt.want || !strings.Contains(reason, tt.reason) {
				t.Fatalf("got %q %q, want %q (reason mentioning %q)", got, reason, tt.want, tt.reason)
			}
		})
	}

	// Wired into the engine, the level turns a default prompt into an audited allow.
	save(global("write"))
	pol := policy.New(db.DB, nil, nil)
	pol.SetAutoApprove(hook)
	req := policy.Request{Actor: "bot:inherit", Action: "mcp.tool.call", Resource: "mcp/s/send", Effect: policy.EffectWrite, FromHint: true}
	if d, err := pol.Check(context.Background(), req); err != nil || d != policy.Allow {
		t.Fatalf("check: %s %v", d, err)
	}
	req.Effect = policy.EffectDestructive
	if ev, err := pol.Decide(context.Background(), req); err != nil || ev.Decision != policy.Ask {
		t.Fatalf("destructive above the level must ask: %+v %v", ev, err)
	}
}

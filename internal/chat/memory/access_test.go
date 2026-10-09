package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// accessFixture builds three bots and four scopes, each holding a core note and a fact with a
// scope-specific marker, so any leak shows up as a foreign marker in some output.
//
//	A: no grants            sees bot:a
//	B: user r, team rw      sees bot:b, user, shared:team
//	C: other r              sees bot:c, shared:other
type accessFixture struct {
	*env
	a, b, c     chat.Bot
	team, other string
}

var markers = map[string]string{
	"bot:a": "aaardvark", "bot:b": "bbbadger", "bot:c": "cccamel",
	chat.ScopeUser: "uuunicorn", "shared:team": "tttiger", "shared:other": "ooowl",
}

func newAccessFixture(t *testing.T) *accessFixture {
	e := newEnv(t)
	f := &accessFixture{env: e}
	f.team, f.other = e.shared("team"), e.shared("other")
	f.a = e.bot("a", "Alpha")
	f.b = e.bot("b", "Beta", grantTo(chat.ScopeUser, true, false), grantTo(f.team, true, true))
	f.c = e.bot("c", "Gamma", grantTo(f.other, true, false))
	ctx := context.Background()
	for scope, m := range markers {
		for _, kind := range []string{chat.KindCore, chat.KindFact} {
			if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: scope, Kind: kind, Text: "note about " + m + " " + kind}); err != nil {
				t.Fatalf("seed %s %s: %v", scope, kind, err)
			}
		}
	}
	return f
}

func foreignMarkers(allowed ...string) []string {
	var out []string
	for scope, m := range markers {
		ok := false
		for _, a := range allowed {
			ok = ok || a == scope
		}
		if !ok {
			out = append(out, m)
		}
	}
	return out
}

func TestAccessSnapshotMatrix(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	cases := []struct {
		bot     chat.Bot
		allowed []string
	}{
		{f.a, []string{"bot:a"}},
		{f.b, []string{"bot:b", chat.ScopeUser, f.team}},
		{f.c, []string{"bot:c", f.other}},
	}
	for _, tc := range cases {
		snap, err := f.s.Snapshot(ctx, tc.bot)
		if err != nil {
			t.Fatal(err)
		}
		for _, sc := range tc.allowed {
			contains(t, snap, markers[sc]+" core")
			excludes(t, snap, markers[sc]+" fact") // archival facts never enter the snapshot
		}
		excludes(t, snap, foreignMarkers(tc.allowed...)...)
	}
	snap, _ := f.s.Snapshot(ctx, f.b)
	contains(t, snap, "USER PROFILE (who the user is) (read-only)", `SHARED MEMORY "team"`)
	excludes(t, snap, `SHARED MEMORY "team" (read-only)`)
}

func TestAccessSearchAndRecallMatrix(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	cases := []struct {
		bot     chat.Bot
		allowed []string
	}{
		{f.a, []string{"bot:a"}},
		{f.b, []string{"bot:b", chat.ScopeUser, f.team}},
		{f.c, []string{"bot:c", f.other}},
	}
	for _, tc := range cases {
		r := newRun(tc.bot)
		// One query naming every marker: only allowed ones may come back.
		var q []string
		for _, m := range markers {
			q = append(q, m)
		}
		res := f.call("memory_search", r, map[string]any{"query": strings.Join(q, " ")})
		if res.IsError {
			t.Fatal(res.Text)
		}
		for _, sc := range tc.allowed {
			contains(t, res.Text, markers[sc])
		}
		excludes(t, res.Text, foreignMarkers(tc.allowed...)...)

		rec, err := f.s.Recall(ctx, tc.bot, "tell me about "+strings.Join(q, " "))
		if err != nil {
			t.Fatal(err)
		}
		if len(rec) > recallBudget+200 {
			t.Errorf("recall is %d bytes", len(rec))
		}
		for _, sc := range tc.allowed {
			contains(t, rec, markers[sc])
		}
		excludes(t, rec, foreignMarkers(tc.allowed...)...)

		// Explicit scope: unreadable scopes are refused, not silently empty.
		for sc := range markers {
			res := f.call("memory_search", r, map[string]any{"query": markers[sc], "scope": sc})
			readable := false
			for _, a := range tc.allowed {
				readable = readable || a == sc
			}
			if readable == res.IsError {
				t.Errorf("%s searching scope %s: IsError=%v, want %v (%s)", tc.bot.ID, sc, res.IsError, !readable, res.Text)
			}
			if !readable {
				excludes(t, res.Text, markers[sc])
			}
		}
	}
}

func TestAccessWriteMatrix(t *testing.T) {
	f := newAccessFixture(t)
	rb := newRun(f.b)
	ra := newRun(f.a)
	write := func(r *run, scope string) chat.ToolResult {
		return f.mem(r, map[string]any{"action": "add", "scope": scope, "content": "written to " + scope})
	}
	if res := write(rb, f.team); res.IsError {
		t.Fatalf("B writes team: %s", res.Text)
	}
	for _, tc := range []struct {
		r     *run
		scope string
	}{
		{rb, chat.ScopeUser},       // read-only grant
		{rb, "bot:a"},              // another bot's private scope
		{rb, f.other},              // no grant
		{ra, f.team},               // no grant at all
		{ra, chat.ScopeUser},       // no grant at all
		{ra, "bot:b"},              // another bot's private scope
		{rb, "shared:not-created"}, // unknown scope
	} {
		res := write(tc.r, tc.scope)
		if !res.IsError {
			t.Errorf("%s wrote to %s: %s", tc.r.BotID, tc.scope, res.Text)
		}
		contains(t, res.Text, "not writable")
	}
	if n := f.count(`SELECT COUNT(*) FROM memory_entries WHERE text LIKE 'written to %'`); n != 1 {
		t.Fatalf("%d entries written, want exactly the permitted one", n)
	}
	// Replace and remove obey the same rule (the note exists, the grant does not).
	for _, action := range []string{"replace", "remove"} {
		res := f.mem(ra, map[string]any{"action": action, "scope": f.team, "old_text": "tttiger", "content": "x"})
		if !res.IsError {
			t.Errorf("A %s on team: %s", action, res.Text)
		}
	}
	snap, _ := f.s.Snapshot(context.Background(), f.b)
	contains(t, snap, "tttiger core")
}

func TestAccessConfigIsSourceOfTruth(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()

	// A stale bot_memory_access row must not widen access.
	if _, err := f.db.Exec(`INSERT INTO bot_memory_access(bot_id, scope, can_read, can_write) VALUES ('a', ?, 1, 1)`, f.team); err != nil {
		t.Fatal(err)
	}
	snap, _ := f.s.Snapshot(ctx, f.a)
	excludes(t, snap, markers[f.team])
	if n := f.count(`SELECT COUNT(*) FROM bot_memory_access WHERE bot_id = 'a'`); n != 0 {
		t.Errorf("stale rows were kept: %d", n)
	}
	if res := f.mem(newRun(f.a), map[string]any{"action": "add", "scope": f.team, "content": "stale"}); !res.IsError {
		t.Error("stale row allowed a write")
	}

	// Rows mirror the config (read implied by write).
	if n := f.count(`SELECT COUNT(*) FROM bot_memory_access WHERE bot_id = 'b'`); n != 0 {
		t.Fatalf("rows exist before first decision: %d", n)
	}
	if _, err := f.s.Snapshot(ctx, f.b); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT COUNT(*) FROM bot_memory_access WHERE bot_id = 'b' AND scope = ? AND can_read = 1 AND can_write = 1`, f.team); n != 1 {
		t.Errorf("team row not synced")
	}
	if n := f.count(`SELECT COUNT(*) FROM bot_memory_access WHERE bot_id = 'b' AND scope = 'user' AND can_read = 1 AND can_write = 0`); n != 1 {
		t.Errorf("user row not synced")
	}

	// Revoking in the config revokes immediately for the next run, tool calls included.
	f.setConfig("b", func(c *chat.BotConfig) { c.Memory = nil })
	b := f.loadBot("b")
	snap, _ = f.s.Snapshot(ctx, b)
	excludes(t, snap, markers[f.team], markers[chat.ScopeUser])
	if res := f.mem(newRun(b), map[string]any{"action": "add", "scope": f.team, "content": "after revoke"}); !res.IsError {
		t.Error("write after revoke allowed")
	}
	if n := f.count(`SELECT COUNT(*) FROM bot_memory_access WHERE bot_id = 'b'`); n != 0 {
		t.Errorf("rows left after revoke: %d", n)
	}

	// Write-only grant implies read; grants to another bot's scope or to missing scopes are ignored.
	f.setConfig("b", func(c *chat.BotConfig) {
		c.Memory = []chat.ScopeAccess{{Scope: f.team, Write: true}, {Scope: "bot:a", Read: true, Write: true}, {Scope: "shared:ghost", Read: true}}
	})
	b = f.loadBot("b")
	snap, _ = f.s.Snapshot(ctx, b)
	contains(t, snap, markers[f.team])
	excludes(t, snap, markers["bot:a"], "ghost")
	res := f.call("memory_search", newRun(b), map[string]any{"query": markers["bot:a"]})
	excludes(t, res.Text, markers["bot:a"])
}

func TestAccessMemoryToolSpecsOnlyListWritableScopes(t *testing.T) {
	f := newAccessFixture(t)
	spec := func(b chat.Bot) string {
		specs := f.tool("memory").Specs(b, toolCaps)
		if len(specs) != 1 {
			t.Fatalf("specs = %d", len(specs))
		}
		return string(specs[0].Schema)
	}
	contains(t, spec(f.b), `"bot:b"`, f.team)
	excludes(t, spec(f.b), chat.ScopeUser+`"`, f.other, "bot:a")
	excludes(t, spec(f.a), f.team)
	if specs := f.tool("memory").Specs(f.a, llm.Caps{}); specs != nil {
		t.Error("tool offered to a model without tool support")
	}
}

func TestHistorySearchStaysInsideTheBotsConversation(t *testing.T) {
	f := newAccessFixture(t)
	f.msg("a", 1, "user", "we discussed the quokka migration plan")
	f.msg("a", 2, "assistant", "the quokka plan has three steps")
	f.msg("b", 1, "user", "my secret platypus project")
	f.msg("a", 3, "assistant", "half-written quokka", withStatus(chat.StatusRunning))

	res := f.call("search_history", newRun(f.a), map[string]any{"query": "quokka"})
	if res.IsError {
		t.Fatal(res.Text)
	}
	contains(t, res.Text, "#1 user", "#2 assistant")
	excludes(t, res.Text, "#3")
	if !res.Untrusted {
		t.Error("history excerpts must be fenced as data")
	}
	res = f.call("search_history", newRun(f.a), map[string]any{"query": "platypus"})
	excludes(t, res.Text, "platypus")
	contains(t, res.Text, "No matching")
	res = f.call("search_history", newRun(f.b), map[string]any{"query": "platypus"})
	contains(t, res.Text, "platypus")

	// Date filters: message 1 is on an earlier day than message 2.
	f.db.Exec(`UPDATE messages SET created_at = ? WHERE id = 'a-m1'`, int64(1_700_000_000_000))
	res = f.call("search_history", newRun(f.a), map[string]any{"query": "quokka", "after": "2024-01-01"})
	contains(t, res.Text, "#2")
	excludes(t, res.Text, "#1 ")
	res = f.call("search_history", newRun(f.a), map[string]any{"query": "quokka", "before": "2024-01-01"})
	contains(t, res.Text, "#1 ")
	excludes(t, res.Text, "#2 ")
	if res := f.call("search_history", newRun(f.a), map[string]any{"query": "quokka", "after": "yesterday"}); !res.IsError {
		t.Error("bad date accepted")
	}
}

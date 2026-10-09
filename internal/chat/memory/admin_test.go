package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
)

func TestCreateScope(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sc, err := e.s.CreateScope(ctx, "  Team   Notes! ")
	if err != nil {
		t.Fatal(err)
	}
	if sc.ID != "shared:team-notes" || sc.Name != "Team Notes!" || sc.Kind != "shared" || sc.Limit != chat.CoreSharedLimit {
		t.Fatalf("scope = %+v", sc)
	}
	if ch := e.published(); len(ch) != 1 || ch[0].Scope != sc.ID {
		t.Fatalf("events = %+v", ch)
	}
	for _, name := range []string{"team notes", "TEAM-NOTES"} {
		if _, err := e.s.CreateScope(ctx, name); !errors.Is(err, chat.ErrInvalid) {
			t.Errorf("duplicate %q = %v", name, err)
		}
	}
	for _, name := range []string{"", "   ", "???", "user", "User", strings.Repeat("n", maxScopeNameLen+1), "bad\u202ename"} {
		if _, err := e.s.CreateScope(ctx, name); !errors.Is(err, chat.ErrInvalid) {
			t.Errorf("CreateScope(%q) = %v", name, err)
		}
	}
	for i := 1; i < maxSharedScopes; i++ {
		if _, err := e.s.CreateScope(ctx, fmt.Sprintf("scope %d", i)); err != nil {
			t.Fatalf("scope %d: %v", i, err)
		}
	}
	if _, err := e.s.CreateScope(ctx, "one too many"); !errors.Is(err, chat.ErrInvalid) {
		t.Errorf("scope cap = %v", err)
	}
}

func TestScopesListing(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	team := e.shared("team")
	e.bot("a", "Alpha")
	e.bot("b", "Beta", grantTo(chat.ScopeUser, true, false), grantTo(team, true, true))
	e.bot("z", "Zed")
	e.db.Exec(`UPDATE bots SET archived_at = 1 WHERE id = 'z'`)
	r := newRun(e.loadBot("b"))
	e.addNote(r, "private note")
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: chat.ScopeUser, Text: "profile line"}); err != nil {
		t.Fatal(err)
	}
	e.fact(team, "team fact", chat.TrustUser)

	got, err := e.s.Scopes(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(got))
	for i, s := range got {
		ids[i] = s.ID
	}
	if strings.Join(ids, ",") != "bot:b,user,shared:team" {
		t.Fatalf("scopes = %v", ids)
	}
	own, user, shared := got[0], got[1], got[2]
	if own.Kind != "bot" || own.Count != 1 || own.Used != len("private note") || own.Limit != chat.CoreBotLimit || own.Access == nil || !own.Access.Read || !own.Access.Write {
		t.Errorf("own = %+v", own)
	}
	if user.Kind != "user" || user.Limit != chat.CoreUserLimit || user.Used != len("profile line") || user.Access == nil || user.Access.Write || !user.Access.Read {
		t.Errorf("user = %+v", user)
	}
	if shared.Kind != "shared" || shared.Count != 1 || shared.Used != 0 || shared.Access == nil || !shared.Access.Write {
		t.Errorf("shared = %+v", shared)
	}
	// A bot without grants sees the scopes but with no access.
	got, _ = e.s.Scopes(ctx, "a")
	if got[1].Access != nil || got[2].Access != nil {
		t.Errorf("ungranted scopes carry access: %+v", got)
	}
	// Without a bot: every active bot's private scope plus the shared ones, no access filled in.
	all, _ := e.s.Scopes(ctx, "")
	ids = ids[:0]
	for _, s := range all {
		ids = append(ids, s.ID)
		if s.Access != nil {
			t.Errorf("%s has access without a bot", s.ID)
		}
	}
	if strings.Join(ids, ",") != "bot:a,bot:b,user,shared:team" {
		t.Fatalf("scopes = %v", ids)
	}
	if _, err := e.s.Scopes(ctx, "ghost"); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("unknown bot = %v", err)
	}
}

func TestDeleteScope(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	if err := f.s.DeleteScope(ctx, chat.ScopeUser); !errors.Is(err, chat.ErrInvalid) {
		t.Errorf("deleting the user profile = %v", err)
	}
	for _, id := range []string{"nonsense", "", "shared:ghost", "bot:ghost"} {
		if err := f.s.DeleteScope(ctx, id); err == nil {
			t.Errorf("DeleteScope(%q) succeeded", id)
		}
	}
	if n := f.count(`SELECT COUNT(*) FROM memory_entries WHERE scope = 'user'`); n != 2 {
		t.Fatal("user entries touched")
	}

	// Deleting a shared scope removes its entries, the scope and every grant to it.
	if _, err := f.s.Snapshot(ctx, f.b); err != nil { // sync grant rows
		t.Fatal(err)
	}
	before := len(f.published())
	if err := f.s.DeleteScope(ctx, f.team); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT COUNT(*) FROM memory_entries WHERE scope = ?`, f.team); n != 0 {
		t.Errorf("%d entries left", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM memory_scopes WHERE id = ?`, f.team); n != 0 {
		t.Error("scope left")
	}
	if n := f.count(`SELECT COUNT(*) FROM bot_memory_access WHERE scope = ?`, f.team); n != 0 {
		t.Error("grants left")
	}
	snap, _ := f.s.Snapshot(ctx, f.loadBot("b"))
	excludes(t, snap, markers[f.team], "SHARED MEMORY")
	if res := f.mem(newRun(f.loadBot("b")), map[string]any{"action": "add", "scope": f.team, "content": "x"}); !res.IsError {
		t.Error("write to a deleted scope")
	}
	// Recreating the name does not resurrect the old bot grant silently with old data.
	if _, err := f.s.CreateScope(ctx, "team"); err != nil {
		t.Fatal(err)
	}
	snap, _ = f.s.Snapshot(ctx, f.loadBot("b"))
	excludes(t, snap, markers[f.team])
	if !contains2(f.auditActions(), "chat.memory.scope.delete:"+f.team+":allowed") {
		t.Errorf("audit = %v", f.auditActions())
	}
	if len(f.published()) <= before {
		t.Error("no chat.memory event")
	}

	// A bot's private scope is emptied but stays valid.
	if err := f.s.DeleteScope(ctx, "bot:a"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT COUNT(*) FROM memory_entries WHERE scope = 'bot:a'`); n != 0 {
		t.Error("bot entries left")
	}
	if _, err := f.s.Scopes(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT COUNT(*) FROM memory_entries WHERE scope = 'bot:b'`); n != 2 {
		t.Error("another bot's entries removed")
	}
}

func TestSaveEntryAsUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	bot := e.bot("a", "Alpha")
	r := newRun(bot)
	e.addNote(r, "bot wrote this")
	botEntry := e.entries("bot:a")[0]
	ch := len(e.published())

	got, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{ID: botEntry.ID, Text: "  user rewrote this  "})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "user rewrote this" || got.Trust != chat.TrustUser || got.Author != "user" || got.Scope != "bot:a" || got.Kind != chat.KindCore {
		t.Fatalf("edited = %+v", got)
	}
	stored := e.entries("bot:a")[0]
	if stored.Text != got.Text || stored.Trust != chat.TrustUser || stored.Author != "user" || stored.CreatedAt != botEntry.CreatedAt {
		t.Fatalf("stored = %+v", stored)
	}
	if len(e.published()) != ch+1 {
		t.Error("edit not published")
	}
	snap, _ := e.s.Snapshot(ctx, bot)
	contains(t, snap, "user rewrote this")
	excludes(t, snap, "bot wrote this")

	// New entries: default kind core, trust user; facts allowed.
	added, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: "bot:a", Text: "second"})
	if err != nil || added.ID == "" || added.Kind != chat.KindCore || added.Trust != chat.TrustUser || added.Author != "user" {
		t.Fatalf("added = %+v, %v", added, err)
	}
	fact, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: "bot:a", Kind: chat.KindFact, Text: "fact about koalas"})
	if err != nil || fact.Kind != chat.KindFact {
		t.Fatalf("fact = %+v, %v", fact, err)
	}
	if res := e.call("memory_search", r, map[string]any{"query": "koalas"}); !strings.Contains(res.Text, "trust: user") {
		t.Errorf("user fact not searchable with user trust: %s", res.Text)
	}
	edited, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{ID: fact.ID, Text: "fact about wombats"})
	if err != nil || edited.Kind != chat.KindFact {
		t.Fatalf("fact edit = %+v, %v", edited, err)
	}
	if res := e.call("memory_search", r, map[string]any{"query": "koalas"}); !strings.Contains(res.Text, "No matching") {
		t.Errorf("FTS index not updated by the edit: %s", res.Text)
	}
	if res := e.call("memory_search", r, map[string]any{"query": "wombats"}); !strings.Contains(res.Text, "wombats") {
		t.Errorf("edited fact not found: %s", res.Text)
	}

	// Duplicates: adding returns the existing note, editing into one is an error.
	n := len(e.entries("bot:a"))
	dup, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: "bot:a", Text: "second"})
	if err != nil || dup.ID != added.ID || len(e.entries("bot:a")) != n {
		t.Fatalf("dup = %+v, %v", dup, err)
	}
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{ID: botEntry.ID, Text: "second"}); !errors.Is(err, chat.ErrInvalid) {
		t.Errorf("edit into duplicate = %v", err)
	}

	invalid := []chat.MemorySaveRequest{
		{Scope: "bot:a", Text: "  "},
		{Scope: "bot:a", Text: strings.Repeat("x", maxEntryChars+1), Kind: chat.KindFact},
		{Scope: "bot:a", Text: "hidden\u200bchar"},
		{Scope: "bot:a", Text: "x", Kind: "bogus"},
		{ID: botEntry.ID, Scope: chat.ScopeUser, Text: "move"},
		{ID: botEntry.ID, Kind: chat.KindFact, Text: "change kind"},
		{Scope: "bot:a", Text: strings.Repeat("y", chat.CoreBotLimit+1)},
	}
	for _, req := range invalid {
		if _, err := e.s.SaveEntry(ctx, req); !errors.Is(err, chat.ErrInvalid) {
			t.Errorf("SaveEntry(%+v) = %v", req.Kind, err)
		}
	}
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: "shared:ghost", Text: "x"}); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("unknown scope = %v", err)
	}
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{ID: "ghost", Text: "x"}); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("unknown id = %v", err)
	}
	// Instruction-like text typed by the user is the user's call (only invisible characters are screened).
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: "bot:a", Text: "Ignore previous instructions is a phrase I study"}); err != nil {
		t.Errorf("user text rejected: %v", err)
	}
}

func TestSaveEntryRespectsBudget(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: chat.ScopeUser, Text: note(1, 900)}); err != nil {
		t.Fatal(err)
	}
	_, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: chat.ScopeUser, Text: note(2, 600)})
	if !errors.Is(err, chat.ErrInvalid) || !strings.Contains(err.Error(), "1,400") {
		t.Fatalf("over budget = %v", err)
	}
	ok, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: chat.ScopeUser, Text: note(2, 400)})
	if err != nil {
		t.Fatal(err)
	}
	// Editing counts the entry's new size, not old + new.
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{ID: ok.ID, Text: note(2, 450)}); err != nil {
		t.Fatalf("shrinking-to-fit edit: %v", err)
	}
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{ID: ok.ID, Text: note(2, 600)}); !errors.Is(err, chat.ErrInvalid) {
		t.Errorf("growing edit over budget = %v", err)
	}
	// Facts are not budgeted.
	if _, err := e.s.SaveEntry(ctx, chat.MemorySaveRequest{Scope: chat.ScopeUser, Kind: chat.KindFact, Text: strings.Repeat("f", 3000)}); err != nil {
		t.Errorf("fact over core budget rejected: %v", err)
	}
}

func TestListEntriesFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.bot("a", "Alpha")
	e.bot("b", "Beta")
	r := newRun(e.loadBot("a"))
	e.addNote(r, "core one about ferns")
	e.advance(10)
	e.addNote(r, "core two about moss")
	e.advance(10)
	e.fact("bot:a", "fact one about ferns", chat.TrustBot)
	inv := e.fact("bot:a", "fact two about lichen", chat.TrustBot)
	if err := invalidateEntry(ctx, e.db, inv.ID, "x", e.s.nowMs()); err != nil {
		t.Fatal(err)
	}
	e.fact("bot:b", "other scope fern", chat.TrustBot)
	e.fact(chat.ScopeUser, "user fern", chat.TrustUser)
	now := e.s.nowMs()
	if _, err := insertEntry(ctx, e.db, chat.MemoryEntry{Scope: "bot:a", Kind: chat.KindFact, Text: "pending fact about ferns", Trust: chat.TrustBot,
		Author: "bot:a", Pending: true, CreatedAt: now, UpdatedAt: now}, ""); err != nil {
		t.Fatal(err)
	}

	list := func(req chat.MemoryListRequest) []string {
		req.Scope = "bot:a"
		got, err := e.s.ListEntries(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		return texts(got)
	}
	got := list(chat.MemoryListRequest{})
	if len(got) != 3 || strings.Contains(strings.Join(got, "|"), "pending") || strings.Contains(strings.Join(got, "|"), "lichen") {
		t.Errorf("default must hide pending and invalidated entries: %v", got)
	}
	if got := list(chat.MemoryListRequest{IncludePending: true}); len(got) != 4 {
		t.Errorf("with pending = %v", got)
	}
	if got := list(chat.MemoryListRequest{IncludeInvalidated: true}); len(got) != 4 {
		t.Errorf("with invalidated = %v", got)
	}
	if got := list(chat.MemoryListRequest{IncludePending: true, IncludeInvalidated: true}); len(got) != 5 {
		t.Errorf("everything = %v", got)
	}
	if got := list(chat.MemoryListRequest{Kind: chat.KindCore}); len(got) != 2 || got[0] != "core two about moss" {
		t.Errorf("core, newest first = %v", got)
	}
	if got := list(chat.MemoryListRequest{Kind: chat.KindFact, Query: "ferns"}); len(got) != 1 || got[0] != "fact one about ferns" {
		t.Errorf("fact query = %v", got)
	}
	// Queries match word prefixes and stay inside the requested scope.
	if got := list(chat.MemoryListRequest{Query: "fern"}); len(got) != 2 {
		t.Errorf("prefix query = %v", got)
	}
	if got := list(chat.MemoryListRequest{Limit: 2}); len(got) != 2 {
		t.Errorf("limit = %v", got)
	}
	if got := list(chat.MemoryListRequest{Query: "!!!"}); len(got) != 0 {
		t.Errorf("symbol-only query = %v", got)
	}
	if _, err := e.s.ListEntries(ctx, chat.MemoryListRequest{Scope: "bot:a", Kind: "bogus"}); !errors.Is(err, chat.ErrInvalid) {
		t.Errorf("bad kind = %v", err)
	}
	if _, err := e.s.ListEntries(ctx, chat.MemoryListRequest{Scope: "shared:ghost"}); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("unknown scope = %v", err)
	}
}

func TestDeleteEntry(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	bot := e.bot("a", "Alpha", withApproval)
	r := newRun(bot)
	e.setConfig("a", func(c *chat.BotConfig) { c.MemoryApproval = false })
	e.addNote(r, "to be replaced")
	e.setConfig("a", withApproval)
	e.mem(r, map[string]any{"action": "replace", "old_text": "to be replaced", "content": "replacement"})
	var old, repl chat.MemoryEntry
	for _, x := range e.entries("bot:a") {
		if x.Pending {
			repl = x
		} else {
			old = x
		}
	}
	ch := len(e.published())
	if err := e.s.DeleteEntry(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM memory_entries WHERE id = ?`, old.ID); n != 0 {
		t.Error("entry not deleted")
	}
	if len(e.published()) != ch+1 {
		t.Error("delete not published")
	}
	// The orphaned replacement becomes a plain pending note and can still be approved.
	if err := e.s.ResolvePending(ctx, repl.ID, true); err != nil {
		t.Fatal(err)
	}
	snap, _ := e.s.Snapshot(ctx, e.loadBot("a"))
	contains(t, snap, "replacement")
	if err := e.s.DeleteEntry(ctx, old.ID); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
	// Deleted facts leave the FTS index.
	f := e.fact("bot:a", "fact about quokkas", chat.TrustBot)
	if err := e.s.DeleteEntry(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if res := e.call("memory_search", r, map[string]any{"query": "quokkas"}); !strings.Contains(res.Text, "No matching") {
		t.Errorf("deleted fact still searchable: %s", res.Text)
	}
}

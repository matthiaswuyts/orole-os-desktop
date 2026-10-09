package daemon_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
)

func TestChatMemoryHandlers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := control.NewClient(dir, "chat-memory-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	changes := make(chan chat.MemoryChange, 16)
	c.OnEvent(func(name string, data json.RawMessage) {
		var ch chat.MemoryChange
		if name == "chat.memory" && json.Unmarshal(data, &ch) == nil {
			changes <- ch
		}
	})
	if err := c.Subscribe(ctx, "chat.memory"); err != nil {
		t.Fatal(err)
	}
	call := func(method string, params, result any) error {
		t.Helper()
		return c.Call(ctx, method, params, result)
	}
	must := func(method string, params, result any) {
		t.Helper()
		if err := call(method, params, result); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	fails := func(method string, params any, want string) {
		t.Helper()
		err := call(method, params, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want %q", method, err, want)
		}
	}
	nextChange := func() chat.MemoryChange {
		t.Helper()
		select {
		case ch := <-changes:
			return ch
		case <-ctx.Done():
			t.Fatal("chat.memory event not received")
			return chat.MemoryChange{}
		}
	}

	var bot chat.Bot
	must("chat.bots.save", chat.BotInput{Name: "Alpha", Instructions: "Be Alpha."}, &bot)
	if bot.ID == "" {
		t.Fatal("no bot id")
	}
	own := chat.BotPrefix + bot.ID

	// Scopes.
	var team chat.MemoryScope
	must("chat.memory.scopes.create", chat.MemoryScopeCreate{Name: "Team Notes"}, &team)
	if team.ID != "shared:team-notes" || team.Kind != "shared" {
		t.Fatalf("scope = %+v", team)
	}
	if ch := nextChange(); ch.Scope != team.ID {
		t.Fatalf("event = %+v", ch)
	}
	fails("chat.memory.scopes.create", chat.MemoryScopeCreate{Name: "team notes"}, "already exists")
	var scopes []chat.MemoryScope
	must("chat.memory.scopes.list", chat.BotRef{ID: bot.ID}, &scopes)
	ids := make([]string, len(scopes))
	for i, s := range scopes {
		ids[i] = s.ID
	}
	if strings.Join(ids, ",") != own+",user,shared:team-notes" {
		t.Fatalf("scopes = %v", ids)
	}
	if scopes[0].Access == nil || !scopes[0].Access.Write || scopes[2].Access != nil {
		t.Fatalf("access = %+v / %+v", scopes[0].Access, scopes[2].Access)
	}
	must("chat.memory.scopes.list", chat.BotRef{}, &scopes)
	if len(scopes) != 3 {
		t.Fatalf("all scopes = %d", len(scopes))
	}

	// Entries.
	var e chat.MemoryEntry
	must("chat.memory.entries.save", chat.MemorySaveRequest{Scope: own, Text: "The user likes tea."}, &e)
	if e.ID == "" || e.Trust != chat.TrustUser || e.Kind != chat.KindCore {
		t.Fatalf("entry = %+v", e)
	}
	if ch := nextChange(); ch.Scope != own {
		t.Fatalf("event = %+v", ch)
	}
	must("chat.memory.entries.save", chat.MemorySaveRequest{ID: e.ID, Text: "The user likes green tea."}, &e)
	nextChange()
	var list []chat.MemoryEntry
	must("chat.memory.entries.list", chat.MemoryListRequest{Scope: own}, &list)
	if len(list) != 1 || list[0].Text != "The user likes green tea." {
		t.Fatalf("list = %+v", list)
	}
	must("chat.memory.scopes.list", chat.BotRef{ID: bot.ID}, &scopes)
	if scopes[0].Count != 1 || scopes[0].Used != len("The user likes green tea.") || scopes[0].Limit != chat.CoreBotLimit {
		t.Fatalf("stats = %+v", scopes[0])
	}
	fails("chat.memory.entries.save", chat.MemorySaveRequest{Scope: own, Text: "bad\u202etext"}, "invisible")
	fails("chat.memory.entries.save", chat.MemorySaveRequest{Scope: "shared:ghost", Text: "x"}, "not found")
	fails("chat.memory.pending.resolve", chat.MemoryResolve{ID: e.ID, Approve: true}, "not waiting for approval")
	fails("chat.memory.pending.resolve", chat.MemoryResolve{ID: "ghost", Approve: true}, "not found")
	must("chat.memory.entries.delete", chat.MemoryEntryRef{ID: e.ID}, nil)
	nextChange()
	must("chat.memory.entries.list", chat.MemoryListRequest{Scope: own}, &list)
	if len(list) != 0 {
		t.Fatalf("list after delete = %+v", list)
	}
	fails("chat.memory.entries.delete", chat.MemoryEntryRef{ID: e.ID}, "not found")

	// Deleting scopes: the profile is protected, shared scopes go away.
	fails("chat.memory.scopes.delete", chat.MemoryScopeRef{ID: chat.ScopeUser}, "cannot be deleted")
	must("chat.memory.scopes.delete", chat.MemoryScopeRef{ID: team.ID}, nil)
	nextChange()

	// Knowledge and skill proposals (nothing to list yet; bad references are user errors).
	var files []chat.KnowledgeFile
	must("chat.knowledge.list", chat.BotRef{ID: bot.ID}, &files)
	if files == nil || len(files) != 0 {
		t.Fatalf("files = %#v", files)
	}
	fails("chat.knowledge.add", chat.KnowledgeAdd{BotID: bot.ID, AttachmentID: "nope"}, "attachment not found")
	fails("chat.knowledge.remove", chat.KnowledgeRef{BotID: bot.ID, AttachmentID: "nope"}, "not found")
	var props []chat.SkillProposal
	must("chat.skills.pending.list", chat.BotRef{}, &props)
	if props == nil || len(props) != 0 {
		t.Fatalf("proposals = %#v", props)
	}
	fails("chat.skills.pending.resolve", chat.SkillResolve{ID: "nope", Approve: true}, "not found")
}

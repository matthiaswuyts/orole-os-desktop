package daemon

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/memory"
)

// registerChatMemory runs after registerChat: it builds the memory subsystem (core and archival
// memory, consolidation, ask_bot, skill proposals, knowledge files), attaches it to the chat
// Manager and registers the chat.memory.*, chat.knowledge.* and chat.skills.* methods. Memory
// tools reach the engine through Memory.Tools(); they are not registered again with AddTools.
// Every committed change publishes chat.memory (done by the store); mutations the user makes here
// are audited by id only, never with memory text.
func (d *Daemon) registerChatMemory() {
	m := d.Chat
	if m == nil {
		return
	}
	store, err := memory.New(memory.Config{
		DB: d.DB.DB, DataDir: d.DataDir, LLM: d.LLM, Skills: d.Skills, Grants: m, Delegator: m, Log: d.Log,
		Publish: func(event string, payload any) { d.IPC.Publish(event, payload) },
		Audit:   d.audit,
	})
	if err != nil {
		d.Log.Error("chat memory unavailable", "err", err)
		return
	}
	d.ChatMemory = store
	m.SetMemory(store)

	handle(d, "chat.memory.scopes.list", func(ctx context.Context, p chat.BotRef) ([]chat.MemoryScope, error) {
		out, err := store.Scopes(ctx, p.ID)
		return out, chatError(err)
	})
	handle(d, "chat.memory.scopes.create", func(ctx context.Context, p chat.MemoryScopeCreate) (chat.MemoryScope, error) {
		out, err := store.CreateScope(ctx, p.Name)
		if err == nil {
			d.audit("chat.memory.scope.create", out.ID, "allowed", "")
		}
		return out, chatError(err)
	})
	handle(d, "chat.memory.scopes.delete", func(ctx context.Context, p chat.MemoryScopeRef) (none, error) {
		// The store audits scope deletion itself.
		return none{}, chatError(store.DeleteScope(ctx, p.ID))
	})
	handle(d, "chat.memory.entries.list", func(ctx context.Context, p chat.MemoryListRequest) ([]chat.MemoryEntry, error) {
		out, err := store.ListEntries(ctx, p)
		return out, chatError(err)
	})
	handle(d, "chat.memory.entries.save", func(ctx context.Context, p chat.MemorySaveRequest) (chat.MemoryEntry, error) {
		out, err := store.SaveEntry(ctx, p)
		if err == nil {
			action := "added"
			if p.ID != "" {
				action = "edited"
			}
			d.audit("chat.memory.entry.save", out.ID, "allowed", out.Scope+" "+action)
		}
		return out, chatError(err)
	})
	handle(d, "chat.memory.entries.delete", func(ctx context.Context, p chat.MemoryEntryRef) (none, error) {
		err := store.DeleteEntry(ctx, p.ID)
		if err == nil {
			d.audit("chat.memory.entry.delete", p.ID, "allowed", "")
		}
		return none{}, chatError(err)
	})
	handle(d, "chat.memory.pending.resolve", func(ctx context.Context, p chat.MemoryResolve) (none, error) {
		err := store.ResolvePending(ctx, p.ID, p.Approve)
		if err == nil {
			d.audit("chat.memory.pending.resolve", p.ID, "allowed", resolution(p.Approve))
		}
		return none{}, chatError(err)
	})

	handle(d, "chat.knowledge.list", func(ctx context.Context, p chat.BotRef) ([]chat.KnowledgeFile, error) {
		out, err := store.KnowledgeFiles(ctx, p.ID)
		return out, chatError(err)
	})
	handle(d, "chat.knowledge.add", func(ctx context.Context, p chat.KnowledgeAdd) (chat.KnowledgeFile, error) {
		out, err := store.AddKnowledge(ctx, p.BotID, p.AttachmentID)
		if err == nil {
			d.audit("chat.knowledge.add", p.BotID, "allowed", p.AttachmentID)
		}
		return out, chatError(err)
	})
	handle(d, "chat.knowledge.remove", func(ctx context.Context, p chat.KnowledgeRef) (none, error) {
		err := store.RemoveKnowledge(ctx, p.BotID, p.AttachmentID)
		if err == nil {
			d.audit("chat.knowledge.remove", p.BotID, "allowed", p.AttachmentID)
		}
		return none{}, chatError(err)
	})

	handle(d, "chat.skills.pending.get", func(ctx context.Context, p chat.SkillRef) (chat.SkillProposal, error) {
		out, err := store.Proposal(ctx, p.ID)
		return out, chatError(err)
	})
	handle(d, "chat.skills.pending.resolve", func(ctx context.Context, p chat.SkillResolve) (none, error) {
		// The store audits approval and rejection. An approval changes the central registry, so
		// subscribers of skills.changed (the skills page) are told as for any other save.
		proposal, _ := store.Proposal(ctx, p.ID)
		err := store.ResolveSkill(ctx, p.ID, p.Approve)
		if err == nil && p.Approve && proposal.Name != "" {
			d.skillsChanged("save", proposal.Name)
		}
		return none{}, chatError(err)
	})
}

func resolution(approve bool) string {
	if approve {
		return "approved"
	}
	return "rejected"
}

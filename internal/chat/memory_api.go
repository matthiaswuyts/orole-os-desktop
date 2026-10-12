package chat

// Memory, knowledge, skill-proposal and automation-idea IPC (handlers live in internal/daemon/svc_chat_memory.go).
//
//	chat.memory.scopes.list     BotRef (BotID may be empty)   -> []MemoryScope   (private + shared scopes; Access filled when a bot is given)
//	chat.memory.scopes.create   MemoryScopeCreate             -> MemoryScope     ("shared:<slug>")
//	chat.memory.scopes.delete   MemoryScopeRef                -> struct{}        (deletes the scope's entries; not 'user')
//	chat.memory.entries.list    MemoryListRequest             -> []MemoryEntry
//	chat.memory.entries.save    MemorySaveRequest             -> MemoryEntry     (user edit/add; trust "user")
//	chat.memory.entries.delete  MemoryEntryRef                -> struct{}
//	chat.memory.pending.resolve MemoryResolve                 -> struct{}        (approve or reject a held bot write)
//	chat.knowledge.list         BotRef                        -> []KnowledgeFile
//	chat.knowledge.add          KnowledgeAdd                  -> KnowledgeFile   (attachment already committed)
//	chat.knowledge.remove       KnowledgeRef                  -> struct{}
//	chat.skills.pending.get     SkillRef                      -> SkillProposal   (any state: the chat card asks what became of its proposal)
//	chat.skills.pending.resolve SkillResolve                  -> struct{}        (approve saves into internal/skills registry and gives the skill to the proposing bot)
//	chat.signals.list           BotRef                        -> []AutomationSignal (requests the bot may suggest automating)
//	chat.signals.delete         SignalRef                     -> struct{}
//
// Event chat.memory (MemoryChange) is published on every committed change.

// Memory scope id helpers.
const (
	ScopeUser    = "user"
	SharedPrefix = "shared:"
	BotPrefix    = "bot:"
	// ReportPrefix authors the message in which a bot reports the outcome of a task it was handed back to the bot that handed it off.
	ReportPrefix = "report:"
)

// Core-memory budgets in characters (Hermes-style bounded notes).
const (
	CoreBotLimit    = 2200 // per bot "learned" notes
	CoreUserLimit   = 1400 // "user" profile
	CoreSharedLimit = 2200
)

// Entry kinds and trust levels.
const (
	KindCore = "core"
	KindFact = "fact"

	TrustUser      = "user"
	TrustBot       = "bot"
	TrustUntrusted = "untrusted" // derived from web/tool/attachment content
)

// MemoryScope is a visible memory namespace.
type MemoryScope struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Kind   string       `json:"kind"`  // "bot" | "user" | "shared"
	Count  int          `json:"count"` // active entries
	Used   int          `json:"used"`  // core characters used
	Limit  int          `json:"limit"` // core character limit
	Access *ScopeAccess `json:"access,omitempty"`
}

// MemoryScopeCreate creates a shared scope from a display name.
type MemoryScopeCreate struct {
	Name string `json:"name"`
}

// MemoryScopeRef identifies a scope.
type MemoryScopeRef struct {
	ID string `json:"id"`
}

// MemoryEntry is one remembered item.
type MemoryEntry struct {
	ID            string `json:"id"`
	Scope         string `json:"scope"`
	Kind          string `json:"kind"`
	Text          string `json:"text"`
	Trust         string `json:"trust"`
	Author        string `json:"author"`
	SourceMsgID   string `json:"sourceMsgId,omitempty"`
	Pending       bool   `json:"pending"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
	InvalidatedAt int64  `json:"invalidatedAt,omitempty"`
}

// MemoryListRequest lists entries of a scope. IncludeInvalidated shows history.
type MemoryListRequest struct {
	Scope              string `json:"scope"`
	Kind               string `json:"kind"` // "" = both
	IncludePending     bool   `json:"includePending"`
	IncludeInvalidated bool   `json:"includeInvalidated"`
	Query              string `json:"query"` // optional FTS filter
	Limit              int    `json:"limit"` // default 100, max 500
}

// MemorySaveRequest adds (empty ID) or edits an entry as the user.
type MemorySaveRequest struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

// MemoryEntryRef identifies an entry.
type MemoryEntryRef struct {
	ID string `json:"id"`
}

// MemoryResolve approves or rejects a pending bot write.
type MemoryResolve struct {
	ID      string `json:"id"`
	Approve bool   `json:"approve"`
}

// MemoryChange is the chat.memory event payload.
type MemoryChange struct {
	Scope string `json:"scope"`
}

// KnowledgeFile is a document attached to a bot as searchable knowledge.
type KnowledgeFile struct {
	Attachment
	BotID  string `json:"botId"`
	Status string `json:"status"` // "indexing" | "ready" | "error"
	Error  string `json:"error,omitempty"`
	Chunks int    `json:"chunks"`
}

// KnowledgeAdd attaches a committed upload as knowledge.
type KnowledgeAdd struct {
	BotID        string `json:"botId"`
	AttachmentID string `json:"attachmentId"`
}

// KnowledgeRef identifies a knowledge file of a bot.
type KnowledgeRef struct {
	BotID        string `json:"botId"`
	AttachmentID string `json:"attachmentId"`
}

// SkillProposal is a skill a bot proposed; inert until approved.
type SkillProposal struct {
	ID          string `json:"id"`
	BotID       string `json:"botId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Status      string `json:"status"` // "pending" | "approved" (saved and given to the bot) | "rejected"
	CreatedAt   int64  `json:"createdAt"`
}

// SkillRef identifies a skill proposal.
type SkillRef struct {
	ID string `json:"id"`
}

// SkillResolve approves or rejects a proposal.
type SkillResolve struct {
	ID      string `json:"id"`
	Approve bool   `json:"approve"`
}

// AutomationSignal is a request the user keeps making of a bot: the same tool call, with the same
// arguments, in turns the user started. A bot that sets up automations may suggest automating it.
type AutomationSignal struct {
	BotID string `json:"botId"`
	Key   string `json:"key"`
	Label string `json:"label"` // the tool and its arguments, one line
	Count int    `json:"count"`
	Days  int    `json:"days"` // distinct local days it was asked on
	// Ready reports whether it is repeated enough to be suggested and not silenced.
	Ready bool `json:"ready"`
	// Answered reports that the user declined or accepted it; it is silenced for a while.
	Answered bool  `json:"answered"`
	FirstAt  int64 `json:"firstAt"`
	LastAt   int64 `json:"lastAt"`
}

// SignalRef identifies one signal of a bot.
type SignalRef struct {
	BotID string `json:"botId"`
	Key   string `json:"key"`
}

// Package chat owns bots, their continuous conversations, the turn engine and
// the tool/memory plumbing. The desktop process only forwards typed IPC calls
// (internal/app) and renders events; all state and every permission decision
// live here, in the daemon.
//
// IPC methods (daemon names; handlers use the handle[P,R] helper):
//
//	chat.bots.list        struct{}                 -> []Bot
//	chat.bots.get         BotRef                   -> Bot
//	chat.bots.save        BotInput                 -> Bot
//	chat.bots.archive     BotRef                   -> struct{}
//	chat.bots.export      BotRef                   -> BotExport
//	chat.bots.import      BotExport                -> Bot
//	chat.messages.list    MessagesRequest          -> MessagePage
//	chat.send             SendRequest              -> Message   (the stored user message; reply streams via events)
//	chat.cancel           BotRef                   -> struct{}
//	chat.regenerate       RegenerateRequest        -> struct{}
//	chat.clear            BotRef                   -> struct{}  (deletes the conversation + summary; memory is kept)
//	chat.draft.send       DraftRequest             -> Message   (bot-builder preview: ephemeral, nothing persisted)
//	chat.attachments.begin  AttachmentBegin        -> AttachmentUpload
//	chat.attachments.chunk  AttachmentChunk        -> struct{}
//	chat.attachments.commit AttachmentCommit       -> Attachment
//	chat.attachments.discard AttachmentRef         -> struct{}
//	chat.attachments.read   AttachmentRef          -> AttachmentData (bytes for previews, size-capped)
//	chat.memory.*, chat.knowledge.*, chat.skills.* : see memory_api.go
//
// IPC events (published with d.IPC.Publish; MyGo forwards them as colon names):
//
//	chat.message  Message        upsert of a message snapshot (streaming snapshots coalesced to <= 1 per 50ms; final always sent)
//	chat.run      RunState       a bot started/finished a run
//	chat.bot      BotChange      bot saved/archived/cleared
//	chat.memory   MemoryChange   memory content changed (see memory_api.go)
package chat

import "encoding/json"

// Limits shared by the engine and handlers.
const (
	MaxBots              = 100
	MaxNameLen           = 80
	MaxInstructionsLen   = 32 << 10
	MaxMessageTextLen    = 64 << 10
	MaxAttachmentsPerMsg = 10
	MaxAttachmentBytes   = 20 << 20 // per file
	AttachmentChunkMax   = 1 << 20  // bytes per chunk before base64; frames are capped at 4 MiB
	AttachmentTTLHours   = 24       // staged, never-sent uploads expire
)

// Tool identifiers a bot can enable (BotConfig.Tools[].ID).
const (
	ToolWebSearch = "web_search"
	ToolWebFetch  = "web_fetch"
	ToolDatetime  = "datetime"
	ToolResearch  = "research"
	ToolKnowledge = "knowledge_search"
)

// BotTool enables one built-in tool with optional parameters (e.g. allowed_domains).
type BotTool struct {
	ID     string         `json:"id"`
	Params map[string]any `json:"params,omitempty"`
}

// ScopeAccess grants a bot access to a shared memory scope ("user", "shared:<name>").
type ScopeAccess struct {
	Scope string `json:"scope"`
	Read  bool   `json:"read"`
	Write bool   `json:"write"`
}

// BotLimits bound a single turn. Zero values select engine defaults.
type BotLimits struct {
	MaxSteps     int `json:"maxSteps"`     // model calls per turn (default 8)
	MaxToolCalls int `json:"maxToolCalls"` // total tool calls per turn incl. hosted (default 20)
}

// BotConfig is stored as JSON in bots.config. It never contains secrets.
type BotConfig struct {
	Tools      []BotTool     `json:"tools"`
	MCPTools   []string      `json:"mcpTools"`   // "<serverId>/<tool>" allow-list, default none
	Skills     []string      `json:"skills"`     // enabled registry skill names
	DelegateTo []string      `json:"delegateTo"` // bot ids this bot may ask (ask_bot)
	Memory     []ScopeAccess `json:"memory"`     // grants beyond the private scope
	Limits     BotLimits     `json:"limits"`
	// MemoryApproval holds bot memory writes as pending until the user approves them.
	MemoryApproval bool `json:"memoryApproval"`
}

// Bot is a contact: identity, brain settings and live status.
type Bot struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Avatar       string    `json:"avatar"` // emoji or initials; the UI renders colour + avatar
	Color        string    `json:"color"`  // token name, e.g. "blue"; validated against a fixed list in the UI
	Description  string    `json:"description"`
	Instructions string    `json:"instructions"`
	Provider     string    `json:"provider"` // empty = use defaults
	Model        string    `json:"model"`
	Reasoning    string    `json:"reasoning"`
	Config       BotConfig `json:"config"`
	CreatedAt    int64     `json:"createdAt"` // Unix milliseconds
	UpdatedAt    int64     `json:"updatedAt"`
	// Derived, not stored:
	Running     bool            `json:"running"`
	LastMessage *MessagePreview `json:"lastMessage,omitempty"`
}

// MessagePreview feeds the bot rail.
type MessagePreview struct {
	Role string `json:"role"`
	Text string `json:"text"` // <= 160 chars, single line
	At   int64  `json:"at"`
}

// BotInput creates (empty ID) or updates a bot. Updates replace all fields.
type BotInput struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Avatar       string    `json:"avatar"`
	Color        string    `json:"color"`
	Description  string    `json:"description"`
	Instructions string    `json:"instructions"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	Reasoning    string    `json:"reasoning"`
	Config       BotConfig `json:"config"`
}

// BotRef identifies a bot.
type BotRef struct {
	ID string `json:"id"`
}

// BotExport is portable: settings and instructions only, never memory, messages or secrets.
type BotExport struct {
	Version int      `json:"version"` // 1
	Bot     BotInput `json:"bot"`     // ID is ignored on import
}

// BotChange is the chat.bot event payload.
type BotChange struct {
	ID     string `json:"id"`
	Action string `json:"action"` // "saved" | "archived" | "cleared"
}

// RunState is the chat.run event payload.
type RunState struct {
	BotID     string `json:"botId"`
	Running   bool   `json:"running"`
	MessageID string `json:"messageId,omitempty"` // the assistant message being produced
}

// Part types.
const (
	PartText       = "text"
	PartReasoning  = "reasoning"
	PartTool       = "tool"       // tool activity row
	PartCitation   = "citation"   // a source
	PartAttachment = "attachment" // user/assistant attachment chip
	PartReport     = "report"     // research report artifact
	PartDelegation = "delegation" // ask_bot step
	PartMemory     = "memory"     // verified memory write ("Memory updated")
	PartNotice     = "notice"     // engine notices (compaction, budget stop)
)

// MsgPart is one renderable piece of a message; fields are used per Type.
type MsgPart struct {
	Type string `json:"type"`
	// text, reasoning, report (markdown), notice
	Text string `json:"text,omitempty"`
	// tool / delegation / research steps
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`   // tool id ("web_search") or delegated bot name
	Status     string `json:"status,omitempty"` // "running" | "done" | "error"
	Summary    string `json:"summary,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	// citation
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	// attachment
	AttachmentID string `json:"attachmentId,omitempty"`
	MIME         string `json:"mime,omitempty"`
	Size         int64  `json:"size,omitempty"`
	// report: sources are carried as citation parts; Title is the report title.
}

// MessageUsage is provider-reported only; never estimated.
type MessageUsage struct {
	InputTokens  *int64   `json:"inputTokens,omitempty"`
	OutputTokens *int64   `json:"outputTokens,omitempty"`
	CostUSD      *float64 `json:"costUsd,omitempty"`
}

// Message statuses.
const (
	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusComplete   = "complete"
	StatusIncomplete = "incomplete" // daemon restarted or run aborted mid-stream
	StatusError      = "error"
	StatusCanceled   = "canceled"
)

// Message is one entry of a bot's conversation.
type Message struct {
	ID        string        `json:"id"`
	BotID     string        `json:"botId"`
	Seq       int64         `json:"seq"`
	Role      string        `json:"role"`   // "user" | "assistant" | "system"
	Author    string        `json:"author"` // "user" | "bot:<id>" | ""
	Parts     []MsgPart     `json:"parts"`
	Status    string        `json:"status"`
	Usage     *MessageUsage `json:"usage,omitempty"`
	Error     string        `json:"error,omitempty"`
	Model     string        `json:"model,omitempty"`
	CreatedAt int64         `json:"createdAt"`
}

// MessagesRequest pages backwards through a conversation: newest first window.
type MessagesRequest struct {
	BotID     string `json:"botId"`
	BeforeSeq int64  `json:"beforeSeq"` // 0 = latest page
	Limit     int    `json:"limit"`     // default 50, max 200
}

// MessagePage returns messages in ascending seq order.
type MessagePage struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"hasMore"` // older messages exist
}

// Send modes.
const (
	ModeChat     = ""
	ModeResearch = "research" // run the research loop for this message (needs the research tool enabled)
)

// SendRequest sends a user message and starts a run. If a run is active the message is queued by the daemon.
type SendRequest struct {
	BotID         string   `json:"botId"`
	Text          string   `json:"text"`
	AttachmentIDs []string `json:"attachmentIds"`
	Mode          string   `json:"mode"`
	ClientNonce   string   `json:"clientNonce"` // idempotency key; a retried nonce returns the stored message
}

// RegenerateRequest replaces the assistant reply to the last user message.
type RegenerateRequest struct {
	BotID     string `json:"botId"`
	MessageID string `json:"messageId"` // must be the last assistant message
}

// DraftRequest runs an unsaved bot configuration against one message (builder preview).
// The exchange is returned through chat.message events with BotID "draft:<DraftID>" and is never stored.
type DraftRequest struct {
	DraftID string      `json:"draftId"`
	Bot     BotInput    `json:"bot"`
	History []DraftTurn `json:"history"`
	Text    string      `json:"text"`
}

// DraftTurn is prior preview context held by the UI.
type DraftTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Attachment describes a stored file. Bytes are fetched with chat.attachments.read only for previews.
type Attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int64  `json:"size"`
}

// AttachmentBegin starts a chunked upload (frames are capped at 4 MiB).
type AttachmentBegin struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// AttachmentUpload is the staged upload handle.
type AttachmentUpload struct {
	ID string `json:"id"`
}

// AttachmentChunk appends the next sequential chunk (Seq starts at 0).
type AttachmentChunk struct {
	ID         string `json:"id"`
	Seq        int    `json:"seq"`
	DataBase64 string `json:"dataBase64"`
}

// AttachmentCommit finishes an upload; the daemon sniffs the type from content, never the extension alone.
type AttachmentCommit struct {
	ID string `json:"id"`
}

// AttachmentRef identifies an attachment.
type AttachmentRef struct {
	ID string `json:"id"`
}

// AttachmentData is a size-capped read for UI previews.
type AttachmentData struct {
	Attachment
	DataBase64 string `json:"dataBase64"`
}

// RawParts decodes a stored parts JSON column.
func RawParts(raw string) []MsgPart {
	var out []MsgPart
	if raw == "" {
		return []MsgPart{}
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []MsgPart{}
	}
	return out
}

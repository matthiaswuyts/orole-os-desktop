package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/manifest"
)

var (
	ChatMessage = mygo.NewEvent[chat.Message]("chat:message")
	ChatRun     = mygo.NewEvent[chat.RunState]("chat:run")
	ChatBot     = mygo.NewEvent[chat.BotChange]("chat:bot")
	ChatMemory  = mygo.NewEvent[chat.MemoryChange]("chat:memory")
	LLMChanged  = mygo.NewEvent[llm.Change]("llm:changed")
)

func forwardChat(event string, data json.RawMessage) bool {
	switch event {
	case "chat.message":
		var v chat.Message
		if json.Unmarshal(data, &v) == nil {
			ChatMessage.Broadcast(v)
		}
	case "chat.run":
		var v chat.RunState
		if json.Unmarshal(data, &v) == nil {
			ChatRun.Broadcast(v)
		}
	case "chat.bot":
		var v chat.BotChange
		if json.Unmarshal(data, &v) == nil {
			ChatBot.Broadcast(v)
		}
	case "chat.memory":
		var v chat.MemoryChange
		if json.Unmarshal(data, &v) == nil {
			ChatMemory.Broadcast(v)
		}
	case "llm.changed":
		var v llm.Change
		if json.Unmarshal(data, &v) == nil {
			LLMChanged.Broadcast(v)
		}
	default:
		return false
	}
	return true
}

// Chat is a stateless bridge; bots, conversations, runs and memory live in the daemon.
type Chat struct{ L *Link }

func (c Chat) Bots(ctx context.Context) ([]chat.Bot, error) {
	var out []chat.Bot
	err := c.L.call(ctx, "chat.bots.list", struct{}{}, &out)
	return out, err
}
func (c Chat) Bot(ctx context.Context, id string) (chat.Bot, error) {
	var out chat.Bot
	err := c.L.call(ctx, "chat.bots.get", chat.BotRef{ID: id}, &out)
	return out, err
}
func (c Chat) SaveBot(ctx context.Context, in chat.BotInput) (chat.Bot, error) {
	var out chat.Bot
	err := c.L.call(ctx, "chat.bots.save", in, &out)
	return out, err
}
func (c Chat) ArchiveBot(ctx context.Context, id string) error {
	return c.L.call(ctx, "chat.bots.archive", chat.BotRef{ID: id}, nil)
}
func (c Chat) ExportBot(ctx context.Context, id string) (chat.BotExport, error) {
	var out chat.BotExport
	err := c.L.call(ctx, "chat.bots.export", chat.BotRef{ID: id}, &out)
	return out, err
}
func (c Chat) ImportBot(ctx context.Context, in chat.BotExport) (chat.Bot, error) {
	var out chat.Bot
	err := c.L.call(ctx, "chat.bots.import", in, &out)
	return out, err
}
func (c Chat) Messages(ctx context.Context, req chat.MessagesRequest) (chat.MessagePage, error) {
	var out chat.MessagePage
	err := c.L.call(ctx, "chat.messages.list", req, &out)
	return out, err
}
func (c Chat) Send(ctx context.Context, req chat.SendRequest) (chat.Message, error) {
	var out chat.Message
	err := c.L.call(ctx, "chat.send", req, &out)
	return out, err
}
func (c Chat) Cancel(ctx context.Context, botID string) error {
	return c.L.call(ctx, "chat.cancel", chat.BotRef{ID: botID}, nil)
}
func (c Chat) Regenerate(ctx context.Context, req chat.RegenerateRequest) error {
	return c.L.call(ctx, "chat.regenerate", req, nil)
}
func (c Chat) Clear(ctx context.Context, botID string) error {
	return c.L.call(ctx, "chat.clear", chat.BotRef{ID: botID}, nil)
}
func (c Chat) DraftSend(ctx context.Context, req chat.DraftRequest) (chat.Message, error) {
	var out chat.Message
	err := c.L.call(ctx, "chat.draft.send", req, &out)
	return out, err
}

func (c Chat) BeginAttachment(ctx context.Context, req chat.AttachmentBegin) (chat.AttachmentUpload, error) {
	var out chat.AttachmentUpload
	err := c.L.call(ctx, "chat.attachments.begin", req, &out)
	return out, err
}
func (c Chat) AttachmentChunk(ctx context.Context, req chat.AttachmentChunk) error {
	return c.L.call(ctx, "chat.attachments.chunk", req, nil)
}
func (c Chat) CommitAttachment(ctx context.Context, id string) (chat.Attachment, error) {
	var out chat.Attachment
	err := c.L.call(ctx, "chat.attachments.commit", chat.AttachmentCommit{ID: id}, &out)
	return out, err
}
func (c Chat) DiscardAttachment(ctx context.Context, id string) error {
	return c.L.call(ctx, "chat.attachments.discard", chat.AttachmentRef{ID: id}, nil)
}
func (c Chat) ReadAttachment(ctx context.Context, id string) (chat.AttachmentData, error) {
	var out chat.AttachmentData
	err := c.L.call(ctx, "chat.attachments.read", chat.AttachmentRef{ID: id}, &out)
	return out, err
}

func (c Chat) MemoryScopes(ctx context.Context, botID string) ([]chat.MemoryScope, error) {
	var out []chat.MemoryScope
	err := c.L.call(ctx, "chat.memory.scopes.list", chat.BotRef{ID: botID}, &out)
	return out, err
}
func (c Chat) CreateMemoryScope(ctx context.Context, name string) (chat.MemoryScope, error) {
	var out chat.MemoryScope
	err := c.L.call(ctx, "chat.memory.scopes.create", chat.MemoryScopeCreate{Name: name}, &out)
	return out, err
}
func (c Chat) DeleteMemoryScope(ctx context.Context, id string) error {
	return c.L.call(ctx, "chat.memory.scopes.delete", chat.MemoryScopeRef{ID: id}, nil)
}
func (c Chat) MemoryEntries(ctx context.Context, req chat.MemoryListRequest) ([]chat.MemoryEntry, error) {
	var out []chat.MemoryEntry
	err := c.L.call(ctx, "chat.memory.entries.list", req, &out)
	return out, err
}
func (c Chat) SaveMemoryEntry(ctx context.Context, req chat.MemorySaveRequest) (chat.MemoryEntry, error) {
	var out chat.MemoryEntry
	err := c.L.call(ctx, "chat.memory.entries.save", req, &out)
	return out, err
}
func (c Chat) DeleteMemoryEntry(ctx context.Context, id string) error {
	return c.L.call(ctx, "chat.memory.entries.delete", chat.MemoryEntryRef{ID: id}, nil)
}
func (c Chat) ResolveMemory(ctx context.Context, req chat.MemoryResolve) error {
	return c.L.call(ctx, "chat.memory.pending.resolve", req, nil)
}
func (c Chat) KnowledgeFiles(ctx context.Context, botID string) ([]chat.KnowledgeFile, error) {
	var out []chat.KnowledgeFile
	err := c.L.call(ctx, "chat.knowledge.list", chat.BotRef{ID: botID}, &out)
	return out, err
}
func (c Chat) AddKnowledge(ctx context.Context, req chat.KnowledgeAdd) (chat.KnowledgeFile, error) {
	var out chat.KnowledgeFile
	err := c.L.call(ctx, "chat.knowledge.add", req, &out)
	return out, err
}
func (c Chat) RemoveKnowledge(ctx context.Context, req chat.KnowledgeRef) error {
	return c.L.call(ctx, "chat.knowledge.remove", req, nil)
}
func (c Chat) SkillProposals(ctx context.Context, botID string) ([]chat.SkillProposal, error) {
	var out []chat.SkillProposal
	err := c.L.call(ctx, "chat.skills.pending.list", chat.BotRef{ID: botID}, &out)
	return out, err
}
func (c Chat) ResolveSkill(ctx context.Context, req chat.SkillResolve) error {
	return c.L.call(ctx, "chat.skills.pending.resolve", req, nil)
}

// LLM is a stateless bridge to the daemon's provider manager. API keys are sent once on connect
// and are never returned.
type LLM struct{ L *Link }

func (l LLM) Providers(ctx context.Context) ([]llm.ProviderStatus, error) {
	var out []llm.ProviderStatus
	err := l.L.call(ctx, "llm.providers.list", struct{}{}, &out)
	return out, err
}
func (l LLM) Connect(ctx context.Context, req llm.ConnectRequest) (llm.ProviderStatus, error) {
	var out llm.ProviderStatus
	err := l.L.call(ctx, "llm.providers.connect", req, &out)
	return out, err
}
func (l LLM) Disconnect(ctx context.Context, id string) error {
	return l.L.call(ctx, "llm.providers.disconnect", llm.ProviderRef{ID: id}, nil)
}
func (l LLM) Test(ctx context.Context, id string) (llm.ProviderStatus, error) {
	var out llm.ProviderStatus
	err := l.L.call(ctx, "llm.providers.test", llm.ProviderRef{ID: id}, &out)
	return out, err
}
func (l LLM) Models(ctx context.Context, id string) ([]llm.Model, error) {
	var out []llm.Model
	err := l.L.call(ctx, "llm.models", llm.ProviderRef{ID: id}, &out)
	return out, err
}
func (l LLM) Defaults(ctx context.Context) (llm.Defaults, error) {
	var out llm.Defaults
	err := l.L.call(ctx, "llm.defaults.get", struct{}{}, &out)
	return out, err
}
func (l LLM) SetDefaults(ctx context.Context, d llm.Defaults) (llm.Defaults, error) {
	var out llm.Defaults
	err := l.L.call(ctx, "llm.defaults.set", d, &out)
	return out, err
}
func (l LLM) OpenRouterAuthStart(ctx context.Context) (llm.AuthStart, error) {
	var out llm.AuthStart
	err := l.L.call(ctx, "llm.openrouter.auth.start", struct{}{}, &out)
	return out, err
}
func (l LLM) OpenRouterAuthFinish(ctx context.Context, code string) (llm.ProviderStatus, error) {
	var out llm.ProviderStatus
	err := l.L.call(ctx, "llm.openrouter.auth.finish", llm.AuthFinish{Code: code}, &out)
	return out, err
}

func (c Chat) Tools(ctx context.Context, req chat.ToolsRequest) ([]chat.ToolInfo, error) {
	var out []chat.ToolInfo
	err := c.L.call(ctx, "chat.tools.list", req, &out)
	return out, err
}

func (l LLM) Manifests(ctx context.Context) ([]manifest.ManifestRecord, error) {
	var out []manifest.ManifestRecord
	err := l.L.call(ctx, "llm.manifests.list", struct{}{}, &out)
	return out, err
}
func (l LLM) SaveManifest(ctx context.Context, req manifest.ManifestSaveRequest) (manifest.ManifestRecord, error) {
	var out manifest.ManifestRecord
	err := l.L.call(ctx, "llm.manifests.save", req, &out)
	return out, err
}
func (l LLM) TrustManifest(ctx context.Context, req manifest.ManifestTrustRequest) (manifest.ManifestRecord, error) {
	var out manifest.ManifestRecord
	err := l.L.call(ctx, "llm.manifests.trust", req, &out)
	return out, err
}
func (l LLM) RemoveManifest(ctx context.Context, req manifest.ManifestRef) error {
	return l.L.call(ctx, "llm.manifests.remove", req, nil)
}

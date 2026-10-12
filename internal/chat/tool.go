package chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// RunContext identifies the run a tool call belongs to.
type RunContext struct {
	BotID string
	Bot   Bot
	// MessageID of the assistant message being produced; "" for background work.
	MessageID string
	// Actor for policy checks: "bot:<id>".
	Actor string
	// Depth is the ask_bot delegation depth (0 = user-initiated run).
	Depth int
	// Chain lists the bot ids of the runs waiting on this run's answer through ask_bot, outermost
	// first (empty for a user-initiated run). A tool calling Delegator.Ask sets it to the chain of
	// the asking run plus the asking bot; the delegated run receives it unchanged.
	Chain []string
	// Automations is the automation chain behind this run (outermost first): set when an automation
	// delivered the message, inherited by hand-offs and their reports; empty for user-started runs.
	Automations []string
	// Author of the message this run answers: "user", or "automation:<id>", "bot:<id>",
	// "report:<id>", "task:<id>" for turns nobody typed.
	Author string
	// Emit publishes a UI part on the running assistant message (progress, citations).
	// It is a no-op for background work. Tools must keep parts small.
	Emit func(MsgPart)
	// Caps of the model serving this run.
	Caps llm.Caps
}

// ToolResult is what the model sees plus what the UI persists.
type ToolResult struct {
	// Text is returned to the model. Content from the outside world MUST be marked Untrusted.
	Text    string
	IsError bool
	// Untrusted text is fenced by the engine (Fence) before the model sees it.
	Untrusted bool
	// Parts are persisted on the assistant message (citations, report, memory confirmation, ...).
	Parts []MsgPart
}

// Tool is a client-run tool. Hosted tools (web_search, web_fetch, datetime on providers that run
// them) are llm.ToolSpec.Hosted entries produced by the hosted-tool Tool and never reach Call.
type Tool interface {
	// ID is the stable name: the function name for client tools, or the BotTool.ID for hosted ones.
	ID() string
	// Specs returns the llm.ToolSpec(s) to offer for this bot and model, or nil when the tool is
	// disabled for the bot or unsupported by the model. A tool must never be offered if it cannot run.
	Specs(bot Bot, caps llm.Caps) []llm.ToolSpec
	// Call executes a client tool call. name is the function name from the spec (a Tool may expose several).
	Call(ctx context.Context, rc RunContext, name string, args json.RawMessage) (ToolResult, error)
}

// Memory is the engine's view of the memory subsystem (implemented in internal/chat/memory).
type Memory interface {
	// Snapshot renders the always-in-prompt core memory for a bot (own + granted scopes). It is taken
	// once per run and must be stable for the run, so provider prompt caches stay valid.
	Snapshot(ctx context.Context, bot Bot) (string, error)
	// Recall returns a small fenced block of relevant archival facts and knowledge for the user's text.
	Recall(ctx context.Context, bot Bot, query string) (string, error)
	// Tools are the memory-related client tools (memory, memory_search, search_history,
	// knowledge_search, ask_bot, propose_skill).
	Tools() []Tool
	// Consolidate extracts facts from the bot's unprocessed conversation tail (idle job).
	Consolidate(ctx context.Context, botID string) error
}

// Delegator lets ask_bot hand off work to another bot without importing the engine.
type Delegator interface {
	// Handoff gives task to the callee bot as a real turn of its own conversation (stored messages, live events, its
	// own memory and tools, queued behind anything it is already doing) and returns once it is queued: the delegating
	// run is not blocked. When the callee finishes, its outcome is delivered to the delegating bot as a report.
	// from.Chain must hold the chain of the delegating run plus the delegating bot.
	Handoff(ctx context.Context, from RunContext, calleeID, task string) error
}

// Fence wraps untrusted text so the model treats it as data. Engines and tools use this one function.
func Fence(source, text string) string {
	source = strings.Map(func(r rune) rune {
		if r == '"' || r == '<' || r == '>' || r < 0x20 {
			return '_'
		}
		return r
	}, source)
	text = strings.ReplaceAll(text, "</untrusted", "< /untrusted")
	return "<untrusted source=\"" + source + "\">\n" + text + "\n</untrusted>"
}

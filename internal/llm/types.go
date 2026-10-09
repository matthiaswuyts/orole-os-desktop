// Package llm is the single seam between Orole's chat engine and model APIs.
//
// Providers talk to a vendor API directly with the user's API key. They never
// shell out to a harness and never read subscription credentials of other tools.
// The chat engine depends only on the types in this file.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// Role of a Message in a Request.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// PartKind discriminates Part.
type PartKind string

const (
	PartText       PartKind = "text"
	PartImage      PartKind = "image"       // Data holds the raw bytes, MIME the media type
	PartFile       PartKind = "file"        // PDFs and other provider-parsed documents; Data raw bytes
	PartToolCall   PartKind = "tool_call"   // assistant requested a client-run tool
	PartToolResult PartKind = "tool_result" // result for ToolCallID
)

// Part is one piece of message content.
type Part struct {
	Kind PartKind `json:"kind"`
	Text string   `json:"text,omitempty"`
	// Image/File content. Bounded by the engine before it reaches a provider.
	MIME string `json:"mime,omitempty"`
	Name string `json:"name,omitempty"`
	Data []byte `json:"data,omitempty"`
	// Tool call / result.
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
}

// Message is one conversation turn sent to a provider.
type Message struct {
	Role  Role   `json:"role"`
	Parts []Part `json:"parts"`
}

// Hosted tool identifiers. A ToolSpec with Hosted set is executed by the provider
// (or the provider's gateway); the engine never runs it and never sees a call.
const (
	HostedWebSearch = "web_search"
	HostedWebFetch  = "web_fetch"
	HostedDatetime  = "datetime"
)

// ToolSpec describes a tool offered to the model.
type ToolSpec struct {
	// Name is the function name for client tools ([a-zA-Z0-9_-]{1,64}).
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"` // JSON Schema object, client tools only
	// Hosted is one of the Hosted* constants for provider-run tools.
	Hosted string `json:"hosted,omitempty"`
	// Params carries hosted-tool options (max_results, allowed_domains, blocked_domains,
	// max_uses, max_content_tokens). Providers ignore options they cannot honour.
	Params map[string]any `json:"params,omitempty"`
}

// Request is one model invocation.
type Request struct {
	Model     string     `json:"model"`
	System    string     `json:"system,omitempty"`
	Messages  []Message  `json:"messages"`
	Tools     []ToolSpec `json:"tools,omitempty"`
	Reasoning string     `json:"reasoning,omitempty"` // "", "low", "medium", "high" ...; must be in Model.Reasoning
	// MaxOutputTokens 0 = provider default.
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
	// MaxHostedToolCalls caps provider-side tool loops (OpenRouter max_tool_calls). 0 = provider default.
	MaxHostedToolCalls int `json:"maxHostedToolCalls,omitempty"`
	// PDFEngine selects how providers without native PDF input parse files: "", "cloudflare-ai", "mistral-ocr", "native".
	PDFEngine string `json:"pdfEngine,omitempty"`
}

// DeltaKind discriminates Delta.
type DeltaKind string

const (
	DeltaText      DeltaKind = "text"
	DeltaReasoning DeltaKind = "reasoning"
	DeltaToolCall  DeltaKind = "tool_call"   // a complete client tool call (arguments fully assembled)
	DeltaServer    DeltaKind = "server_tool" // activity of a hosted tool (searching, fetching)
	DeltaCitation  DeltaKind = "citation"
	DeltaUsage     DeltaKind = "usage"
	DeltaStop      DeltaKind = "stop"  // exactly one, last unless DeltaError
	DeltaError     DeltaKind = "error" // terminal
)

// ToolCall is a fully assembled client tool call.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ServerTool reports hosted tool activity for the UI ("Searched the web").
type ServerTool struct {
	ID     string `json:"id"`
	Tool   string `json:"tool"`   // Hosted* identifier
	Status string `json:"status"` // "running" | "done" | "error"
	Detail string `json:"detail,omitempty"`
}

// Citation is a source the provider attached to the answer.
type Citation struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Usage is provider-reported only. Nil pointers mean "not reported"; never estimate.
type Usage struct {
	InputTokens     *int64   `json:"inputTokens,omitempty"`
	OutputTokens    *int64   `json:"outputTokens,omitempty"`
	CachedTokens    *int64   `json:"cachedTokens,omitempty"`
	ReasoningTokens *int64   `json:"reasoningTokens,omitempty"`
	CostUSD         *float64 `json:"costUsd,omitempty"`
}

// StopReason is why generation ended.
type StopReason string

const (
	StopEnd       StopReason = "end"
	StopToolCalls StopReason = "tool_calls"
	StopLength    StopReason = "length"
	StopFilter    StopReason = "filter"
)

// Delta is one streamed event. Exactly one payload field is set for its Kind.
type Delta struct {
	Kind     DeltaKind   `json:"kind"`
	Text     string      `json:"text,omitempty"`
	Call     *ToolCall   `json:"call,omitempty"`
	Server   *ServerTool `json:"server,omitempty"`
	Citation *Citation   `json:"citation,omitempty"`
	Usage    *Usage      `json:"usage,omitempty"`
	Stop     StopReason  `json:"stop,omitempty"`
	Err      *Error      `json:"err,omitempty"`
}

// ErrorCode classifies provider failures for the engine and for user-facing messages.
type ErrorCode string

const (
	ErrAuth          ErrorCode = "auth"           // missing/invalid/revoked key
	ErrRateLimit     ErrorCode = "rate_limit"     // RetryAfter may be set
	ErrQuota         ErrorCode = "quota"          // credits exhausted / billing
	ErrContextLength ErrorCode = "context_length" // input too large for the model
	ErrFilter        ErrorCode = "filter"         // refused/blocked by provider policy
	ErrBadRequest    ErrorCode = "bad_request"    // unsupported parameter, modality, model
	ErrNetwork       ErrorCode = "network"
	ErrServer        ErrorCode = "server"
	ErrCanceled      ErrorCode = "canceled"
)

// Error is the only error type a Provider returns or streams.
type Error struct {
	Code       ErrorCode     `json:"code"`
	Message    string        `json:"message"` // safe to show; never contains keys
	Status     int           `json:"status,omitempty"`
	RetryAfter time.Duration `json:"retryAfter,omitempty"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// AsError converts any error to *Error (ErrNetwork fallback; context.Canceled -> ErrCanceled).
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: ErrCanceled, Message: "canceled"}
	}
	return &Error{Code: ErrNetwork, Message: err.Error()}
}

// Caps describe what a model can do. Values come from the provider's catalogue, never guesses.
type Caps struct {
	Tools         bool     `json:"tools"`
	Vision        bool     `json:"vision"`
	PDF           bool     `json:"pdf"`
	Reasoning     bool     `json:"reasoning"`
	HostedTools   []string `json:"hostedTools"`   // Hosted* identifiers the endpoint can run
	DeepResearch  bool     `json:"deepResearch"`  // native background research model
	ContextWindow int64    `json:"contextWindow"` // 0 = unknown
}

// Model is one selectable model.
type Model struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Caps      Caps     `json:"caps"`
	Reasoning []string `json:"reasoning"` // accepted Request.Reasoning levels, empty = none
}

// Provider is a direct model API.
type Provider interface {
	ID() string
	// Models lists chat-capable models. Implementations may cache briefly.
	Models(ctx context.Context) ([]Model, error)
	// Stream starts a generation. The channel is closed after the terminal Delta
	// (DeltaStop or DeltaError). Cancelling ctx ends the stream promptly with
	// DeltaError{ErrCanceled} or channel close; goroutines must not leak.
	Stream(ctx context.Context, req Request) (<-chan Delta, error)
	// Validate checks the credentials with the cheapest authenticated call.
	Validate(ctx context.Context) (Account, error)
}

// Account is what Validate learned about the connection. Never contains the key.
type Account struct {
	Label     string   `json:"label,omitempty"`     // e.g. key label / org
	Remaining *float64 `json:"remaining,omitempty"` // credits left (USD) when reported
	Limit     *float64 `json:"limit,omitempty"`
}

// Config builds a Provider.
type Config struct {
	APIKey  string
	BaseURL string // empty = vendor default
	HTTP    HTTPDoer
}

// HTTPDoer is satisfied by *http.Client; tests inject httptest-backed clients.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

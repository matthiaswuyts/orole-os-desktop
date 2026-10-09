package llm

import (
	"context"
	"errors"
	"strings"
)

// Built-in provider identifiers.
const (
	ProviderOpenRouter = "openrouter"
	ProviderOpenAI     = "openai"
)

// ErrNotConfigured means the provider has no stored credentials.
var ErrNotConfigured = errors.New("provider is not connected")

// ProviderStatus is what the UI may know about a provider. Never contains the key.
type ProviderStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "builtin" | "manifest"
	Connected bool   `json:"connected"`
	KeyHint   string `json:"keyHint,omitempty"` // last 4 characters only
	// Valid is the outcome of the last Test: nil = not tested since daemon start.
	Valid   *bool    `json:"valid,omitempty"`
	Error   string   `json:"error,omitempty"`
	Account *Account `json:"account,omitempty"`
}

// Defaults are the models Chat uses when a bot does not choose its own.
type Defaults struct {
	// ChatModel is "provider/model" (the model id may itself contain slashes).
	ChatModel string `json:"chatModel"`
	// MemoryModel runs background work (summaries, memory consolidation). Cheap model preferred.
	MemoryModel string `json:"memoryModel"`
}

// AuthStart begins OpenRouter's OAuth PKCE headless flow. The verifier stays in the daemon.
type AuthStart struct {
	URL string `json:"url"` // open in the browser; the page displays a code to paste
}

// Service is the daemon-side provider manager (credentials just-in-time, defaults, test).
type Service interface {
	Statuses(ctx context.Context) ([]ProviderStatus, error)
	Connect(ctx context.Context, id, apiKey string) (ProviderStatus, error) // validates, then stores
	Disconnect(ctx context.Context, id string) error
	Test(ctx context.Context, id string) (ProviderStatus, error)
	Models(ctx context.Context, id string) ([]Model, error)
	// Provider resolves a ready provider; ErrNotConfigured when not connected.
	Provider(ctx context.Context, id string) (Provider, error)
	Defaults(ctx context.Context) (Defaults, error)
	SetDefaults(ctx context.Context, d Defaults) error
	OpenRouterAuthStart(ctx context.Context) (AuthStart, error)
	OpenRouterAuthFinish(ctx context.Context, code string) (ProviderStatus, error)
}

// SplitModel splits "provider/model" at the first slash.
func SplitModel(sel string) (provider, model string, ok bool) {
	provider, model, found := strings.Cut(sel, "/")
	return provider, model, found && provider != "" && model != ""
}

// IPC parameter types. Methods (daemon names):
//
//	llm.providers.list        struct{}        -> []ProviderStatus
//	llm.providers.connect     ConnectRequest  -> ProviderStatus  (validates the key, then stores it)
//	llm.providers.disconnect  ProviderRef     -> struct{}
//	llm.providers.test        ProviderRef     -> ProviderStatus
//	llm.models                ProviderRef     -> []Model
//	llm.defaults.get          struct{}        -> Defaults
//	llm.defaults.set          Defaults        -> Defaults
//	llm.openrouter.auth.start struct{}        -> AuthStart
//	llm.openrouter.auth.finish AuthFinish     -> ProviderStatus
//
// Event llm.changed (Change) fires after connect, disconnect and defaults changes.

// ProviderRef identifies a provider.
type ProviderRef struct {
	ID string `json:"id"`
}

// ConnectRequest carries a pasted API key. The key is never echoed back or logged.
type ConnectRequest struct {
	ID     string `json:"id"`
	APIKey string `json:"apiKey"`
}

// AuthFinish carries the code the user pasted from OpenRouter's authorization page.
type AuthFinish struct {
	Code string `json:"code"`
}

// Change is the llm.changed event payload.
type Change struct {
	ID string `json:"id,omitempty"`
}

// Package automations executes durable, daemon-owned workflows. Definitions and
// the write-ahead step journal live in SQLite; secrets live in credentials.Store.
package automations

import (
	"context"
	"encoding/json"
	"time"
)

const SchemaVersion = 1
const MaxDepth = 3

// Definition is a versioned automation document.
type Definition struct {
	SchemaVersion      int      `json:"schemaVersion"`
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	Enabled            bool     `json:"enabled"`
	Trigger            Trigger  `json:"trigger"`
	Steps              []Step   `json:"steps"`
	AllowedExecutables []string `json:"allowedExecutables"`
	AllowedHosts       []string `json:"allowedHosts"`
	AllowHTTP          bool     `json:"allowHttp"`
	Concurrency        int      `json:"concurrency"`
	TimeoutSec         int      `json:"timeoutSec"`
}
type Trigger struct {
	Type     string `json:"type"`
	Schedule string `json:"schedule"`
	CatchUp  string `json:"catchUp"`
	Event    string `json:"event"`
	Filter   string `json:"filter"`
}
type Step struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Params     json.RawMessage `json:"params"`
	When       string          `json:"when"`
	TimeoutSec int             `json:"timeoutSec"`
	Retries    int             `json:"retries"`
	BackoffMs  int             `json:"backoffMs"`
	OnError    string          `json:"onError"`
	// Strategy is at-most-once, key (executor must deduplicate), or repeatable
	// (the user explicitly asserts that repeating the action is safe).
	Strategy string `json:"strategy"`
}
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}
type SaveResult struct {
	Definition Definition `json:"definition"`
	Issues     []Issue    `json:"issues"`
	Saved      bool       `json:"saved"`
}
type Summary struct {
	Definition Definition `json:"definition"`
	LastStatus string     `json:"lastStatus"`
	LastRunAt  int64      `json:"lastRunAt"`
}
type Run struct {
	ID           string          `json:"id"`
	AutomationID string          `json:"automationId"`
	Name         string          `json:"name"`
	Status       string          `json:"status"`
	TriggerType  string          `json:"triggerType"`
	TriggerData  json.RawMessage `json:"triggerData"`
	Chain        []string        `json:"chain"`
	Error        string          `json:"error"`
	CreatedAt    int64           `json:"createdAt"`
	StartedAt    int64           `json:"startedAt"`
	FinishedAt   int64           `json:"finishedAt"`
	Definition   Definition      `json:"definition"`
}
type Journal struct {
	RunID      string          `json:"runId"`
	StepID     string          `json:"stepId"`
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	Attempt    int             `json:"attempt"`
	Key        string          `json:"key"`
	Input      json.RawMessage `json:"input"`
	Output     json.RawMessage `json:"output"`
	Error      string          `json:"error"`
	StartedAt  int64           `json:"startedAt"`
	FinishedAt int64           `json:"finishedAt"`
}
type Detail struct {
	Run   Run       `json:"run"`
	Steps []Journal `json:"steps"`
}
type Approval struct {
	RunID     string `json:"runId"`
	StepID    string `json:"stepId"`
	Name      string `json:"name"`
	Message   string `json:"message"`
	ExpiresAt int64  `json:"expiresAt"`
}
type Config struct {
	GlobalConcurrency int  `json:"globalConcurrency"`
	PerAutomation     int  `json:"perAutomation"`
	WebhookEnabled    bool `json:"webhookEnabled"`
	WebhookPort       int  `json:"webhookPort"`
	HasSecret         bool `json:"hasSecret"`
}

func DefaultConfig() Config {
	return Config{GlobalConcurrency: 4, PerAutomation: 1, WebhookPort: 17843}
}

// Field describes a typed step parameter for the structured editor.
type Field struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
	Hint     string   `json:"hint"`
}
type StepType struct {
	Type         string  `json:"type"`
	Label        string  `json:"label"`
	Available    bool    `json:"available"`
	Reason       string  `json:"reason"`
	SideEffect   bool    `json:"sideEffect"`
	KeySupported bool    `json:"keySupported"`
	Fields       []Field `json:"fields"`
}

// Clock is injected in scheduler/backoff tests.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Adapters receive provenance and a stable per-(run,step) key. Implementations
// exposing strategy=key MUST durably deduplicate the key before side effects.
type ToolRequest struct {
	Server string         `json:"server"`
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args"`
	Key    string         `json:"key"`
	Actor  string         `json:"actor"`
	Chain  []string       `json:"chain"`
}
type ToolInvoker interface {
	InvokeTool(context.Context, ToolRequest) (any, error)
}
type CommandRequest struct {
	Executable string
	Args       []string
	Dir        string
	Env        map[string]string
}
type CommandResult struct {
	ExitCode  int    `json:"exitCode"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
	Passed    bool   `json:"passed"`
}
type CommandRunner interface {
	RunCommand(context.Context, CommandRequest) (CommandResult, error)
}
type AgentRequest struct {
	AgentID   string   `json:"agentId"`
	ProjectID string   `json:"projectId"`
	Adapter   string   `json:"adapter"`
	Prompt    string   `json:"prompt"`
	Key       string   `json:"key"`
	Actor     string   `json:"actor"`
	Chain     []string `json:"chain"`
}
type AgentControl interface {
	StartAgent(context.Context, AgentRequest) (any, error)
	SendPrompt(context.Context, AgentRequest) (any, error)
	WaitForAgent(context.Context, string) (any, error)
}
type QueryRequest struct {
	Kind  string `json:"kind"`
	Repo  string `json:"repo"`
	Query string `json:"query"`
	Limit int    `json:"limit"`
}
type GitHubQuery interface {
	QueryGitHub(context.Context, QueryRequest) (any, error)
}
type NotificationRequest struct {
	Title  string
	Body   string
	Level  string
	Source string
	Key    string
}
type Notifier interface {
	Notify(context.Context, NotificationRequest) (any, error)
}

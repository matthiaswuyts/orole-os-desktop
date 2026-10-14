// Package automations executes durable, daemon-owned workflows. Definitions and
// the write-ahead step journal live in SQLite; secrets live in credentials.Store.
package automations

import (
	"context"
	"encoding/json"
	"time"
)

// SchemaVersion 2 nests conditional lanes (Step.Then/Else) instead of v1's forward
// "branch" jumps. Stored v1 documents are upgraded when read (Upgrade).
const SchemaVersion = 2
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
	// Watch configures a "watch" trigger.
	Watch *Watch `json:"watch,omitempty"`
}

// Watch is a "watch" trigger: the daemon polls an integration (there is no network
// listener) and runs the flow once for every item it has not seen before. The first
// poll after the watch is created or changed only records a baseline.
type Watch struct {
	Source string `json:"source"` // github | mcp
	// Kind is, for github: push | pr_opened | pr_merged | workflow_failed | issue_opened;
	// for mcp: deployment.
	Kind   string `json:"kind"`
	Repo   string `json:"repo,omitempty"`   // github: owner/name
	Branch string `json:"branch,omitempty"` // github push and workflow_failed: branch ("" = default branch / any)
	// Server and Tool name a connected MCP integration tool that lists deployments (Vercel
	// list_deployments, Railway list-deployments...); Args are its arguments.
	Server string         `json:"server,omitempty"`
	Tool   string         `json:"tool,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
	// States limits mcp deployment items to these states (case-insensitive); empty = any change.
	States      []string `json:"states,omitempty"`
	IntervalSec int      `json:"intervalSec,omitempty"` // 0 = default (60 s GitHub, 120 s MCP); minimum 30
}

// WatchItem is one polled item: Key identifies it across polls, Data becomes event.data.
type WatchItem struct {
	Key  string
	Data map[string]any
}

// WatchSource polls one integration. actor is "automation:<id>"; sources that call
// policy-checked tools must use it. Items are returned newest first.
type WatchSource interface {
	Poll(ctx context.Context, actor string, w Watch) ([]WatchItem, error)
}

// WatchStatus is a watch trigger's last poll, for the builder.
type WatchStatus struct {
	PolledAt int64  `json:"polledAt"`
	Error    string `json:"error"`
	Seen     int    `json:"seen"`
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
	// Then and Else are the lanes of a "condition" step; empty for every other type.
	Then []Step `json:"then,omitempty"`
	Else []Step `json:"else,omitempty"`
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
	NextRunAt  int64      `json:"nextRunAt"` // next scheduled fire time; 0 when not scheduled
	// Issues are the definition's validation problems; a flow with issues cannot run.
	Issues []Issue `json:"issues"`
	// Watch is the last poll of a watch trigger; nil before its first poll.
	Watch *WatchStatus `json:"watch,omitempty"`
	// CreatedBy is "user", or "bot:<id>" for a bot's draft the user approved in chat.
	CreatedBy string `json:"createdBy"`
	// UpdatedAt is when the definition was last saved (Unix milliseconds).
	UpdatedAt int64 `json:"updatedAt"`
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
	// RetentionDays and RetentionMaxRuns bound the finished run history. Runs
	// that are queued, running or waiting for approval are never removed.
	// RetentionMaxRuns applies per automation. Zero means the default.
	RetentionDays    int `json:"retentionDays"`
	RetentionMaxRuns int `json:"retentionMaxRuns"`
}

func DefaultConfig() Config {
	return Config{GlobalConcurrency: 4, PerAutomation: 1, WebhookPort: 17843, RetentionDays: DefaultRetentionDays, RetentionMaxRuns: DefaultRetentionMaxRuns}
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
	Group        string  `json:"group"` // Bots | Tasks | Notify | Integrations | Logic | Run
	Description  string  `json:"description"`
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
	AgentID        string   `json:"agentId"`
	ProjectID      string   `json:"projectId"`
	Adapter        string   `json:"adapter"`
	Prompt         string   `json:"prompt"`
	Title          string   `json:"title"`
	Model          string   `json:"model"`
	PermissionMode string   `json:"permissionMode"` // manual | auto; never yolo
	NewWorktree    bool     `json:"newWorktree"`
	Until          string   `json:"until"` // task.wait: finished | attention
	Key            string   `json:"key"`
	Actor          string   `json:"actor"`
	Chain          []string `json:"chain"`
}
type AgentControl interface {
	StartAgent(context.Context, AgentRequest) (any, error)
	SendPrompt(context.Context, AgentRequest) (any, error)
	WaitForAgent(context.Context, AgentRequest) (any, error)
}

// BotRequest delivers a message from a flow into a bot's conversation.
type BotRequest struct {
	BotID          string   `json:"botId"`
	Message        string   `json:"message"`
	Wait           bool     `json:"wait"` // wait for the bot's reply and return it as the output
	AutomationID   string   `json:"automationId"`
	AutomationName string   `json:"automationName"`
	Chain          []string `json:"chain"`
}
type BotMessenger interface {
	TellBot(context.Context, BotRequest) (any, error)
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
	Title        string
	Body         string
	Level        string
	Source       string
	AutomationID string
	Key          string
}
type Notifier interface {
	Notify(context.Context, NotificationRequest) (any, error)
}

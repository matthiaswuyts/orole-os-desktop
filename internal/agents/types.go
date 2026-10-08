// Package agents runs coding agents (Claude Code, Codex, OpenCode, Gemini CLI, custom CLIs)
// in daemon-owned sessions, locally or on a paired SSH machine.
//
// Local processes live in per-session Windows job objects and die with the daemon.
// Remote processes use pinned, revocable SSH transports; losing that transport does
// not prove the remote process or descendants stopped. Recovery marks either kind
// lost. Only local adapters with a structured resume contract resume CLI sessions.
package agents

import (
	"encoding/json"
	"time"
)

// Status is where a session sits in the dashboard.
type Status string

const (
	StatusIdle     Status = "idle"      // alive (or created) but no task in flight
	StatusWorking  Status = "working"   // a turn is running
	StatusNeedsYou Status = "needs-you" // waiting on a permission decision or user input
	StatusDone     Status = "done"      // last turn finished successfully
	StatusFailed   Status = "failed"    // last turn or the process failed
	StatusStopped  Status = "stopped"   // stopped by the user
	StatusLost     Status = "lost"      // process vanished with a previous daemon run
)

// Terminal reports whether no further events can arrive without a (re)start.
func (s Status) Terminal() bool {
	return s == StatusFailed || s == StatusStopped || s == StatusLost
}

// Mode is how an agent is run.
type Mode string

const (
	// ModeStructured runs the CLI headless over pipes speaking its structured protocol
	// (Claude Code stream-json). Status and permission requests come from that protocol.
	ModeStructured Mode = "structured"
	// ModeTerminal runs the CLI interactively in a ConPTY terminal the UI can attach to.
	// Status comes from CLI hooks where the CLI has them (Claude Code), never from parsing
	// terminal output; CLIs without hooks only report running/exited.
	ModeTerminal Mode = "terminal"
	// ModeProcess is a remote one-shot CLI with text output and exit status only.
	ModeProcess Mode = "process"
)

// Review decisions recorded on a session.
const (
	ReviewNone     = ""
	ReviewApproved = "approved"
	ReviewSentBack = "sent-back"
	ReviewDiscard  = "discarded"
)

// Check states.
const (
	CheckNone    = ""
	CheckRunning = "running"
	CheckPassing = "passing"
	CheckFailing = "failing"
)

// PermissionRequest is a tool-use approval the agent is blocked on.
type PermissionRequest struct {
	ID      string          `json:"id"`
	Tool    string          `json:"tool"`
	Summary string          `json:"summary"`
	Input   json.RawMessage `json:"input"`
	At      time.Time       `json:"at"`
}

// Session is one agent run, persisted in agent_sessions.
type Session struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`    // claude | codex | opencode | gemini | custom
	Machine string `json:"machine"` // "local" or a paired machine id
	Project string `json:"project"` // repository the working directory belongs to ("" if none)
	Dir     string `json:"dir"`     // working directory (a worktree when one was created)
	Branch  string `json:"branch"`
	BaseRef string `json:"baseRef"` // commit the work is reviewed against
	// WorktreeOwned is set when the daemon created Dir as a worktree for this session.
	WorktreeOwned bool   `json:"worktreeOwned"`
	Model         string `json:"model"`
	Reasoning     string `json:"reasoning,omitempty"`
	Title         string `json:"title"`
	Task          string `json:"task"`
	Mode          Mode   `json:"mode"`
	Command       string `json:"command,omitempty"` // custom CLI command line
	// PermissionMode is the Claude Code permission mode the session runs with.
	PermissionMode string `json:"permissionMode"`
	Pid            int    `json:"pid"`
	Status         Status `json:"status"`
	// Live is true while the agent process is running (runtime state, not persisted).
	Live        bool   `json:"live"`
	Activity    string `json:"activity"`    // what the agent is doing right now
	Tail        string `json:"tail"`        // last few lines of output, for the card
	LastMessage string `json:"lastMessage"` // the agent's most recent message in full
	// AgentSessionID is the CLI's own session id, used to resume.
	AgentSessionID string `json:"agentSessionId"`
	TerminalID     string `json:"terminalId"`    // ConPTY terminal id while the session has one
	TranscriptRef  string `json:"transcriptRef"` // path of the CLI's own transcript when known
	ParentID       string `json:"parentId"`      // session this one was handed off / reviews
	Group          string `json:"group"`         // try-N-ways group
	// Chain records automation provenance for lifecycle events, never tool authority.
	Chain        []string `json:"chain,omitempty"`
	Review       string   `json:"review"` // see Review* constants
	ReviewNote   string   `json:"reviewNote"`
	CheckState   string   `json:"checkState"`
	CheckCommand string   `json:"checkCommand"`
	CheckOutput  string   `json:"checkOutput"`
	Files        int      `json:"files"`
	Additions    int      `json:"additions"`
	Deletions    int      `json:"deletions"`
	Turns        int      `json:"turns"`   // completed turns
	Prompts      int      `json:"prompts"` // prompts sent
	// Pending are the permission requests awaiting a decision (runtime state).
	Pending   []PermissionRequest `json:"pending"`
	Result    string              `json:"result"`
	Error     string              `json:"error"`
	ExitCode  *int                `json:"exitCode,omitempty"`
	StartedAt time.Time           `json:"startedAt"`
	EndedAt   *time.Time          `json:"endedAt,omitempty"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

// Event kinds.
const (
	EvInit        = "init"        // CLI session established; Data = {agentSessionId, model}
	EvPrompt      = "prompt"      // a prompt was sent to the agent
	EvMessage     = "message"     // assistant text
	EvTool        = "tool"        // tool use; Text = summary
	EvToolResult  = "tool_result" // tool output summary
	EvPermission  = "permission"  // blocked on approval; Data = PermissionRequest
	EvDecision    = "decision"    // approval decision; Data = {id, allow, reason}
	EvTurnDone    = "turn_done"   // turn finished; Data = {ok, result}
	EvNeedsInput  = "needs_input" // CLI is waiting on the user (hook notification)
	EvWorking     = "working"     // a turn started
	EvActivity    = "activity"    // activity update only
	EvError       = "error"       // error text
	EvExit        = "exit"        // process ended; Data = {code}
	EvSystem      = "system"      // daemon-authored note (resume, recovery, review...)
	EvTranscript  = "transcript"  // transcript path known; Text = path
	EvPermCancel  = "perm_cancel" // agent withdrew a permission request; Data = {id}
	EvOutputBytes = "output"      // terminal output counter bump (not persisted)
)

// Event is one entry in a session's history.
type Event struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"sessionId"`
	At        time.Time       `json:"at"`
	Kind      string          `json:"kind"`
	Text      string          `json:"text"`
	Data      json.RawMessage `json:"data"`
}

// Detection reports whether an agent CLI is installed.
type Detection struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Error     string `json:"error"`
	// Modes this adapter can run in; Structured/Resume/Permissions describe what the UI
	// can honestly rely on.
	Modes       []Mode `json:"modes"`
	Resume      bool   `json:"resume"`      // can continue a session by the CLI's session id
	Permissions bool   `json:"permissions"` // permission requests can be approved from the dashboard
	StatusFeed  string `json:"statusFeed"`  // "stream-json" | "hooks" | "process" (exit only)
}

// StartSpec is a request to start an agent.
type StartSpec struct {
	Kind      string `json:"kind"`
	Machine   string `json:"machine,omitempty"`   // empty means local
	Reasoning string `json:"reasoning,omitempty"` // empty preserves the CLI default
	Dir       string `json:"dir"`                 // project or worktree directory
	Prompt    string `json:"prompt"`              // the task; may be empty (session starts idle)
	Model     string `json:"model"`               // optional
	Mode      Mode   `json:"mode"`                // "" = adapter default
	Command   string `json:"command"`             // custom CLI command line
	Title     string `json:"title"`
	// PermissionMode: "" | default | acceptEdits | plan (Claude Code).
	PermissionMode string `json:"permissionMode"`
	// NewWorktree creates a fresh worktree of the repository at Dir on a new branch and
	// runs the agent there.
	NewWorktree bool   `json:"newWorktree"`
	Branch      string `json:"branch"` // branch name for NewWorktree ("" = generated)
	BaseRef     string `json:"baseRef"`
	ParentID    string `json:"parentId"`
	Group       string `json:"group"`
	// Key makes the request idempotent: the same key never starts a second session.
	Key string `json:"key"`
}

// ReviewDiff is the work a session produced, for the Review view.
type ReviewDiff struct {
	SessionID string `json:"sessionId"`
	BaseRef   string `json:"baseRef"`
	Dir       string `json:"dir"`
	Files     int    `json:"files"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Truncated bool   `json:"truncated"`
	Diff      Diff   `json:"diff"`
	Error     string `json:"error"`
}

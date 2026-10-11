// Package gateway is Orole's MCP gateway: the one stdio MCP server ("orole") every harness is
// given, so skills and MCP servers are configured once in Orole and reach whichever model or
// harness runs.
//
// The package has three parts. Hub is the daemon side: it lists the tools of every enabled and
// connected MCP server and routes calls through the MCP manager's checked path (policy, approval,
// audit). Server is the MCP server speaking to the harness; it needs only a Backend. The
// gateway/stdio subpackage is the `orole-daemon mcp-gateway` entry point that binds a Server to
// the authenticated daemon pipe.
//
// Everything an MCP server returns is untrusted data and is forwarded as data.
package gateway

import (
	"context"
	"encoding/json"

	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/skills"
)

// IPC methods the gateway process calls on the daemon.
const (
	MethodTools  = "gateway.tools"
	MethodCall   = "gateway.call"
	MethodSkills = "gateway.skills"
	MethodSkill  = "gateway.skill"
)

// Events that make the gateway re-list its tools.
var RefreshEvents = []string{"mcp.status", "mcp.removed", "skills.changed"}

// Environment the gateway process reads. OROLE_HOME selects the daemon's data directory (the
// same variable the daemon honours); the session id attributes calls to `agent:<id>` in policy.
const (
	EnvHome    = "OROLE_HOME"
	EnvSession = "OROLE_GATEWAY_SESSION"
)

// Names of the built-in gateway tools.
const (
	ToolListSkills = "orole_list_skills"
	ToolGetSkill   = "orole_get_skill"
)

// Tool is one proxied MCP tool as the daemon exposes it.
type Tool struct {
	Name         string          `json:"name"`   // exposed name: <server>__<tool>
	Server       string          `json:"server"` // server display name
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	ReadOnly     bool            `json:"readOnly,omitempty"`
	Destructive  *bool           `json:"destructive,omitempty"`
	Idempotent   bool            `json:"idempotent,omitempty"`
	OpenWorld    *bool           `json:"openWorld,omitempty"`
}

// ToolsParams is the gateway.tools request. ETag is the ToolList.ETag the caller already holds
// ("" = none); when it is still current the daemon answers NotModified without the tool bodies.
type ToolsParams struct {
	SessionID string `json:"sessionId"`
	ETag      string `json:"etag,omitempty"`
}

// ToolList is the gateway.tools answer. ETag identifies the exact content of Tools (it does not
// change when only connection state or permissions churn). NotModified means the caller's ETag is
// current; Tools is then empty.
type ToolList struct {
	ETag        string `json:"etag"`
	NotModified bool   `json:"notModified,omitempty"`
	Tools       []Tool `json:"tools"`
}

// CallParams is the gateway.call request. Arguments is the raw JSON object the model produced.
type CallParams struct {
	SessionID string          `json:"sessionId"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// SkillParams is the gateway.skill request; File "" means SKILL.md.
type SkillParams struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// SkillContent is one served skill file.
type SkillContent struct {
	Name    string   `json:"name"`
	File    string   `json:"file"`
	Content string   `json:"content"`
	Files   []string `json:"files"` // resource files of the skill (set for SKILL.md)
}

// Backend is what the gateway server needs from the daemon.
type Backend interface {
	// Tools lists the proxied tools; etag is the ETag of the list the caller holds ("" = none).
	Tools(ctx context.Context, etag string) (ToolList, error)
	Call(ctx context.Context, name string, args json.RawMessage) (*mcp.CallResult, error)
	Skills(ctx context.Context) ([]skills.Skill, error)
	Skill(ctx context.Context, name, file string) (SkillContent, error)
}

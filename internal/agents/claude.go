package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Claude is the Claude Code adapter (binary `claude`).
//
// Structured mode runs `claude -p --output-format stream-json --input-format stream-json
// --verbose --permission-prompt-tool stdio`: prompts go in as stream-json user messages,
// permission requests arrive as control_request/can_use_tool messages and are answered with
// control_response messages. Terminal mode runs the interactive TUI in a ConPTY with a
// --settings file whose hooks call back into the daemon (see Manager.Hook).
type Claude struct {
	// Bin overrides the executable (tests); "" = `claude` from PATH.
	Bin string
}

func (c Claude) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "claude"
}

func (Claude) Kind() string    { return "claude" }
func (Claude) Name() string    { return "Claude Code" }
func (Claude) Modes() []Mode   { return []Mode{ModeStructured, ModeTerminal} }
func (Claude) CanResume() bool { return true }

func (c Claude) Detect(ctx context.Context) Detection {
	d := detectBinary(ctx, "claude", "Claude Code", c.bin())
	d.Modes, d.Resume, d.Permissions, d.StatusFeed = c.Modes(), true, true, "stream-json"
	return d
}

func validPermissionMode(m string) error {
	switch m {
	case "", "default", "acceptEdits", "plan":
		return nil
	}
	return fmt.Errorf("unsupported permission mode %q", m)
}

func (c Claude) Start(ctx context.Context, h *Host, spec LaunchSpec) (Process, error) {
	if !localMachine(spec.Machine) {
		return nil, fmt.Errorf("local Claude adapter cannot launch on a remote machine")
	}
	if err := validPermissionMode(spec.PermissionMode); err != nil {
		return nil, err
	}
	if err := checkToken("model", spec.Model); err != nil {
		return nil, err
	}
	if err := validateReasoning("claude", spec.Reasoning); err != nil {
		return nil, err
	}
	if err := checkToken("resume id", spec.Resume); err != nil {
		return nil, err
	}
	path, err := lookBin(c.bin())
	if err != nil {
		return nil, err
	}
	var args []string
	if spec.Resume != "" {
		args = append(args, "--resume", spec.Resume)
	} else if spec.NewAgentSessionID != "" {
		args = append(args, "--session-id", spec.NewAgentSessionID)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if spec.Reasoning != "" {
		args = append(args, "--effort", spec.Reasoning)
	}
	if spec.PermissionMode != "" && spec.PermissionMode != "default" {
		args = append(args, "--permission-mode", spec.PermissionMode)
	}
	if spec.Mode == ModeTerminal {
		return c.startTerminal(h, spec, path, args)
	}
	args = append([]string{"-p", "--output-format", "stream-json", "--input-format", "stream-json", "--verbose",
		"--permission-prompt-tool", "stdio"}, args...)
	p, err := startPipe(spec.Dir, path, args, decodeClaude)
	if err != nil {
		return nil, err
	}
	p.encodePrompt = func(prompt string) []byte {
		b, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prompt}})
		return append(b, '\n')
	}
	p.encodeReply = encodeClaudeReply
	return p, nil
}

func encodeClaudeReply(id string, allow bool, reason string, input json.RawMessage) []byte {
	var resp map[string]any
	if allow {
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		resp = map[string]any{"behavior": "allow", "updatedInput": input}
	} else {
		if reason == "" {
			reason = "The user denied this action."
		}
		resp = map[string]any{"behavior": "deny", "message": reason}
	}
	b, _ := json.Marshal(map[string]any{"type": "control_response",
		"response": map[string]any{"subtype": "success", "request_id": id, "response": resp}})
	return append(b, '\n')
}

// hookEvents are the Claude Code hook events the daemon listens to in terminal mode.
var hookEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop", "SessionEnd"}

// HookSettings renders the --settings JSON that routes Claude Code hooks to
// `<hookExe> agent-hook <sessionID> <Event>`.
func HookSettings(hookExe, sessionID string) ([]byte, error) {
	exe := filepath.ToSlash(hookExe) // forward slashes survive bash, cmd and PowerShell
	hooks := map[string]any{}
	for _, ev := range hookEvents {
		hooks[ev] = []any{map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": fmt.Sprintf(`"%s" agent-hook %s %s`, exe, sessionID, ev), "timeout": 10,
		}}}}
	}
	return json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
}

func (c Claude) startTerminal(h *Host, spec LaunchSpec, path string, args []string) (Process, error) {
	if h.HookExe != "" {
		dir, err := h.sessionDir(spec.SessionID)
		if err != nil {
			return nil, err
		}
		b, err := HookSettings(h.HookExe, spec.SessionID)
		if err != nil {
			return nil, err
		}
		sp := filepath.Join(dir, "claude-settings.json")
		if err := os.WriteFile(sp, b, 0o600); err != nil {
			return nil, err
		}
		args = append(args, "--settings", filepath.ToSlash(sp))
	}
	pa, err := promptArg(h, spec, path)
	if err != nil {
		return nil, err
	}
	if pa != "" {
		args = append(args, "--", pa)
	}
	line, err := commandLine(path, args...)
	if err != nil {
		return nil, err
	}
	p, err := startPty(h, spec.SessionID, spec.Dir, line, "Claude Code")
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ---- stream-json decoding ----

type claudeMsg struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	Terminal  string `json:"terminal_reason"`
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype   string          `json:"subtype"`
		ToolName  string          `json:"tool_name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
	} `json:"request"`
	ParentToolUseID *string `json:"parent_tool_use_id"`
}

type claudeBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

func decodeClaude(p *pipeProc, line []byte) []Event {
	var m claudeMsg
	if err := json.Unmarshal(line, &m); err != nil {
		return []Event{{Kind: EvActivity, Text: truncate(string(line), 200)}}
	}
	switch m.Type {
	case "system":
		if m.Subtype == "init" {
			return []Event{{Kind: EvInit, Data: jraw(map[string]string{"agentSessionId": m.SessionID, "model": m.Model})}}
		}
	case "assistant":
		var blocks []claudeBlock
		_ = json.Unmarshal(m.Message.Content, &blocks)
		var out []Event
		for _, b := range blocks {
			switch b.Type {
			case "text":
				if t := strings.TrimSpace(b.Text); t != "" {
					out = append(out, Event{Kind: EvMessage, Text: t})
				}
			case "tool_use":
				out = append(out, Event{Kind: EvTool, Text: b.Name + ": " + summarizeTool(b.Name, b.Input), Data: jraw(map[string]any{"tool": b.Name})})
			}
		}
		return out
	case "user":
		var blocks []claudeBlock
		if json.Unmarshal(m.Message.Content, &blocks) == nil {
			var out []Event
			for _, b := range blocks {
				if b.Type == "tool_result" {
					out = append(out, Event{Kind: EvToolResult, Text: truncate(toolResultText(b.Content), 400), Data: jraw(map[string]bool{"error": b.IsError})})
				}
			}
			return out
		}
	case "control_request":
		if m.Request.Subtype == "can_use_tool" {
			p.pmu.Lock()
			var canceled []Event
			for id, pending := range p.pendingIn {
				if pending.requestID == m.RequestID {
					delete(p.pendingIn, id)
					canceled = append(canceled, Event{Kind: EvPermCancel, Data: jraw(map[string]string{"id": id})})
				}
			}
			if len(canceled) > 0 || m.RequestID == "" || len(m.Request.Input) > 64<<10 || !json.Valid(m.Request.Input) {
				p.pmu.Unlock()
				_ = p.write(encodeClaudeReply(m.RequestID, false, "Tool input is invalid, oversized, or reuses a pending request ID", nil))
				return canceled
			}
			// UI IDs identify immutable snapshots, never agent-controlled/reused IDs.
			id := uuid.NewString()
			p.pendingIn[id] = pendingPermission{requestID: m.RequestID, input: m.Request.Input}
			p.pmu.Unlock()
			return []Event{{Kind: EvPermission, Text: m.Request.ToolName, Data: jraw(PermissionRequest{
				ID: id, Tool: m.Request.ToolName, Summary: summarizeTool(m.Request.ToolName, m.Request.Input),
				Input: m.Request.Input, InputJSON: string(m.Request.Input), At: time.Now().UTC()})}}
		}
		// Anything else would block the CLI forever; refuse explicitly.
		b, _ := json.Marshal(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "error", "request_id": m.RequestID, "error": "unsupported control request: " + m.Request.Subtype}})
		_ = p.write(append(b, '\n'))
	case "control_cancel_request":
		p.pmu.Lock()
		var canceled []Event
		for id, pending := range p.pendingIn {
			if pending.requestID == m.RequestID {
				delete(p.pendingIn, id)
				canceled = append(canceled, Event{Kind: EvPermCancel, Data: jraw(map[string]string{"id": id})})
			}
		}
		p.pmu.Unlock()
		return canceled
	case "result":
		ok := !m.IsError && (m.Subtype == "" || m.Subtype == "success")
		res := strings.TrimSpace(m.Result)
		if !ok && res == "" {
			res = strings.TrimSpace(m.Subtype + " " + m.Terminal)
		}
		return []Event{{Kind: EvTurnDone, Text: res, Data: jraw(map[string]any{"ok": ok, "result": res, "subtype": m.Subtype})}}
	}
	return nil
}

func toolResultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}

// summarizeTool is a one-line description of a tool call for the UI.
func summarizeTool(name string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	pick := ""
	for _, k := range []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt"} {
		if v := str(k); v != "" {
			pick = v
			break
		}
	}
	if pick == "" {
		return name
	}
	return truncate(strings.Join(strings.Fields(pick), " "), 240)
}

// NewClaudeSessionID makes a fresh CLI session id.
func NewClaudeSessionID() string { return uuid.NewString() }

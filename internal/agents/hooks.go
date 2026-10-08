package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// hookPayload is the subset of the JSON Claude Code writes to a hook's stdin that we use.
type hookPayload struct {
	SessionID            string          `json:"session_id"`
	TranscriptPath       string          `json:"transcript_path"`
	ToolName             string          `json:"tool_name"`
	ToolInput            json.RawMessage `json:"tool_input"`
	Prompt               string          `json:"prompt"`
	Message              string          `json:"message"`
	NotificationType     string          `json:"notification_type"`
	LastAssistantMessage string          `json:"last_assistant_message"`
}

// Hook folds a Claude Code hook callback (terminal mode) into the session. Status comes from
// these structured events, never from reading terminal output. payload is the hook's stdin
// JSON and is treated as untrusted data: it is only ever stored and displayed.
func (m *Manager) Hook(id, event string, raw json.RawMessage) error {
	m.mu.Lock()
	ls := m.live[id]
	var kind string
	var mode Mode
	var expect string
	if ls != nil {
		kind, mode, expect = ls.s.Kind, ls.s.Mode, ls.expectPrompt
	}
	m.mu.Unlock()
	if ls == nil {
		return errors.New("no running session with that id")
	}
	if kind != "claude" || mode != ModeTerminal {
		return errors.New("hooks are only used for Claude Code terminal sessions")
	}
	var p hookPayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("bad hook payload: %w", err)
		}
	}
	switch event {
	case "SessionStart":
		if p.SessionID != "" {
			m.apply(id, Event{Kind: EvInit, Data: jraw(map[string]string{"agentSessionId": p.SessionID})})
		}
		if p.TranscriptPath != "" {
			m.apply(id, Event{Kind: EvTranscript, Text: p.TranscriptPath})
		}
	case "UserPromptSubmit":
		if p.Prompt != expect {
			m.record(id, Event{Kind: EvPrompt, Text: p.Prompt})
		}
		m.mu.Lock()
		if ls := m.live[id]; ls != nil {
			ls.expectPrompt = ""
		}
		m.mu.Unlock()
		m.apply(id, Event{Kind: EvWorking, Text: "Working on: " + truncate(firstLine(p.Prompt), 100)})
	case "PreToolUse":
		m.apply(id, Event{Kind: EvTool, Text: p.ToolName + ": " + summarizeTool(p.ToolName, p.ToolInput), Data: jraw(map[string]any{"tool": p.ToolName})})
	case "PostToolUse":
		m.apply(id, Event{Kind: EvWorking})
	case "Notification":
		switch p.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			m.apply(id, Event{Kind: EvNeedsInput, Text: strings.TrimSpace(p.Message)})
		default:
			// idle_prompt only repeats that the finished turn is waiting; auth_success etc. are noise.
			m.record(id, Event{Kind: EvActivity, Text: p.Message})
		}
	case "Stop":
		if t := strings.TrimSpace(p.LastAssistantMessage); t != "" {
			m.apply(id, Event{Kind: EvMessage, Text: t})
		}
		m.apply(id, Event{Kind: EvTurnDone, Text: p.LastAssistantMessage, Data: jraw(map[string]any{"ok": true, "result": strings.TrimSpace(p.LastAssistantMessage)})})
	case "SessionEnd":
		m.record(id, Event{Kind: EvSystem, Text: "Claude Code session ended"})
	default:
		return fmt.Errorf("unknown hook event %q", event)
	}
	return nil
}

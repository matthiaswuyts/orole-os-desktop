package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func claudeFlag(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("missing %s in %q", flag, args)
	return ""
}

func TestClaudeStructuredCentralConfig(t *testing.T) {
	for _, model := range []string{"", "sonnet", "opus", "custom-provider-model"} {
		for _, resume := range []string{"", "previous-session"} {
			t.Run(model+"/"+resume, func(t *testing.T) {
				h := &Host{DataDir: filepath.Join(t.TempDir(), "data with spaces")}
				gateway := &Gateway{Command: `C:\Program Files\Orole\daemon.exe`, Args: []string{"mcp-gateway", `quote" and & shell %characters%`}, Env: map[string]string{"OROLE_HOME": h.DataDir, "OROLE_GATEWAY_SESSION": "session"}}
				prompt := "## Skills\n--not-a-flag \"quoted\" & %value% 日本語\n" + strings.Repeat("skill description\n", 4000)
				spec := LaunchSpec{SessionID: "session", Mode: ModeStructured, Model: model, Resume: resume, Gateway: gateway, SkillsPrompt: prompt}
				base := []string{"--model", model, "--resume", resume}
				args, err := claudeStructuredArgs(h, spec, append([]string(nil), base...), true)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(args[:len(base)], base) {
					t.Fatalf("launch arguments changed: %q", args)
				}
				mcpPath := claudeFlag(t, args, "--mcp-config")
				promptPath := claudeFlag(t, args, "--append-system-prompt-file")
				for _, path := range []string{mcpPath, promptPath} {
					if !filepath.IsAbs(path) || filepath.Dir(path) != filepath.Join(h.DataDir, "agents", spec.SessionID) {
						t.Fatalf("config escaped session directory: %q", path)
					}
				}
				data, err := os.ReadFile(mcpPath)
				if err != nil {
					t.Fatal(err)
				}
				var config struct {
					Servers map[string]*Gateway `json:"mcpServers"`
				}
				if err := json.Unmarshal(data, &config); err != nil {
					t.Fatal(err)
				}
				if len(config.Servers) != 1 || !reflect.DeepEqual(config.Servers["orole"], gateway) {
					t.Fatalf("gateway was altered: %s", data)
				}
				data, err = os.ReadFile(promptPath)
				if err != nil || string(data) != prompt {
					t.Fatalf("skill index was altered: %v", err)
				}
				if claudeFlag(t, args, "--system-prompt-snapshot") != "off" {
					t.Fatal("resumes would use stale skill instructions")
				}
				if strings.Contains(strings.Join(args, " "), "skill description") {
					t.Fatal("large skill index leaked into command line")
				}
				// A resume replaces the per-session index rather than appending stale skills.
				spec.Resume, spec.SkillsPrompt = "previous-session", "replacement index"
				if _, err := claudeStructuredArgs(h, spec, nil, true); err != nil {
					t.Fatal(err)
				}
				data, err = os.ReadFile(promptPath)
				if err != nil || string(data) != spec.SkillsPrompt {
					t.Fatalf("resume did not refresh index: %v", err)
				}
				spec.SkillsPrompt = ""
				args, err = claudeStructuredArgs(h, spec, nil, true)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(strings.Join(args, " "), "--append-system-prompt") || claudeFlag(t, args, "--system-prompt-snapshot") != "off" {
					t.Fatal("removed skills would remain active on resume")
				}
				// Builds from before the flag reject it as an unknown option, so it is left out.
				args, err = claudeStructuredArgs(h, spec, nil, false)
				if err != nil || slices.Contains(args, "--system-prompt-snapshot") {
					t.Fatalf("older Claude Code would fail to start: %q %v", args, err)
				}
			})
		}
	}
}

func TestClaudeStructuredConfigOptionalAndWriteErrors(t *testing.T) {
	base := []string{"--resume", "session"}
	args, err := claudeStructuredArgs(nil, LaunchSpec{}, base, true)
	if err != nil || !reflect.DeepEqual(args, base) {
		t.Fatalf("absent central config changed launch: %q %v", args, err)
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := claudeStructuredArgs(&Host{DataDir: file}, LaunchSpec{SessionID: "session", SkillsPrompt: "skills"}, nil, true); err == nil {
		t.Fatal("ignored central configuration write error")
	}
	// Skill instructions do not depend on a selected model or a gateway being present.
	args, err = claudeStructuredArgs(&Host{DataDir: t.TempDir()}, LaunchSpec{SessionID: "skills-only", SkillsPrompt: "skills"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	claudeFlag(t, args, "--append-system-prompt-file")
	for _, arg := range args {
		if arg == "--mcp-config" {
			t.Fatal("invented unavailable gateway")
		}
	}
}

func TestClaudeReasoningAndToolEvents(t *testing.T) {
	d := claudeDecoder{tools: make(map[string]ToolData)}
	events := d.decode(nil, []byte(`{"type":"assistant","message":{"id":"msg-1","content":[{"type":"thinking","thinking":"Consider the options.","signature":"private-signature"},{"type":"redacted_thinking","data":"private-data"},{"type":"text","text":"Working."},{"type":"tool_use","id":"call-1","name":"mcp__orole__orole_get_skill","input":{"name":"a \"skill\""}},{"type":"tool_use","id":"call-2","name":"Bash","input":{"command":"echo hi"}}]}}`))
	if len(events) != 4 || events[0].Kind != EvReasoning || events[0].Text != "Consider the options." || events[1].Kind != EvMessage {
		t.Fatalf("completed content: %+v", events)
	}
	var reasoning struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(events[0].Data, &reasoning); err != nil || reasoning.ID != "msg-1:0" {
		t.Fatalf("reasoning identity: %s %v", events[0].Data, err)
	}
	var call ToolData
	if err := json.Unmarshal(events[2].Data, &call); err != nil {
		t.Fatal(err)
	}
	if events[2].Kind != EvTool || call.ID != "call-1" || call.Name != "orole_get_skill" || call.Server != "orole" || call.Status != "running" || string(call.Input) != `{"name":"a \"skill\""}` {
		t.Fatalf("MCP call: %+v", call)
	}
	results := d.decode(nil, []byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call-2","content":[{"type":"text","text":"denied"}],"is_error":true},{"type":"tool_result","tool_use_id":"call-1","content":"skill content"}]}}`))
	if len(results) != 2 {
		t.Fatalf("results: %+v", results)
	}
	for i, expected := range []ToolData{{ID: "call-2", Name: "Bash", Status: "error", Output: "denied"}, {ID: "call-1", Name: call.Name, Server: "orole", Status: "ok", Output: "skill content"}} {
		var result ToolData
		if err := json.Unmarshal(results[i].Data, &result); err != nil || !reflect.DeepEqual(result, expected) || results[i].Kind != EvToolResult {
			t.Fatalf("result %d: %+v %v", i, result, err)
		}
	}
	if len(d.tools) != 0 {
		t.Fatal("completed tool metadata retained")
	}
	if got := d.decode(nil, []byte(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"partial"}}}`)); len(got) != 0 {
		t.Fatalf("partial thinking duplicated a completed item: %+v", got)
	}
}

func TestClaudeToolResultWithoutCallAndOutputBound(t *testing.T) {
	d := claudeDecoder{tools: make(map[string]ToolData)}
	line := jraw(map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "from-resumed-history", "content": strings.Repeat("x", 10000)}}}})
	events := d.decode(nil, line)
	if len(events) != 1 {
		t.Fatalf("orphan result dropped: %+v", events)
	}
	var result ToolData
	if err := json.Unmarshal(events[0].Data, &result); err != nil || result.ID != "from-resumed-history" || result.Status != "ok" || len(result.Output) > (8<<10)+3 || len(events[0].Text) > 403 {
		t.Fatalf("unbounded or unlinked result: %+v %v", result, err)
	}
}

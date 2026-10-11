package agents

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Omp is the Oh My Pi adapter (binary `omp`).
//
// It runs `omp --mode rpc`, omp's newline-delimited JSON protocol over stdio (see omp's
// docs/rpc.md). Orole negotiates protocol v2 (lossless rpc_chunk framing), reads the
// session id with get_state and turns omp's session events into Orole's item-level events:
// completed thinking blocks become EvReasoning, completed assistant text EvMessage,
// tool_execution_start/end EvTool/EvToolResult, and one agent_end that yields EvTurnDone.
//
// Approvals remain separate from questions: confirm and Approve/Deny selects use
// Respond, while native input/select/editor dialogs use InputResponder and their
// exact extension_ui_response value/cancelled wire shape.
//
// The central gateway ("orole" MCP server) reaches omp without touching the user's omp
// config or the repository: a per-session extension package in the session directory carries
// a `.mcp.json`, and `-e <package>` makes omp discover it as an OMP extension package.
type Omp struct {
	// Bin overrides the executable (tests); "" = `omp` from PATH.
	Bin string
}

func (o Omp) bin() string {
	if o.Bin != "" {
		return o.Bin
	}
	return "omp"
}

func (Omp) Kind() string    { return "omp" }
func (Omp) Name() string    { return "Oh My Pi" }
func (Omp) Modes() []Mode   { return []Mode{ModeStructured} }
func (Omp) CanResume() bool { return true }

func (o Omp) Detect(ctx context.Context) Detection {
	d := detectBinary(ctx, "omp", "Oh My Pi", o.bin())
	d.Modes, d.Resume, d.Permissions, d.StatusFeed = o.Modes(), true, true, "stream-json"
	return d
}

// ompThinkingLevels are the `--thinking` / set_thinking_level values (omp --help). Which
// ones a model honours is model-specific; ompModels reports that per model.
var ompThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// ompReadOnlyTools is the tool set of a "plan" session. omp keeps its `write` tool (it also
// carries MCP calls); the always-ask approval mode keeps every call to it behind approval.
const ompReadOnlyTools = "read,glob,grep,web_search,todo"

// ompApprovalMode maps Orole's permission modes onto omp's --approval-mode and, for "plan", a
// read-only tool set. omp defaults to yolo (never ask), which would bypass Orole's approvals, so
// Manual is always-ask. omp has no AI reviewer: Auto is its `write` mode, which runs reads and
// file edits and still asks before commands (reported as approximated).
func ompApprovalMode(mode string) (approval string, tools string, err error) {
	switch mode {
	case "", PermManual:
		return "always-ask", "", nil
	case PermAuto:
		return "write", "", nil
	case PermYolo:
		return "yolo", "", nil
	case PermPlan:
		return "always-ask", ompReadOnlyTools, nil
	}
	return "", "", fmt.Errorf("unsupported permission mode %q", mode)
}

// PermissionModes: omp has no model-decides mode, so Auto is approximated.
func (Omp) PermissionModes(context.Context, Detection) []PermissionModeSupport {
	return []PermissionModeSupport{
		modeSupport(PermManual, false, "Oh My Pi asks before every tool call that is not a read."),
		modeSupport(PermAuto, true, "Oh My Pi has no mode where the AI decides; Auto approves file edits and still asks before running commands."),
		modeSupport(PermYolo, false, "Oh My Pi runs every tool call without asking."),
	}
}

// omp's RPC protocol has no approval-mode setter (--approval-mode is a launch flag), so ompProc
// does not implement permissionModeProcess: the manager resumes the conversation with the new mode.

func (o Omp) Start(ctx context.Context, h *Host, spec LaunchSpec) (Process, error) {
	if !localMachine(spec.Machine) {
		return nil, errors.New("local Oh My Pi adapter cannot launch on a remote machine")
	}
	if spec.Mode != "" && spec.Mode != ModeStructured {
		return nil, errors.New("Oh My Pi only runs in structured mode")
	}
	approval, tools, err := ompApprovalMode(spec.PermissionMode)
	if err != nil {
		return nil, err
	}
	if err := checkToken("model", spec.Model); err != nil {
		return nil, err
	}
	if err := validateReasoning("omp", spec.Reasoning); err != nil {
		return nil, err
	}
	if err := checkToken("resume id", spec.Resume); err != nil {
		return nil, err
	}
	if h == nil {
		return nil, errors.New("Oh My Pi needs a host session directory")
	}
	path, err := lookBin(o.bin())
	if err != nil {
		return nil, err
	}
	sd, err := h.sessionDir(spec.SessionID)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(sd, "omp")
	sessions, images := filepath.Join(root, "sessions"), filepath.Join(root, "images")
	for _, d := range []string{sessions, images} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	args := []string{"--mode", "rpc", "--cwd", spec.Dir, "--session-dir", sessions, "--approval-mode", approval}
	if spec.Resume != "" {
		args = append(args, "--resume", spec.Resume)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if spec.Reasoning != "" {
		args = append(args, "--thinking", spec.Reasoning)
	}
	if tools != "" {
		args = append(args, "--tools", tools)
	}
	if spec.Gateway != nil {
		ext, err := writeOmpGatewayPackage(filepath.Join(root, "ext"), spec.Gateway)
		if err != nil {
			return nil, err
		}
		args = append(args, "-e", ext)
	}
	promptFile := filepath.Join(root, "skills-prompt.md")
	if sp := strings.TrimSpace(spec.SkillsPrompt); sp != "" {
		if err := os.WriteFile(promptFile, []byte(sp+"\n"), 0o600); err != nil {
			return nil, err
		}
		args = append(args, "--append-system-prompt", promptFile)
	} else {
		_ = os.Remove(promptFile)
	}

	st := &ompState{images: images, tools: map[string]ompToolMeta{}, failed: map[string]bool{}}
	pp, err := startPipe(spec.Dir, path, args, st.decode)
	if err != nil {
		return nil, err
	}
	pp.encodeReply = encodeOmpReply
	p := &ompProc{pipeProc: pp, state: st}
	// Commands queue in the stdin pipe until omp has written its ready frame. Protocol v2
	// first so later oversized frames arrive lossless, then the delta projection of
	// message_update (the full snapshots resend the whole message on every token), then the
	// state that names the session.
	for _, c := range []map[string]any{
		{"id": "orole-neg", "type": "negotiate_protocol", "protocolVersion": 2},
		{"id": "orole-filter", "type": "set_event_filter", "events": nil, "messageUpdates": "delta"},
		{"id": "orole-subagents", "type": "set_subagent_subscription", "level": "progress"},
		{"id": "orole-state", "type": "get_state"},
	} {
		if err := p.command(c); err != nil {
			_ = p.Stop()
			return nil, err
		}
	}
	return p, nil
}

// ompGatewayEnvName limits gateway environment variable names to plain identifiers.
var ompGatewayEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// writeOmpGatewayPackage writes the per-session omp extension package that makes omp start
// the gateway: package.json + a no-op entry point (omp resolves a package directory through
// them) and the `.mcp.json` omp reads from explicit extension packages.
func writeOmpGatewayPackage(dir string, gw *Gateway) (string, error) {
	if strings.TrimSpace(gw.Command) == "" {
		return "", errors.New("gateway has no command")
	}
	// omp runs env/header values that start with "!" as shell commands and expands ${VAR}
	// placeholders in every string; the gateway needs neither, so refuse them outright.
	check := func(what, v string) error {
		if strings.HasPrefix(v, "!") || strings.Contains(v, "${") {
			return fmt.Errorf("gateway %s %q would be interpreted by omp's MCP loader", what, truncate(v, 60))
		}
		return nil
	}
	if err := check("command", gw.Command); err != nil {
		return "", err
	}
	for _, a := range gw.Args {
		if err := check("argument", a); err != nil {
			return "", err
		}
	}
	for k, v := range gw.Env {
		if !ompGatewayEnvName.MatchString(k) {
			return "", fmt.Errorf("gateway environment name %q is invalid", k)
		}
		if err := check("environment value for "+k, v); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	server := map[string]any{"type": "stdio", "command": gw.Command, "args": nonNilArgs(gw.Args)}
	if len(gw.Env) > 0 {
		server["env"] = gw.Env
	}
	files := map[string]any{
		"package.json": map[string]any{"name": "orole-session-gateway", "version": "0.0.0", "private": true,
			"omp": map[string]any{"extensions": []string{"./index.js"}}},
		".mcp.json": map[string]any{"mcpServers": map[string]any{"orole": server}},
	}
	for name, v := range files {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0o600); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("export default function () {}\n"), 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

func nonNilArgs(a []string) []string {
	if a == nil {
		return []string{}
	}
	return a
}

// ---- process ----

// ompProc is a pipeProc whose prompts and shutdown follow omp's RPC protocol.
type ompProc struct {
	*pipeProc
	seq   atomic.Uint64
	state *ompState
}

func (p *ompProc) command(c map[string]any) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return p.write(append(b, '\n'))
}

// Send delivers a prompt. streamingBehavior "steer" makes omp queue it as steering when a
// turn is running (delivered at the next tool boundary) and start a turn when it is idle, so
// no client-side busy tracking can race the agent.
func (p *ompProc) Send(prompt string) error {
	return p.command(map[string]any{"id": fmt.Sprintf("orole-p-%d", p.seq.Add(1)), "type": "prompt",
		"message": prompt, "streamingBehavior": "steer"})
}

func (p *ompProc) AnswerInput(id string, answers []QuestionAnswer, cancel bool) error {
	p.pmu.Lock()
	defer p.pmu.Unlock()
	pending, ok := p.state.inputs[id]
	if !ok {
		return fmt.Errorf("no pending question request %q", id)
	}
	if err := adapterValidateAnswers(pending.request, answers, cancel); err != nil {
		return err
	}
	delete(p.state.inputs, id)
	response := map[string]any{"type": "extension_ui_response", "id": pending.wireID}
	if cancel {
		response["cancelled"] = true
	} else {
		response["value"] = answers[0].Values[0]
	}
	return p.command(response)
}

// Stop aborts the running turn, then ends omp the way its protocol defines (close stdin,
// which drains and exits 0), falling back to the job kill after the grace period.
func (p *ompProc) Stop() error {
	_ = p.command(map[string]any{"type": "abort"})
	return p.pipeProc.Stop()
}

// encodeOmpReply answers a pending dialog. input is the {kind, approve, deny} snapshot
// stored when the permission was raised.
func encodeOmpReply(id string, allow bool, _ string, input json.RawMessage) []byte {
	var in struct {
		Kind    string `json:"kind"`
		Approve string `json:"approve"`
		Deny    string `json:"deny"`
	}
	_ = json.Unmarshal(input, &in)
	resp := map[string]any{"type": "extension_ui_response", "id": id}
	if in.Kind == "select" {
		if allow {
			resp["value"] = in.Approve
		} else {
			resp["value"] = in.Deny
		}
	} else {
		resp["confirmed"] = allow
	}
	b, _ := json.Marshal(resp)
	return append(b, '\n')
}

// ---- decoding ----

const (
	ompMaxFrame      = 64 << 20 // rpc_chunk reassembly limit (ready.maxReassembledFrameBytes)
	ompMaxToolOutput = 8 << 10
	ompMaxToolInput  = 16 << 10
	ompMaxImage      = 32 << 20
	ompMaxPending    = 64
)

type ompToolMeta struct {
	name, server string
	args         json.RawMessage
	intent       string
}

// ompState is the decoder state of one omp process. It is only touched from that process's
// stdout reader goroutine.
type ompState struct {
	images          string // directory generated images are written to
	maxFrame        int
	asm             *ompChunks
	tools           map[string]ompToolMeta // toolCallId -> the call, kept until its result arrives
	failed          map[string]bool        // prompt ids whose failure response was already reported
	lastText        string                 // last assistant text of the current run
	stop            string                 // stopReason of the last assistant message of the run
	errMsg          string
	imageSeq        int
	stateRequestSeq int
	initialized     bool
	inputs          map[string]ompInput // guarded by pipeProc.pmu, shared with AnswerInput
}

// ompChunks reassembles one rpc_chunk sequence.
type ompChunks struct {
	id          string
	count, next int
	byteLength  int
	buf         []byte
}

type ompMessage struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	StopReason   string          `json:"stopReason"`
	ErrorMessage string          `json:"errorMessage"`
	Usage        *struct {
		Input   *int64 `json:"input"`
		Output  *int64 `json:"output"`
		Cached  *int64 `json:"cacheRead"`
		Context *int64 `json:"contextTokens"`
	} `json:"usage"`
	StopDetails *struct {
		Type        string `json:"type"`
		Category    string `json:"category"`
		Explanation string `json:"explanation"`
	} `json:"stopDetails"`
}

type ompBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Redacted  bool            `json:"redacted"`
	Data      string          `json:"data"`
	MimeType  string          `json:"mimeType"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ompToolResult struct {
	Content []ompBlock      `json:"content"`
	Details json.RawMessage `json:"details"`
}

// decode turns one stdout line into events.
func (s *ompState) decode(p *pipeProc, line []byte) []Event {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &head) != nil {
		return []Event{{Kind: EvActivity, Text: truncate(string(line), 200)}}
	}
	// message_update deltas (the common case) carry nothing the item-level events lack.
	if head.Type == "message_update" && isOmpDelta(line) {
		return nil
	}
	var out []Event
	if head.Type == "rpc_chunk" {
		frame, evs := s.chunk(line)
		if frame == nil {
			return evs
		}
		out = evs
		line = frame
		if json.Unmarshal(line, &head) != nil {
			return append(out, Event{Kind: EvError, Text: "omp sent a reassembled frame that is not a JSON object"})
		}
	} else if s.asm != nil {
		s.asm = nil
		out = append(out, Event{Kind: EvError, Text: "omp interrupted an rpc_chunk sequence; the frame was dropped"})
	}
	return append(out, s.frame(p, head.Type, line)...)
}

// isOmpDelta reports whether a message_update frame is a *_delta event, judged from the
// frame prefix so a long streamed message is never fully parsed per token.
func isOmpDelta(line []byte) bool {
	n := min(len(line), 256)
	return bytes.Contains(line[:n], []byte(`_delta"`))
}

// chunk consumes one rpc_chunk frame. It returns the whole frame once the sequence is
// complete; invalid or interrupted sequences are dropped with an error event.
func (s *ompState) chunk(line []byte) ([]byte, []Event) {
	var c struct {
		ChunkID    string `json:"chunkId"`
		Index      int    `json:"index"`
		Count      int    `json:"count"`
		ByteLength int    `json:"byteLength"`
		Data       string `json:"data"`
	}
	fail := func(why string) ([]byte, []Event) {
		s.asm = nil
		return nil, []Event{{Kind: EvError, Text: "malformed rpc_chunk sequence from omp: " + why}}
	}
	if json.Unmarshal(line, &c) != nil || c.ChunkID == "" || c.Count < 1 || c.Index < 0 || c.Index >= c.Count || c.ByteLength < 0 {
		return fail("invalid chunk header")
	}
	limit := ompMaxFrame
	if s.maxFrame > 0 && s.maxFrame < limit {
		limit = s.maxFrame
	}
	if c.ByteLength > limit {
		return fail("frame exceeds the reassembly limit")
	}
	var evs []Event
	if c.Index == 0 {
		if s.asm != nil {
			evs = append(evs, Event{Kind: EvError, Text: "omp started an rpc_chunk sequence before finishing the previous one"})
		}
		s.asm = &ompChunks{id: c.ChunkID, count: c.Count, byteLength: c.ByteLength, buf: make([]byte, 0, min(c.ByteLength, 1<<20))}
	}
	a := s.asm
	if a == nil || a.id != c.ChunkID || a.count != c.Count || a.byteLength != c.ByteLength || a.next != c.Index {
		_, e := fail("chunk out of sequence")
		return nil, append(evs, e...)
	}
	part, err := base64.StdEncoding.DecodeString(c.Data)
	if err != nil {
		_, e := fail("chunk data is not base64")
		return nil, append(evs, e...)
	}
	if len(a.buf)+len(part) > a.byteLength {
		_, e := fail("chunks exceed the declared byteLength")
		return nil, append(evs, e...)
	}
	a.buf = append(a.buf, part...)
	a.next++
	if a.next < a.count {
		return nil, evs
	}
	s.asm = nil
	if len(a.buf) != a.byteLength || !utf8.Valid(a.buf) {
		_, e := fail("reassembled frame has the wrong length or is not UTF-8")
		return nil, append(evs, e...)
	}
	return a.buf, evs
}

func (s *ompState) frame(p *pipeProc, typ string, line []byte) []Event {
	switch typ {
	case "ready":
		var r struct {
			Max int `json:"maxReassembledFrameBytes"`
		}
		_ = json.Unmarshal(line, &r)
		s.maxFrame = r.Max
	case "response":
		if p != nil && p.acceptControlResponse(line, false) {
			return nil
		}
		return s.response(line)
	case "prompt_result":
		return s.promptResult(line)
	case "agent_start":
		s.lastText, s.stop, s.errMsg = "", "", ""
		return []Event{{Kind: EvWorking, Text: "Working"}}
	case "agent_end":
		return s.agentEnd(p, line)
	case "message_update":
		return s.messageUpdate(line)
	case "message_end":
		s.requestState(p)
		return s.messageEnd(line)
	case "tool_execution_start":
		return s.toolStart(line)
	case "tool_execution_end":
		return s.toolEnd(line)
	case "extension_ui_request":
		return s.dialog(p, line)
	case "subagent_lifecycle":
		var frame struct {
			Payload struct {
				ID               string `json:"id"`
				Agent            string `json:"agent"`
				Status           string `json:"status"`
				Description      string `json:"description"`
				ParentToolCallID string `json:"parentToolCallId"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &frame) == nil && frame.Payload.ID != "" && frame.Payload.Status != "" {
			a := frame.Payload
			return []Event{{Kind: EvSubagent, Data: jraw(SubagentData{ID: a.ID, Name: a.Agent, Status: a.Status, Task: a.Description, ToolCallID: a.ParentToolCallID})}}
		}
	case "auto_compaction_end":
		s.requestState(p)
	case "auto_compaction_start":
		return []Event{{Kind: EvActivity, Text: "Compacting context"}}
	case "auto_retry_start":
		return []Event{{Kind: EvActivity, Text: "Retrying after a model error"}}
	case "notice":
		var n struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if json.Unmarshal(line, &n) == nil && n.Level == "error" && n.Message != "" {
			return []Event{{Kind: EvError, Text: truncate(n.Message, 2000)}}
		}
	case "extension_error", "rpc_frame_error":
		var e struct {
			Error string `json:"error"`
			Event string `json:"event"`
		}
		_ = json.Unmarshal(line, &e)
		return []Event{{Kind: EvSystem, Text: truncate(strings.TrimSpace("omp "+typ+" "+e.Event+": "+e.Error), 500)}}
	}
	return nil
}

func ompTurnDone(ok bool, result, subtype string) Event {
	return Event{Kind: EvTurnDone, Text: result, Data: jraw(map[string]any{"ok": ok, "result": result, "subtype": subtype})}
}

func (s *ompState) response(line []byte) []Event {
	var r struct {
		ID      string `json:"id"`
		Command string `json:"command"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			ContextUsage *struct {
				Tokens        *int64 `json:"tokens"`
				ContextWindow *int64 `json:"contextWindow"`
			} `json:"contextUsage"`
			AgentInvoked *bool  `json:"agentInvoked"`
			SessionID    string `json:"sessionId"`
			SessionFile  string `json:"sessionFile"`
			Model        struct {
				Provider string `json:"provider"`
				ID       string `json:"id"`
			} `json:"model"`
		} `json:"data"`
	}
	if json.Unmarshal(line, &r) != nil {
		return nil
	}
	switch r.Command {
	case "get_state":
		if !r.Success {
			return []Event{{Kind: EvError, Text: "omp get_state failed: " + truncate(r.Error, 500)}}
		}
		model := r.Data.Model.ID
		if r.Data.Model.Provider != "" && model != "" {
			model = r.Data.Model.Provider + "/" + model
		}
		var out []Event
		if !s.initialized {
			s.initialized = true
			out = append(out, Event{Kind: EvInit, Data: jraw(map[string]string{"agentSessionId": r.Data.SessionID, "model": model})})
			if r.Data.SessionFile != "" {
				out = append(out, Event{Kind: EvTranscript, Text: r.Data.SessionFile})
			}
		}
		if u := r.Data.ContextUsage; u != nil {
			out = append(out, usageEvent(ContextUsage{Source: "omp:get_state:contextUsage",
				ContextTokens: u.Tokens, ContextWindow: u.ContextWindow})...)
		}
		return out
	case "prompt":
		switch {
		case !r.Success:
			s.failed[r.ID] = true
			return []Event{{Kind: EvError, Text: "omp rejected the prompt: " + truncate(r.Error, 2000)},
				ompTurnDone(false, truncate(r.Error, 2000), "rejected")}
		case r.Data.AgentInvoked != nil && !*r.Data.AgentInvoked:
			// A builtin slash command finished without an agent turn; no prompt_result follows.
			return []Event{ompTurnDone(true, "", "local")}
		}
	}
	return nil
}

func (s *ompState) promptResult(line []byte) []Event {
	var r struct {
		ID           string `json:"id"`
		AgentInvoked bool   `json:"agentInvoked"`
		Status       string `json:"status"`
		Error        struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(line, &r) != nil {
		return nil
	}
	if s.failed[r.ID] {
		delete(s.failed, r.ID)
		return nil
	}
	// An invoked prompt is closed by its agent_end (provider failures included), so only
	// prompts that never reached the agent (finished locally, failed first) end here.
	if r.AgentInvoked {
		return nil
	}
	switch r.Status {
	case "error":
		msg := truncate(r.Error.Message, 2000)
		return []Event{{Kind: EvError, Text: "omp prompt failed: " + msg}, ompTurnDone(false, msg, "error")}
	case "aborted":
		return []Event{ompTurnDone(false, "Aborted", "aborted")}
	}
	if !r.AgentInvoked {
		return []Event{ompTurnDone(true, "", "local")}
	}
	return nil
}

func (s *ompState) agentEnd(p *pipeProc, line []byte) []Event {
	var e struct {
		Yielded    *bool `json:"yielded"`
		IsTerminal *bool `json:"isTerminal"`
	}
	_ = json.Unmarshal(line, &e)
	yielded := e.IsTerminal == nil || *e.IsTerminal
	if e.Yielded != nil {
		yielded = *e.Yielded
	}
	if !yielded { // omp continues (retry, compaction, stop-time reminder)
		return nil
	}
	out := s.dropPending(p)
	s.requestState(p)
	ok, result, sub := s.stop != "error" && s.stop != "aborted", s.lastText, s.stop
	switch s.stop {
	case "error":
		result = s.errMsg
		if result == "" {
			result = "The model request failed."
		}
	case "aborted":
		result = "Aborted"
	}
	return append(out, ompTurnDone(ok, truncate(strings.TrimSpace(result), 32<<10), sub))
}

// dropPending withdraws every open dialog: a finished turn cannot still be waiting on one.
func (s *ompState) dropPending(p *pipeProc) []Event {
	p.pmu.Lock()
	defer p.pmu.Unlock()
	var out []Event
	for id := range p.pendingIn {
		delete(p.pendingIn, id)
		out = append(out, Event{Kind: EvPermCancel, Data: jraw(map[string]string{"id": id})})
	}
	for id := range s.inputs {
		delete(s.inputs, id)
		out = append(out, Event{Kind: EvQuestionCancel, Data: jraw(map[string]string{"id": id})})
	}
	return out
}

func (s *ompState) messageUpdate(line []byte) []Event {
	var m struct {
		AME struct {
			Type string `json:"type"`
		} `json:"assistantMessageEvent"`
	}
	if json.Unmarshal(line, &m) != nil {
		return nil
	}
	switch m.AME.Type {
	case "thinking_start":
		return []Event{{Kind: EvActivity, Text: "Thinking"}}
	case "text_start":
		return []Event{{Kind: EvActivity, Text: "Writing a response"}}
	case "toolcall_start":
		return []Event{{Kind: EvActivity, Text: "Preparing a tool call"}}
	}
	return nil
}

// requestState uses a distinct request ID so concurrent state refreshes are
// correlated by OMP and cannot be mistaken for unsolicited protocol frames.
func (s *ompState) requestState(p *pipeProc) {
	s.stateRequestSeq++
	request, err := json.Marshal(map[string]any{"id": fmt.Sprintf("orole-state-%d", s.stateRequestSeq), "type": "get_state"})
	if err == nil {
		_ = p.write(append(request, '\n'))
	}
}
func (s *ompState) messageEnd(line []byte) []Event {
	var m struct {
		MessageID string     `json:"messageId"`
		Message   ompMessage `json:"message"`
	}
	if json.Unmarshal(line, &m) != nil || m.Message.Role != "assistant" {
		return nil
	}
	s.stop, s.errMsg = m.Message.StopReason, strings.TrimSpace(m.Message.ErrorMessage)
	var blocks []ompBlock
	if json.Unmarshal(m.Message.Content, &blocks) != nil {
		return nil
	}
	var out []Event
	if u := m.Message.Usage; u != nil {
		out = append(out, usageEvent(ContextUsage{Source: "omp:message_end:usage", InputTokens: u.Input,
			OutputTokens: u.Output, CachedTokens: u.Cached, ContextTokens: u.Context})...)
	}
	if detail := m.Message.StopDetails; detail != nil && (detail.Type == "refusal" || detail.Type == "sensitive") {
		out = append(out, Event{Kind: EvGuardrail, Data: jraw(GuardrailData{ID: m.MessageID, Kind: detail.Type,
			Message: detail.Explanation, Source: "omp:message.stopDetails"})})
	}
	for i, b := range blocks {
		switch b.Type {
		case "thinking":
			if t := strings.TrimSpace(b.Thinking); t != "" && !b.Redacted {
				out = append(out, Event{Kind: EvReasoning, Text: t, Data: jraw(map[string]string{"id": fmt.Sprintf("%s-%d", m.MessageID, i)})})
			}
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				s.lastText = t
				out = append(out, Event{Kind: EvMessage, Text: t})
			}
		case "image":
			out = append(out, s.image(b, "")...)
		}
	}
	return out
}

// ---- tools ----

type ompXdevInner struct {
	ServerName  string `json:"serverName"`
	MCPToolName string `json:"mcpToolName"`
}

// splitOmpMCPName splits omp's registered MCP tool name mcp__<server>_<tool>. Server names
// may contain underscores, so this is only a best effort for the live event; the result
// carries the exact names.
func splitOmpMCPName(n string) (server, tool string, ok bool) {
	rest, found := strings.CutPrefix(n, "mcp__")
	if !found {
		return "", "", false
	}
	server, tool, found = strings.Cut(rest, "_")
	if !found || server == "" || tool == "" {
		return "", rest, true
	}
	return server, tool, true
}

func (s *ompState) toolStart(line []byte) []Event {
	var t struct {
		ID     string          `json:"toolCallId"`
		Name   string          `json:"toolName"`
		Args   json.RawMessage `json:"args"`
		Intent string          `json:"intent"`
	}
	if json.Unmarshal(line, &t) != nil || t.ID == "" {
		return nil
	}
	meta := ompToolMeta{name: t.Name, args: t.Args, intent: t.Intent}
	input := t.Args
	// omp reaches MCP tools by writing their JSON arguments to the xd://mcp__<server>_<tool>
	// device (or, in direct mode, by the tool name itself).
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(t.Args, &a)
	mcpName := ""
	switch {
	case t.Name == "write" && strings.HasPrefix(a.Path, "xd://mcp__"):
		mcpName = strings.TrimPrefix(a.Path, "xd://")
		if json.Valid([]byte(a.Content)) {
			input = json.RawMessage(a.Content)
		} else {
			input = jraw(a.Content)
		}
	case strings.HasPrefix(t.Name, "mcp__"):
		mcpName = t.Name
	}
	if srv, tool, ok := splitOmpMCPName(mcpName); ok {
		meta.name, meta.server = tool, srv
	}
	s.tools[t.ID] = meta
	if len(input) > ompMaxToolInput {
		input = jraw(truncate(string(input), ompMaxToolInput))
	}
	label := meta.name
	if meta.server != "" {
		label = meta.server + "/" + meta.name
	}
	summary := summarizeTool(meta.name, input)
	if summary == meta.name && t.Intent != "" {
		summary = truncate(t.Intent, 240)
	}
	return []Event{{Kind: EvTool, Text: label + ": " + summary,
		Data: jraw(ToolData{ID: t.ID, Name: meta.name, Server: meta.server, Status: "running", Input: input})}}
}

func (s *ompState) toolEnd(line []byte) []Event {
	var t struct {
		ID      string        `json:"toolCallId"`
		Name    string        `json:"toolName"`
		Result  ompToolResult `json:"result"`
		IsError bool          `json:"isError"`
	}
	if json.Unmarshal(line, &t) != nil || t.ID == "" {
		return nil
	}
	meta, known := s.tools[t.ID]
	delete(s.tools, t.ID)
	if !known {
		meta = ompToolMeta{name: t.Name}
	}
	var details struct {
		Xdev struct {
			Inner ompXdevInner `json:"inner"`
		} `json:"xdev"`
		Images []ompBlock `json:"images"`
	}
	_ = json.Unmarshal(t.Result.Details, &details)
	if in := details.Xdev.Inner; in.MCPToolName != "" {
		meta.name, meta.server = in.MCPToolName, in.ServerName
	}
	var texts []string
	var images []ompBlock
	for _, b := range t.Result.Content {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "image":
			images = append(images, b)
		}
	}
	// generate_image reports its images in details; its content is text only.
	if len(details.Images) > 0 {
		images = details.Images
	}
	output := strings.TrimSpace(strings.Join(texts, "\n"))
	status := "ok"
	if t.IsError {
		status = "error"
	}
	out := []Event{{Kind: EvToolResult, Text: truncate(output, 400),
		Data: jraw(ToolData{ID: t.ID, Name: meta.name, Server: meta.server, Status: status, Output: truncate(output, ompMaxToolOutput)})}}
	prompt := meta.intent
	var in struct {
		Subject string `json:"subject"`
	}
	if json.Unmarshal(meta.args, &in) == nil && in.Subject != "" {
		prompt = in.Subject
	}
	for _, img := range images {
		out = append(out, s.image(img, prompt)...)
	}
	return out
}

var ompMimeExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// image saves one base64 image block under the session's scratch directory and reports it.
func (s *ompState) image(b ompBlock, prompt string) []Event {
	ext, ok := ompMimeExt[strings.ToLower(b.MimeType)]
	if !ok || b.Data == "" {
		return nil
	}
	if base64.StdEncoding.DecodedLen(len(b.Data)) > ompMaxImage {
		return []Event{{Kind: EvSystem, Text: "omp produced an image over 32 MiB; it was not saved"}}
	}
	raw, err := base64.StdEncoding.DecodeString(b.Data)
	if err != nil {
		return []Event{{Kind: EvSystem, Text: "omp produced an image that is not valid base64"}}
	}
	s.imageSeq++
	id := fmt.Sprintf("img-%d-%s", s.imageSeq, uuid.NewString()[:8])
	path := filepath.Join(s.images, id+ext)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return []Event{{Kind: EvError, Text: "saving omp image: " + err.Error()}}
	}
	return []Event{{Kind: EvImage, Text: prompt, Data: jraw(ImageData{ID: id, Path: path, Mime: strings.ToLower(b.MimeType), Prompt: prompt})}}
}

// ---- dialogs ----

type ompUIRequest struct {
	ID            string   `json:"id"`
	Method        string   `json:"method"`
	Title         string   `json:"title"`
	Message       string   `json:"message"`
	Options       []string `json:"options"`
	TargetID      string   `json:"targetId"`
	Prefill       string   `json:"prefill"`
	Timeout       *int64   `json:"timeout"`
	OptionDetails []struct {
		Description string `json:"description"`
	} `json:"optionDetails"`
}

type ompInput struct {
	wireID  string
	request QuestionRequest
}

func (s *ompState) questionDialog(p *pipeProc, r ompUIRequest) []Event {
	now := time.Now().UTC()
	q := Question{ID: "value", Header: r.Title, Prompt: r.Title,
		AllowText: r.Method != "select", Options: []QuestionOption{}}
	if r.Prefill != "" {
		q.Prompt += "\n\n" + r.Prefill
	}
	for i, label := range r.Options {
		option := QuestionOption{ID: label, Label: label}
		if i < len(r.OptionDetails) {
			option.Description = r.OptionDetails[i].Description
		}
		q.Options = append(q.Options, option)
	}
	req := QuestionRequest{ID: uuid.NewString(), Questions: []Question{q}, At: now}
	if r.Timeout != nil {
		if *r.Timeout < 0 || *r.Timeout > 2147483647 {
			return s.rejectDialog(p, r)
		}
		expires := now.Add(time.Duration(*r.Timeout) * time.Millisecond)
		req.ExpiresAt = &expires
	}
	data := jraw(req)
	if len(data) > 64<<10 || !adapterValidQuestions(req.Questions) {
		return s.rejectDialog(p, r)
	}
	p.pmu.Lock()
	if s.inputs == nil {
		s.inputs = make(map[string]ompInput)
	}
	s.inputs[req.ID] = ompInput{wireID: r.ID, request: req}
	p.pmu.Unlock()
	return []Event{{Kind: EvQuestion, Data: data}}
}

// ompApprovalLabels finds the Approve/Deny pair of omp's tool approval select.
func ompApprovalLabels(options []string) (approve, deny string, ok bool) {
	if len(options) != 2 {
		return "", "", false
	}
	for _, o := range options {
		switch strings.ToLower(strings.TrimSpace(o)) {
		case "approve", "allow", "yes":
			if approve != "" {
				return "", "", false
			}
			approve = o
		case "deny", "reject", "no":
			if deny != "" {
				return "", "", false
			}
			deny = o
		}
	}
	return approve, deny, approve != "" && deny != ""
}

func (s *ompState) dialog(p *pipeProc, line []byte) []Event {
	var r ompUIRequest
	if json.Unmarshal(line, &r) != nil {
		return nil
	}
	if r.Method == "input" || r.Method == "editor" || r.Method == "select" || r.Method == "confirm" {
		p.pmu.Lock()
		var canceled []Event
		for id, pending := range p.pendingIn {
			if pending.requestID == r.ID {
				delete(p.pendingIn, id)
				canceled = append(canceled, Event{Kind: EvPermCancel, Data: jraw(map[string]string{"id": id})})
			}
		}
		for id, pending := range s.inputs {
			if pending.wireID == r.ID {
				delete(s.inputs, id)
				canceled = append(canceled, Event{Kind: EvQuestionCancel, Data: jraw(map[string]string{"id": id})})
			}
		}
		reject := len(canceled) > 0 || r.ID == "" || len(line) > 64<<10 || len(p.pendingIn)+len(s.inputs) >= ompMaxPending
		p.pmu.Unlock()
		if reject {
			return append(canceled, s.rejectDialog(p, r)...)
		}
	}
	switch r.Method {
	case "notify", "setStatus", "setWidget", "setTitle", "set_editor_text", "open_url":
		return nil // presentation only; no reply expected
	case "cancel":
		p.pmu.Lock()
		defer p.pmu.Unlock()
		var out []Event
		for id, pending := range p.pendingIn {
			if pending.requestID == r.TargetID {
				delete(p.pendingIn, id)
				out = append(out, Event{Kind: EvPermCancel, Data: jraw(map[string]string{"id": id})})
			}
		}
		for id, pending := range s.inputs {
			if pending.wireID == r.TargetID {
				delete(s.inputs, id)
				out = append(out, Event{Kind: EvQuestionCancel, Data: jraw(map[string]string{"id": id})})
			}
		}
		return out
	case "input", "editor":
		return s.questionDialog(p, r)
	case "confirm", "select":
		if r.ID == "" {
			return nil
		}
		snap := map[string]string{"kind": "confirm"}
		if r.Method == "select" {
			approve, deny, ok := ompApprovalLabels(r.Options)
			if !ok {
				return s.questionDialog(p, r)
			}
			snap = map[string]string{"kind": "select", "approve": approve, "deny": deny}
		}
		p.pmu.Lock()
		dup := len(p.pendingIn) >= ompMaxPending
		for _, pending := range p.pendingIn {
			dup = dup || pending.requestID == r.ID
		}
		if dup {
			p.pmu.Unlock()
			return s.rejectDialog(p, r)
		}
		id := uuid.NewString()
		p.pendingIn[id] = pendingPermission{requestID: r.ID, input: jraw(snap)}
		p.pmu.Unlock()

		text := strings.TrimSpace(strings.Join([]string{r.Title, r.Message}, "\n"))
		tool, detail := "omp", text
		if first, rest, _ := strings.Cut(text, "\n"); strings.HasPrefix(first, "Allow tool: ") {
			tool, detail = strings.TrimSpace(strings.TrimPrefix(first, "Allow tool: ")), strings.TrimSpace(rest)
		}
		if detail == "" {
			detail = text
		}
		input := jraw(map[string]any{"method": r.Method, "title": r.Title, "message": r.Message, "options": r.Options})
		return []Event{{Kind: EvPermission, Text: tool, Data: jraw(PermissionRequest{
			ID: id, Tool: tool, Summary: truncate(strings.Join(strings.Fields(detail), " "), 240),
			Input: input, InputJSON: string(input), At: time.Now().UTC()})}}
	}
	return s.rejectDialog(p, r)
}

// rejectDialog cancels a dialog Orole cannot answer. omp resolves a cancelled dialog to its
// default (confirm = false, select/input = no value), never to an approval.
func (s *ompState) rejectDialog(p *pipeProc, r ompUIRequest) []Event {
	if r.ID == "" {
		return nil
	}
	b, _ := json.Marshal(map[string]any{"type": "extension_ui_response", "id": r.ID, "cancelled": true})
	_ = p.write(append(b, '\n'))
	return []Event{{Kind: EvSystem, Text: truncate(fmt.Sprintf("omp asked a %s dialog (%s) that the dashboard cannot answer; it was cancelled.",
		r.Method, strings.Join(strings.Fields(r.Title), " ")), 400)}}
}

// ---- model listing ----

type ompModelCache struct {
	mu     sync.Mutex
	path   string
	at     time.Time
	models []ModelOption
}

var ompModels ompModelCache

// list returns the chat models omp has credentials for (`omp models --json`), cached for a
// couple of minutes because listing starts omp.
func (c *ompModelCache) list(ctx context.Context, exe string) []ModelOption {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == exe && time.Since(c.at) < 2*time.Minute {
		return c.models
	}
	models := listOmpModels(ctx, exe)
	if len(models) == 0 {
		return []ModelOption{} // listing failed or nothing is signed in; the UI falls back to a custom id
	}
	c.path, c.at, c.models = exe, time.Now(), models
	return models
}

func listOmpModels(ctx context.Context, exe string) []ModelOption {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "models", "--json")
	hideWindow(cmd)
	cmd.WaitDelay = time.Second
	raw, err := cmd.Output()
	if err != nil {
		return nil
	}
	var doc struct {
		Models []struct {
			Kind     string   `json:"kind"`
			Selector string   `json:"selector"`
			Name     string   `json:"name"`
			Thinking []string `json:"thinking"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	out := []ModelOption{}
	for _, m := range doc.Models {
		if (m.Kind != "" && m.Kind != "chat") || checkToken("model", m.Selector) != nil {
			continue
		}
		reasoning := []string{}
		if len(m.Thinking) > 0 {
			reasoning = append(reasoning, "off")
			for _, level := range m.Thinking {
				if level != "off" && validateReasoning("omp", level) == nil {
					reasoning = append(reasoning, level)
				}
			}
		}
		name := m.Name
		if name == "" {
			name = m.Selector
		}
		out = append(out, ModelOption{ID: m.Selector, Name: name, Reasoning: reasoning})
	}
	return out
}

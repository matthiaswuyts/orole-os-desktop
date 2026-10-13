package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CodexApp is the Codex adapter (binary `codex`).
//
// Structured mode runs `codex app-server` (JSON-RPC 2.0 lines over stdio, the protocol the Codex
// IDE extensions use): initialize, thread/start (or thread/resume), then one turn/start per prompt,
// turn/steer for follow-ups while a turn runs and turn/interrupt on Stop. Item notifications are
// mapped to normalized events when an item completes; approvals arrive as server requests and are
// relayed through Process.Respond. Terminal mode keeps the interactive TUI in a ConPTY.
//
// Orole's MCP gateway and the skills index are injected per thread (thread/start `config` and
// `developerInstructions`), so ~/.codex/config.toml is never read for them and never modified.
type CodexApp struct {
	// Bin overrides the executable (tests); "" = `codex` from PATH.
	Bin string
}

func (c CodexApp) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "codex"
}

func (CodexApp) Kind() string  { return "codex" }
func (CodexApp) Name() string  { return "Codex" }
func (CodexApp) Modes() []Mode { return []Mode{ModeStructured, ModeTerminal} }

// CanResume: structured sessions continue through thread/resume. Terminal sessions never record a
// CLI session id, so the manager never asks to resume them.
func (CodexApp) CanResume() bool { return true }

func (c CodexApp) Detect(ctx context.Context) Detection {
	d := detectBinary(ctx, "codex", "Codex", c.bin())
	d.Modes, d.Resume, d.Permissions, d.StatusFeed = c.Modes(), true, true, "app-server"
	return d
}

// terminal is the interactive `codex [-m model] [prompt]` TUI.
func (c CodexApp) terminal() terminalCLI {
	return terminalCLI{kind: "codex", name: "Codex", bin: c.bin(), args: func(model, prompt string) []string {
		var a []string
		if model != "" {
			a = append(a, "-m", model)
		}
		if prompt != "" {
			a = append(a, "--", prompt)
		}
		return a
	}}
}

// codexPolicy is how an Orole permission mode runs in Codex app-server: the approval policy, the
// sandbox, and who reviews approval requests.
type codexPolicy struct {
	approval string // untrusted | on-request | never
	sandbox  string // read-only | workspace-write | danger-full-access
	reviewer string // user | auto_review
}

// codexPolicyFor maps an Orole permission mode onto Codex:
//
//	manual  ask before anything not known to be safe (untrusted), workspace-write sandbox
//	auto    on-request in the workspace sandbox with Codex's auto-review subagent deciding which
//	        escalations are safe; the rest still reach the user
//	yolo    never ask, no sandbox
//	plan    read-only sandbox, ask to escalate
func codexPolicyFor(mode string) (codexPolicy, error) {
	switch mode {
	case "", PermManual:
		return codexPolicy{"untrusted", "workspace-write", "user"}, nil
	case PermAuto:
		return codexPolicy{"on-request", "workspace-write", "auto_review"}, nil
	case PermYolo:
		return codexPolicy{"never", "danger-full-access", "user"}, nil
	case PermPlan:
		return codexPolicy{"on-request", "read-only", "user"}, nil
	}
	return codexPolicy{}, fmt.Errorf("unsupported permission mode %q", mode)
}

// turnParams are the turn/start overrides that move a live thread onto this policy.
func (c codexPolicy) turnParams() map[string]any {
	policy := map[string]string{"read-only": "readOnly", "workspace-write": "workspaceWrite", "danger-full-access": "dangerFullAccess"}[c.sandbox]
	return map[string]any{"approvalPolicy": c.approval, "approvalsReviewer": c.reviewer, "sandboxPolicy": map[string]any{"type": policy}}
}

// PermissionModes: Codex has a native "AI decides" reviewer (approvalsReviewer = auto_review).
func (CodexApp) PermissionModes(context.Context, Detection) []PermissionModeSupport {
	return []PermissionModeSupport{
		modeSupport(PermManual, false, "Codex asks before anything not known to be safe, inside its workspace sandbox."),
		modeSupport(PermAuto, false, "Codex's auto-review agent decides which escalations from the workspace sandbox are safe."),
		modeSupport(PermYolo, false, "Codex runs without a sandbox and never asks."),
	}
}

func (c CodexApp) Start(ctx context.Context, h *Host, spec LaunchSpec) (Process, error) {
	if !localMachine(spec.Machine) {
		return nil, errors.New("local Codex adapter cannot launch on a remote machine")
	}
	switch spec.Mode {
	case ModeTerminal:
		return c.terminal().Start(ctx, h, spec)
	case "", ModeStructured:
	default:
		return nil, fmt.Errorf("Codex cannot run in %s mode", spec.Mode)
	}
	if err := validPermissionMode(spec.PermissionMode); err != nil {
		return nil, err
	}
	if err := checkToken("model", spec.Model); err != nil {
		return nil, err
	}
	if err := validateReasoning("codex", spec.Reasoning); err != nil {
		return nil, err
	}
	if err := checkToken("resume id", spec.Resume); err != nil {
		return nil, err
	}
	path, err := lookBin(c.bin())
	if err != nil {
		return nil, err
	}
	imageDir := ""
	if h != nil && spec.SessionID != "" {
		dir, err := h.sessionDir(spec.SessionID)
		if err != nil {
			return nil, err
		}
		imageDir = filepath.Join(dir, "images")
	}
	p, err := startCodexProc(spec.Dir, path)
	if err != nil {
		return nil, err
	}
	p.imageDir, p.effort = imageDir, spec.Reasoning
	if err := p.handshake(spec); err != nil {
		p.abort()
		if tail := strings.TrimSpace(p.stderr.String()); tail != "" {
			err = fmt.Errorf("%w: %s", err, truncate(tail, 600))
		}
		return nil, err
	}
	return p, nil
}

// ---- JSON-RPC plumbing ----

const (
	codexCallTimeout = 30 * time.Second
	codexMaxLine     = 64 << 20 // an imageGeneration result carries the whole image as base64
)

type rpcMsg struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("codex: %s (code %d)", e.Message, e.Code) }

type rpcResp struct {
	result json.RawMessage
	err    error
}

// codexProc is a running `codex app-server` bound to one thread.
type codexProc struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	job      *job
	events   chan Event
	stderr   *tailBuf
	wmu      sync.Mutex
	stopping atomic.Bool
	exited   chan struct{}

	imageDir string // <session dir>/images ("" = image events cannot be saved)
	effort   string // reasoning effort for every turn

	nextID atomic.Int64
	sendMu sync.Mutex // serializes Send so steer/start decisions never interleave

	mu       sync.Mutex
	calls    map[int64]chan rpcResp
	closed   bool
	threadID string
	policy   codexPolicy // what the thread runs under
	// policyDirty: policy changed since the last turn/start, so the next one carries the overrides.
	policyDirty bool
	turn        string // id of the running turn ("" = idle)
	lastDone    string // id of the last completed turn, so a late turn/start response cannot revive it
	final       string // last agent message of the current turn
	items       map[string]codexItem
	pending     map[string]*codexPending // by Orole permission id
	byRPC       map[string]string        // JSON-RPC request id -> permission id
	inputs      map[string]codexInput    // by immutable Orole question id
	live        map[string]string        // streamed text per item id, for the activity line
	liveAt      time.Time
	// billedTotal is the thread's cumulative token total at the last billed tokenUsage notification
	// (billedSeen = one arrived), so repeats with an unchanged total are not billed twice.
	billedTotal int64
	billedSeen  bool
	planSeq     int // turn/plan/updated notifications seen, so each plan revision is its own tool event
}

func startCodexProc(dir, bin string) (*codexProc, error) {
	cmd := exec.Command(bin, "app-server")
	cmd.Dir = dir
	cmd.Env = childEnv()
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &codexProc{cmd: cmd, stdin: stdin, events: make(chan Event, 256), stderr: &tailBuf{max: 8 << 10},
		exited: make(chan struct{}), calls: map[int64]chan rpcResp{}, items: map[string]codexItem{},
		pending: map[string]*codexPending{}, byRPC: map[string]string{}, live: map[string]string{}}
	cmd.Stderr = p.stderr
	j, err := newJob()
	if err != nil {
		return nil, err
	}
	p.job = j
	if err := cmd.Start(); err != nil {
		j.close()
		return nil, err
	}
	if err := j.assign(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		j.close()
		return nil, fmt.Errorf("assign to job: %w", err)
	}
	go p.run(stdout)
	return p, nil
}

func (p *codexProc) Pid() int             { return p.cmd.Process.Pid }
func (p *codexProc) TerminalID() string   { return "" }
func (p *codexProc) Events() <-chan Event { return p.events }

func (p *codexProc) emit(e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	p.events <- e
}

// readBoundedLine reads one line of any length, keeping at most limit bytes of it: a longer
// line is consumed to its end and reported as dropped, so a runaway child cannot grow memory.
func readBoundedLine(br *bufio.Reader, limit int) (line []byte, dropped bool, err error) {
	for {
		part, isPrefix, e := br.ReadLine()
		if !dropped {
			if len(line)+len(part) > limit {
				dropped, line = true, nil
			} else {
				line = append(line, part...)
			}
		}
		if e != nil || !isPrefix {
			return line, dropped, e
		}
	}
}

func (p *codexProc) run(stdout io.Reader) {
	br := bufio.NewReaderSize(stdout, 256<<10)
	for {
		line, dropped, err := readBoundedLine(br, codexMaxLine)
		switch {
		case dropped:
			p.emit(Event{Kind: EvError, Text: "Codex sent a message over 64 MiB; it was dropped"})
		case len(line) > 0:
			p.dispatch(line)
		}
		if err != nil {
			break
		}
	}
	waitErr := p.cmd.Wait()
	code := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	p.job.close()
	close(p.exited)
	p.mu.Lock()
	p.closed = true
	for id, ch := range p.calls {
		ch <- rpcResp{err: errors.New("codex app-server exited")}
		delete(p.calls, id)
	}
	var canceled []string
	for id := range p.inputs {
		delete(p.inputs, id)
		canceled = append(canceled, id)
	}
	p.mu.Unlock()
	for _, id := range canceled {
		p.emit(Event{Kind: EvQuestionCancel, Data: jraw(map[string]string{"id": id})})
	}
	p.emit(Event{Kind: EvExit, Data: jraw(map[string]any{"code": code, "stopped": p.stopping.Load(), "stderr": p.stderr.String()})})
	close(p.events)
}

func (p *codexProc) write(b []byte) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	select {
	case <-p.exited:
		return errors.New("agent process has exited")
	default:
	}
	_, err := p.stdin.Write(b)
	return err
}

func (p *codexProc) notify(method string, params any) error {
	m := map[string]any{"method": method}
	if params != nil {
		m["params"] = params
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return p.write(append(b, '\n'))
}

// call sends one request and waits for its response.
func (p *codexProc) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	id := p.nextID.Add(1)
	ch := make(chan rpcResp, 1)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("codex app-server exited")
	}
	p.calls[id] = ch
	p.mu.Unlock()
	drop := func() {
		p.mu.Lock()
		delete(p.calls, id)
		p.mu.Unlock()
	}
	m := map[string]any{"id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	b, err := json.Marshal(m)
	if err != nil {
		drop()
		return nil, err
	}
	if err := p.write(append(b, '\n')); err != nil {
		drop()
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-time.After(timeout):
		drop()
		return nil, fmt.Errorf("codex %s timed out after %s", method, timeout)
	}
}

// reply answers a server request.
func (p *codexProc) reply(id json.RawMessage, result any) error {
	b, err := json.Marshal(map[string]any{"id": id, "result": result})
	if err != nil {
		return err
	}
	return p.write(append(b, '\n'))
}

func (p *codexProc) replyError(id json.RawMessage, code int, msg string) {
	b, _ := json.Marshal(map[string]any{"id": id, "error": map[string]any{"code": code, "message": msg}})
	_ = p.write(append(b, '\n'))
}

func (p *codexProc) dispatch(line []byte) {
	var m rpcMsg
	if err := json.Unmarshal(line, &m); err != nil {
		p.emit(Event{Kind: EvActivity, Text: truncate(string(line), 200)})
		return
	}
	switch {
	case m.Method == "" && len(m.ID) > 0:
		n, err := strconv.ParseInt(string(m.ID), 10, 64)
		if err != nil {
			return
		}
		p.mu.Lock()
		ch := p.calls[n]
		delete(p.calls, n)
		p.mu.Unlock()
		if ch == nil {
			return
		}
		if m.Error != nil {
			ch <- rpcResp{err: m.Error}
		} else {
			ch <- rpcResp{result: m.Result}
		}
	case m.Method != "" && len(m.ID) > 0:
		p.serverRequest(m)
	case m.Method != "":
		p.notification(m.Method, m.Params)
	}
}

// ---- lifecycle ----

// handshake performs initialize and starts or resumes the thread, then reports EvInit.
func (p *codexProc) handshake(spec LaunchSpec) error {
	if _, err := p.call("initialize", map[string]any{
		"clientInfo": map[string]any{"name": "orole", "title": "Orole OS", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true, "optOutNotificationMethods": []string{
			"item/commandExecution/outputDelta", "item/fileChange/outputDelta", "command/exec/outputDelta",
			"process/outputDelta", "account/rateLimits/updated",
			"mcpServer/startupStatus/updated", "turn/diff/updated", "thread/status/changed",
		}},
	}, codexCallTimeout); err != nil {
		return fmt.Errorf("codex initialize: %w", err)
	}
	if err := p.notify("initialized", nil); err != nil {
		return err
	}
	policy, err := codexPolicyFor(spec.PermissionMode)
	if err != nil {
		return err
	}
	params := map[string]any{"cwd": spec.Dir, "approvalPolicy": policy.approval, "sandbox": policy.sandbox}
	if policy.reviewer != "user" {
		params["approvalsReviewer"] = policy.reviewer // "user" is the default; older servers need not know the field
	}
	if spec.Model != "" {
		params["model"] = spec.Model
	}
	if spec.SkillsPrompt != "" {
		params["developerInstructions"] = spec.SkillsPrompt
	}
	if cfg := codexConfig(spec.Gateway); cfg != nil {
		params["config"] = cfg
	}
	method := "thread/start"
	if spec.Resume != "" {
		method = "thread/resume"
		params["threadId"] = spec.Resume
		params["excludeTurns"] = true
	}
	raw, err := p.call(method, params, codexCallTimeout)
	if err != nil {
		return fmt.Errorf("codex %s: %w", method, err)
	}
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model             string `json:"model"`
		ApprovalsReviewer string `json:"approvalsReviewer"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Thread.ID == "" {
		return fmt.Errorf("codex %s returned no thread", method)
	}
	p.mu.Lock()
	p.threadID, p.policy = resp.Thread.ID, policy
	p.mu.Unlock()
	p.emit(Event{Kind: EvInit, Data: jraw(map[string]string{"agentSessionId": resp.Thread.ID, "model": resp.Model})})
	if policy.reviewer == "auto_review" && resp.ApprovalsReviewer != "auto_review" {
		// A Codex without auto-review ignores the field and keeps routing approvals to the user.
		p.emit(Event{Kind: EvSystem, Text: "This Codex did not enable auto-review; approval requests will come to you."})
	}
	return nil
}

// codexConfig renders the per-thread config overrides: Orole's gateway as the MCP server "orole".
// Its tools are approved at the Codex level because the gateway enforces Orole policy itself;
// prompting twice for one action would only add noise.
func codexConfig(g *Gateway) map[string]any {
	if g == nil || g.Command == "" {
		return nil
	}
	args := g.Args
	if args == nil {
		args = []string{}
	}
	server := map[string]any{"command": g.Command, "args": args, "default_tools_approval_mode": "approve"}
	if len(g.Env) > 0 {
		server["env"] = g.Env
	}
	return map[string]any{"mcp_servers": map[string]any{"orole": server}}
}

// abort kills a process that failed to start a thread and drains its events.
func (p *codexProc) abort() {
	p.stopping.Store(true)
	p.wmu.Lock()
	_ = p.stdin.Close()
	p.wmu.Unlock()
	p.job.kill()
	go func() {
		for range p.events {
		}
	}()
	select {
	case <-p.exited:
	case <-time.After(5 * time.Second):
	}
}

// Send starts a turn, or steers the running one.
func (p *codexProc) Send(prompt string) error {
	return p.SendConfigured(prompt, "", "")
}

func (*codexProc) FollowupOptions() (bool, bool) { return true, true }

// SendConfigured uses the app-server TurnStartParams model/effort overrides,
// which preserve the current thread. Never steer when changing configuration.
func (p *codexProc) SendConfigured(prompt, model, reasoning string) error {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	p.mu.Lock()
	thread, turn := p.threadID, p.turn
	p.mu.Unlock()
	input := []map[string]any{{"type": "text", "text": prompt, "text_elements": []any{}}}
	if turn != "" {
		if model != "" || reasoning != "" {
			return errors.New("cannot change Codex settings while a turn is running")
		}
		_, err := p.call("turn/steer", map[string]any{"threadId": thread, "expectedTurnId": turn, "input": input}, codexCallTimeout)
		if err == nil {
			return nil
		}
		var re *rpcError
		if !errors.As(err, &re) {
			return err
		}
		// The turn ended between our check and the steer; start a new one.
	}
	params := map[string]any{"threadId": thread, "input": input, "summary": "auto"}
	p.mu.Lock()
	policy, policyDirty := p.policy, p.policyDirty
	p.mu.Unlock()
	if policyDirty {
		for k, v := range policy.turnParams() {
			params[k] = v
		}
	}
	if model != "" {
		params["model"] = model
	}
	if p.effort != "" {
		params["effort"] = p.effort
	}
	if reasoning != "" {
		params["effort"] = reasoning
	}
	raw, err := p.call("turn/start", params, codexCallTimeout)
	if err != nil {
		return err
	}
	var resp struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &resp) == nil && resp.Turn.ID != "" {
		p.mu.Lock()
		if p.lastDone != resp.Turn.ID {
			p.turn = resp.Turn.ID
		}
		p.mu.Unlock()
	}
	if reasoning != "" {
		p.effort = reasoning
	}
	if policyDirty {
		p.mu.Lock()
		p.policyDirty = p.policyDirty && p.policy != policy // a newer change keeps its own overrides
		p.mu.Unlock()
	}
	return nil
}

// SetPermissionMode records the new policy; the next turn/start carries the approvalPolicy,
// approvalsReviewer and sandboxPolicy overrides, which Codex keeps for later turns. A turn that
// is running finishes under the old policy.
func (p *codexProc) SetPermissionMode(mode string) (bool, error) {
	policy, err := codexPolicyFor(mode)
	if err != nil {
		return false, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.policy, p.policyDirty = policy, true
	return true, nil
}

// Stop interrupts the running turn so Codex can end its child commands, then ends the process tree.
func (p *codexProc) Stop() error {
	p.stopping.Store(true)
	p.mu.Lock()
	thread, turn := p.threadID, p.turn
	p.mu.Unlock()
	if turn != "" {
		// Best effort: the stdin close and job kill below are what guarantee the stop.
		_, _ = p.call("turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, 2*time.Second)
	}
	p.wmu.Lock()
	_ = p.stdin.Close() // app-server exits when its input ends
	p.wmu.Unlock()
	select {
	case <-p.exited:
		return nil
	case <-time.After(2 * time.Second):
	}
	p.job.kill()
	select {
	case <-p.exited:
	case <-time.After(5 * time.Second):
		return errors.New("agent process did not exit after kill")
	}
	return nil
}

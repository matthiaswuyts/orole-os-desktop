package agents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

// Host gives adapters what they need from the daemon.
type Host struct {
	Terminals *terminals.Manager
	// DataDir holds per-session scratch files (hook settings, prompt files).
	DataDir string
	// HookExe is the executable Claude Code hooks call back into ("" disables hooks).
	HookExe string
	// GatewayExe is the executable harnesses launch (as `<exe> mcp-gateway`) to reach Orole's central
	// skills and MCP servers over authenticated IPC ("" = gateway unavailable).
	GatewayExe string
}

func (h *Host) sessionDir(id string) (string, error) {
	d := filepath.Join(h.DataDir, "agents", id)
	return d, os.MkdirAll(d, 0o700)
}

// LaunchSpec is a fully resolved request to start a process for a session.
type LaunchSpec struct {
	SessionID      string
	Kind           string
	Machine        string
	Dir            string
	Prompt         string // initial prompt ("" = none)
	Model          string
	Reasoning      string
	Mode           Mode
	Command        string // custom CLI command line
	PermissionMode string
	// AllowYolo lets a live structured conversation later switch to YOLO. Claude Code only
	// accepts that switch when launched with it; the daemon sets it only while the YOLO setting is on.
	AllowYolo bool
	// Resume is the CLI session id to continue; NewAgentSessionID names a fresh one when the
	// CLI lets us choose (Claude Code). Both empty = the CLI picks.
	Resume            string
	NewAgentSessionID string
	// Gateway is the stdio MCP server named "orole" every structured harness must be given, so
	// skills and MCP servers are central whatever model or harness runs. Nil = unavailable.
	Gateway *Gateway
	// SkillsPrompt lists the enabled central skills; append it to the harness's system or developer
	// instructions ("" = none).
	SkillsPrompt string
}

// Gateway describes how a harness starts Orole's MCP gateway. Env carries the data directory and
// session id; it never carries secrets (the gateway authenticates over the daemon pipe itself).
type Gateway struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// Process is a running agent.
type Process interface {
	Pid() int
	TerminalID() string // "" unless the agent runs in a ConPTY terminal
	// Send delivers a prompt to the running agent.
	Send(prompt string) error
	// Respond answers a permission request previously reported as EvPermission.
	Respond(requestID string, allow bool, reason string) error
	// Stop ends local process trees; remote transport loss cannot guarantee that.
	Stop() error
	// Events yields what the agent does; it is closed after the final EvExit.
	Events() <-chan Event
}

// Adapter knows how to find and launch one agent CLI.
type Adapter interface {
	Kind() string
	Name() string
	// Modes the adapter can run in; the first is the default.
	Modes() []Mode
	// CanResume reports whether a session can be continued by the CLI's own session id.
	CanResume() bool
	Detect(ctx context.Context) Detection
	Start(ctx context.Context, h *Host, spec LaunchSpec) (Process, error)
}

// ErrNoPermissions is returned by processes that cannot relay permission requests.
var ErrNoPermissions = errors.New("this agent cannot relay permission requests; answer in its terminal")

func jraw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// truncate shortens s to max runes-ish bytes on a rune boundary.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// tailBuf keeps the last max bytes written.
type tailBuf struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuf) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.b))
}

// ---- detection ----

func detectBinary(ctx context.Context, kind, name, bin string, versionArgs ...string) Detection {
	d := Detection{Kind: kind, Name: name}
	path, err := exec.LookPath(bin)
	if err != nil {
		d.Error = bin + " not found on PATH"
		return d
	}
	d.Installed, d.Path = true, path
	if len(versionArgs) == 0 {
		versionArgs = []string{"--version"}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, versionArgs...)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		d.Error = "installed, but `" + bin + " " + strings.Join(versionArgs, " ") + "` failed: " + err.Error()
	}
	d.Version = firstLine(string(out))
	return d
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return truncate(s, 200)
}

// ---- command lines ----

// escapeArg quotes one argument for a Windows command line (CommandLineToArgvW rules).
func escapeArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\n\v\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes*2+1))
			b.WriteByte('"')
			slashes = 0
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
			slashes = 0
			b.WriteByte(c)
		}
	}
	b.WriteString(strings.Repeat(`\`, slashes*2))
	b.WriteByte('"')
	return b.String()
}

func isBatch(path string) bool {
	e := strings.ToLower(filepath.Ext(path))
	return e == ".cmd" || e == ".bat"
}

var batchUnsafe = regexp.MustCompile("[\"%^&|<>!\r\n]")

// commandLine builds a CreateProcess command line for bin and args. Batch launchers (npm
// shims such as codex.cmd) go through cmd.exe, where arguments containing shell
// metacharacters cannot be passed safely; those are rejected.
func commandLine(bin string, args ...string) (string, error) {
	if !isBatch(bin) {
		parts := make([]string, 0, len(args)+1)
		parts = append(parts, escapeArg(bin))
		for _, a := range args {
			parts = append(parts, escapeArg(a))
		}
		return strings.Join(parts, " "), nil
	}
	parts := []string{`"` + bin + `"`}
	for _, a := range args {
		if batchUnsafe.MatchString(a) {
			return "", fmt.Errorf("argument %q cannot be passed through a .cmd launcher safely", truncate(a, 40))
		}
		parts = append(parts, `"`+a+`"`)
	}
	return `cmd.exe /d /s /c "` + strings.Join(parts, " ") + `"`, nil
}

// promptArg returns the text to pass as the CLI's positional prompt. Long prompts, prompts
// with shell metacharacters and prompts for batch launchers are written to a file and the
// argument just points at it, so the real prompt never crosses a command line.
func promptArg(h *Host, spec LaunchSpec, bin string) (string, error) {
	if spec.Prompt == "" {
		return "", nil
	}
	if len(spec.Prompt) <= 4000 && !strings.ContainsAny(spec.Prompt, "\r\n") && !(isBatch(bin) && batchUnsafe.MatchString(spec.Prompt)) {
		return spec.Prompt, nil
	}
	p, err := writePromptFile(h, spec)
	if err != nil {
		return "", err
	}
	if batchUnsafe.MatchString(p) {
		return "", fmt.Errorf("prompt file path %q is not safe to pass to a batch launcher", p)
	}
	return "Read the task in " + p + " and carry it out.", nil
}

func writePromptFile(h *Host, spec LaunchSpec) (string, error) {
	dir, err := h.sessionDir(spec.SessionID)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(p, []byte(spec.Prompt), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

// safeToken validates model names and ids passed as CLI arguments.
var safeToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\-\[\]@]{0,127}$`)

func checkToken(what, v string) error {
	if what == "model" {
		return validateModel(v)
	}
	if v != "" && !safeToken.MatchString(v) {
		return fmt.Errorf("invalid %s %q", what, v)
	}
	return nil
}

// ---- pipe (structured) processes ----

type pendingPermission struct {
	requestID string
	input     json.RawMessage
}

// pipeProc is a headless child process speaking a line-delimited JSON protocol.
type pipeProc struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	job      *job
	events   chan Event
	stderr   *tailBuf
	wmu      sync.Mutex
	stopping atomic.Bool
	exited   chan struct{}

	encodePrompt   func(prompt string) []byte
	encodeReply    func(requestID string, allow bool, reason string, input json.RawMessage) []byte
	pmu            sync.Mutex
	pendingIn      map[string]pendingPermission
	controlMu      sync.Mutex
	controlPending map[string]chan controlReply
}

// startPipe launches bin and decodes each stdout line with decode.
func startPipe(dir, bin string, args []string, decode func(p *pipeProc, line []byte) []Event) (*pipeProc, error) {
	cmd := exec.Command(bin, args...)
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
	p := &pipeProc{cmd: cmd, stdin: stdin, events: make(chan Event, 256), stderr: &tailBuf{max: 8 << 10},
		exited: make(chan struct{}), pendingIn: map[string]pendingPermission{}}
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
		cmd.Process.Kill()
		j.close()
		return nil, fmt.Errorf("assign to job: %w", err)
	}
	go p.run(stdout, decode)
	return p, nil
}

func (p *pipeProc) emit(e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	p.events <- e
}

// pipeMaxLine bounds one protocol line from a headless agent (tool results and image payloads
// can be large; a runaway child must not be able to grow the daemon without limit).
const pipeMaxLine = 16 << 20

func (p *pipeProc) run(stdout io.Reader, decode func(p *pipeProc, line []byte) []Event) {
	br := bufio.NewReaderSize(stdout, 256<<10)
	for {
		line, dropped, err := readBoundedLine(br, pipeMaxLine)
		if dropped {
			p.emit(Event{Kind: EvError, Text: "The agent sent a protocol line over 16 MiB; it was dropped"})
		} else if t := bytes.TrimSpace(line); len(t) > 0 {
			for _, e := range decode(p, t) {
				p.emit(e)
			}
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
	p.emit(Event{Kind: EvExit, Data: jraw(map[string]any{"code": code, "stopped": p.stopping.Load(), "stderr": p.stderr.String()})})
	close(p.events)
}

func (p *pipeProc) Pid() int             { return p.cmd.Process.Pid }
func (p *pipeProc) TerminalID() string   { return "" }
func (p *pipeProc) Events() <-chan Event { return p.events }
func (p *pipeProc) write(b []byte) error {
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

func (p *pipeProc) Send(prompt string) error {
	if p.encodePrompt == nil {
		return errors.New("agent does not accept prompts")
	}
	return p.write(p.encodePrompt(prompt))
}

func (p *pipeProc) Respond(id string, allow bool, reason string) error {
	if p.encodeReply == nil {
		return ErrNoPermissions
	}
	p.pmu.Lock()
	in, ok := p.pendingIn[id]
	delete(p.pendingIn, id)
	p.pmu.Unlock()
	if !ok {
		return fmt.Errorf("no pending permission request %q", id)
	}
	return p.write(p.encodeReply(in.requestID, allow, reason, in.input))
}

func (p *pipeProc) Stop() error {
	p.stopping.Store(true)
	p.wmu.Lock()
	_ = p.stdin.Close() // a stream-json CLI exits when its input ends
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

// childEnv is the daemon's environment minus markers that make a nested Claude Code
// believe it runs inside another session.
func childEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ---- terminal (ConPTY) processes ----

// ptyProc is an interactive CLI running in a terminals.Manager session.
type ptyProc struct {
	term     *terminals.Manager
	id       string
	pid      int
	job      *job
	events   chan Event
	stopping atomic.Bool
	exited   chan struct{}
	// hooks reports whether status arrives through CLI hooks (Claude Code).
}

func startPty(h *Host, sessionID, dir, cmdline, title string) (*ptyProc, error) {
	if h.Terminals == nil {
		return nil, errors.New("terminal backend unavailable")
	}
	j, err := newJob()
	if err != nil {
		return nil, err
	}
	info, err := h.Terminals.Create(terminals.CreateOptions{Command: cmdline, Cwd: dir, Title: title, Owner: "agent:" + sessionID, Cols: 120, Rows: 32})
	if err != nil {
		j.close()
		return nil, err
	}
	if err := j.assign(info.Pid); err != nil {
		_ = h.Terminals.Close(info.ID)
		j.close()
		return nil, fmt.Errorf("assign to job: %w", err)
	}
	p := &ptyProc{term: h.Terminals, id: info.ID, pid: info.Pid, job: j, events: make(chan Event, 64), exited: make(chan struct{})}
	go p.watch()
	return p, nil
}

// watch turns terminal exit into the final event: it blocks on the terminals manager's exit
// notification instead of polling the session list.
func (p *ptyProc) watch() {
	info, err := p.term.Wait(context.Background(), p.id)
	switch {
	case err != nil: // closed by the user, or already forgotten
		p.finish(-1)
	case info.ExitCode != nil:
		p.finish(*info.ExitCode)
	default:
		p.finish(0)
	}
}

func (p *ptyProc) finish(code int) {
	p.job.kill()
	p.job.close()
	close(p.exited)
	p.events <- Event{At: time.Now().UTC(), Kind: EvExit, Data: jraw(map[string]any{"code": code, "stopped": p.stopping.Load()})}
	close(p.events)
}

func (p *ptyProc) Pid() int             { return p.pid }
func (p *ptyProc) TerminalID() string   { return p.id }
func (p *ptyProc) Events() <-chan Event { return p.events }

// Send cannot safely identify which foreground program currently reads a PTY.
// Only the attached terminal UI may provide interactive input.
func (p *ptyProc) Send(string) error {
	return errors.New("send follow-up input in this agent's attached terminal; programmatic PTY prompts are not supported")
}

func (p *ptyProc) Respond(string, bool, string) error { return ErrNoPermissions }

func (p *ptyProc) Stop() error {
	p.stopping.Store(true)
	p.job.kill()
	select {
	case <-p.exited:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("agent terminal did not exit after kill")
	}
}

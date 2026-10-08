package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/machines"
)

// Config wires a Manager.
type Config struct {
	Store          *Store
	Host           *Host
	Log            *slog.Logger
	Worktrees      Worktrees // optional; without it NewWorktree and diffs are unavailable
	Machines       *machines.Manager
	ProjectOptions func(context.Context) ([]ProjectOption, error)
	// Publish delivers "agents.updated" (Session), "agents.event" (Event) and
	// "agents.removed" ({id}) to subscribed clients.
	Publish  func(event string, data any)
	Finished func(Session) // called once per completed turn, or process-only completion
	// PermissionDecision checks policy before honoring a user's Allow. Errors fail closed
	// and send Deny to the CLI; the existing agent approval UI supplies the Ask decision.
	PermissionDecision func(context.Context, string, PermissionRequest) error
	// Authorize gates process execution, prompts, checks and destructive review in the daemon.
	// Missing authorization fails closed; trusted test hosts must explicitly supply it.
	Authorize func(context.Context, string, string, string) error
	// Audit records security-relevant decisions (permission answers, process launches).
	Audit    func(action, target, outcome, detail string)
	Adapters []Adapter // default: all built in
	MaxLive  int       // default 16
}

// Manager owns every live agent process and the session records.
type Manager struct {
	cfg      Config
	st       *Store
	adapters map[string]Adapter
	order    []string
	boot     string

	launchMu   sync.Mutex   // serialize starts/resumes against each other and shutdown
	worktreeMu sync.RWMutex // removal excludes manual and background Git readers
	bgWork     sync.WaitGroup
	mu         sync.Mutex
	live       map[string]*liveSess
	changed    chan struct{}
	closing    bool

	bg       context.Context // cancelled by Shutdown; owns background checks
	bgCancel context.CancelFunc

	detectMu sync.Mutex
	detectAt time.Time
	detected []Detection
}

type liveSess struct {
	id       string // immutable; s is guarded by Manager.mu
	s        Session
	proc     Process
	stopping bool
	shutdown bool
	done     chan struct{}
	// expectPrompt is the prompt we typed/passed ourselves, so the matching
	// UserPromptSubmit hook doesn't record it twice.
	expectPrompt string
}

// NewManager creates a manager. Call Recover once at startup.
func NewManager(cfg Config) *Manager {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Publish == nil {
		cfg.Publish = func(string, any) {}
	}
	if cfg.Audit == nil {
		cfg.Audit = func(string, string, string, string) {}
	}
	if cfg.MaxLive <= 0 {
		cfg.MaxLive = 16
	}
	if len(cfg.Adapters) == 0 {
		cfg.Adapters = []Adapter{Claude{}, Codex(), OpenCode(), Gemini(), Custom{}}
	}
	m := &Manager{cfg: cfg, st: cfg.Store, adapters: map[string]Adapter{}, boot: uuid.NewString(),
		live: map[string]*liveSess{}, changed: make(chan struct{})}
	m.bg, m.bgCancel = context.WithCancel(context.Background())
	for _, a := range cfg.Adapters {
		m.adapters[a.Kind()] = a
		m.order = append(m.order, a.Kind())
	}
	return m
}

// Store exposes persistence to orchestration.
func (m *Manager) Store() *Store { return m.st }

// Boot identifies this daemon run in the journal.
func (m *Manager) Boot() string { return m.boot }

// Recover marks sessions whose process died with the previous daemon as lost, and flags
// unfinished journal rows/runs as interrupted. It never re-runs anything.
func (m *Manager) Recover() (lost int, err error) {
	sessions, err := m.st.ListSessions()
	if err != nil {
		return 0, err
	}
	for _, s := range sessions {
		switch s.Status {
		case StatusIdle, StatusWorking, StatusNeedsYou:
		default:
			continue
		}
		now := time.Now().UTC()
		s.Status = StatusLost
		s.EndedAt = &now
		s.UpdatedAt = now
		s.Activity = ""
		s.Error = "The daemon restarted while this agent was running; its process was owned by the previous daemon and is gone."
		if !localMachine(s.Machine) {
			s.Error = "The daemon restarted and lost SSH process control. The remote process may still be running; verify the selected box before restarting."
		}
		if m.adapterCanResume(s.Kind) && s.AgentSessionID != "" {
			s.Error += " It can be resumed from its CLI session."
		}
		if err := m.st.SaveSession(s, ""); err != nil {
			return lost, err
		}
		ev := Event{SessionID: s.ID, At: now, Kind: EvSystem, Text: "Marked lost: " + s.Error}
		_ = m.st.AppendEvent(&ev)
		lost++
	}
	_, _, err = m.st.MarkInterrupted(m.boot)
	return lost, err
}

func (m *Manager) adapterCanResume(kind string) bool {
	a, ok := m.adapters[kind]
	return ok && a.CanResume()
}

// Detect reports which agent CLIs are installed. Results are cached for a minute.
func (m *Manager) Detect(ctx context.Context, refresh bool) []Detection {
	m.detectMu.Lock()
	defer m.detectMu.Unlock()
	if !refresh && m.detected != nil && time.Since(m.detectAt) < time.Minute {
		return m.detected
	}
	out := make([]Detection, len(m.order))
	var wg sync.WaitGroup
	for i, k := range m.order {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := m.adapters[k]
			d := a.Detect(ctx)
			d.Modes, d.Resume = a.Modes(), a.CanResume()
			out[i] = d
		}()
	}
	wg.Wait()
	m.detected, m.detectAt = out, time.Now()
	return out
}

// ---- reading ----

func (m *Manager) snapshotLocked(ls *liveSess) Session {
	s := ls.s
	s.Live = true
	s.Pending = append([]PermissionRequest{}, s.Pending...)
	return s
}

// Get returns one session with runtime state overlaid.
func (m *Manager) Get(id string) (Session, error) {
	m.mu.Lock()
	if ls, ok := m.live[id]; ok {
		s := m.snapshotLocked(ls)
		m.mu.Unlock()
		return s, nil
	}
	m.mu.Unlock()
	return m.st.GetSession(id)
}

// List returns every session, newest first.
func (m *Manager) List() ([]Session, error) {
	all, err := m.st.ListSessions()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range all {
		if ls, ok := m.live[all[i].ID]; ok {
			all[i] = m.snapshotLocked(ls)
		}
	}
	return all, nil
}

// Events returns a session's history after the given event id.
func (m *Manager) Events(id string, after int64, limit int) ([]Event, error) {
	if _, err := m.Get(id); err != nil {
		return nil, err
	}
	return m.st.Events(id, after, limit)
}

// transcriptKinds are the events that make up the readable conversation.
var transcriptKinds = []string{EvPrompt, EvMessage, EvTool, EvToolResult, EvPermission, EvDecision, EvTurnDone, EvError, EvSystem, EvNeedsInput}

// Transcript returns the newest `limit` conversation events, oldest first.
func (m *Manager) Transcript(id string, limit int) ([]Event, error) {
	if _, err := m.Get(id); err != nil {
		return nil, err
	}
	return m.st.LastEvents(id, limit, transcriptKinds...)
}

// WaitFor blocks until pred holds for the session (or ctx ends).
func (m *Manager) WaitFor(ctx context.Context, id string, pred func(Session) bool) (Session, error) {
	for {
		m.mu.Lock()
		ch := m.changed
		m.mu.Unlock()
		s, err := m.Get(id)
		if err != nil {
			return s, err
		}
		if pred(s) {
			return s, nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return s, ctx.Err()
		}
	}
}

// ---- mutation plumbing ----

// mutate loads the session (live cache first), applies f, persists and publishes it.
func (m *Manager) mutate(id string, f func(s *Session)) (Session, error) {
	m.mu.Lock()
	s, err := m.mutateLocked(id, f)
	m.mu.Unlock()
	if err == nil {
		m.cfg.Publish("agents.updated", s)
	}
	return s, err
}

func (m *Manager) mutateLocked(id string, f func(s *Session)) (Session, error) {
	ls := m.live[id]
	var s Session
	if ls != nil {
		s = ls.s
	} else {
		var err error
		if s, err = m.st.GetSession(id); err != nil {
			return Session{}, err
		}
	}
	f(&s)
	s.UpdatedAt = time.Now().UTC()
	if err := m.st.SaveSession(s, ""); err != nil {
		return s, err
	}
	if ls != nil {
		ls.s = s
		s = m.snapshotLocked(ls)
	} else if s.Pending == nil {
		s.Pending = []PermissionRequest{}
	}
	close(m.changed)
	m.changed = make(chan struct{})
	return s, nil
}

// record persists and publishes an event.
func (m *Manager) record(sessionID string, e Event) Event {
	e.SessionID = sessionID
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	e.Text = truncate(e.Text, 16<<10)
	if len(e.Data) == 0 {
		e.Data = json.RawMessage("null")
	}
	if e.Kind != EvOutputBytes {
		if err := m.st.AppendEvent(&e); err != nil {
			m.cfg.Log.Warn("agent event write failed", "session", sessionID, "err", err)
		}
	}
	m.cfg.Publish("agents.event", e)
	return e
}

// ---- start ----

func newBranchName(kind, id string) string { return kind + "-" + id[:8] }

func titleFor(spec StartSpec, a Adapter) string {
	if t := strings.TrimSpace(spec.Title); t != "" {
		return truncate(t, 120)
	}
	if p := strings.TrimSpace(spec.Prompt); p != "" {
		return truncate(firstLine(p), 80)
	}
	return a.Name()
}

func (m *Manager) authorize(ctx context.Context, action, resource, detail string) error {
	if m.cfg.Authorize == nil {
		return errors.New("agent authorization is unavailable")
	}
	return m.cfg.Authorize(ctx, action, resource, detail)
}

// Start creates a session and launches its agent. With spec.Key set, a repeated request
// returns the session the first one created instead of starting another.
func (m *Manager) Start(ctx context.Context, spec StartSpec) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if spec.Machine == "" {
		spec.Machine = "local"
	}
	if err := validateModel(spec.Model); err != nil {
		return Session{}, err
	}
	if err := validateReasoning(spec.Kind, spec.Reasoning); err != nil {
		return Session{}, err
	}
	if len(spec.Prompt) > 100<<10 || strings.ContainsRune(spec.Prompt, 0) {
		return Session{}, errors.New("prompt must be at most 100 KiB and contain no NUL")
	}
	// Reading an already-dispatched result has no execution side effect. Keep
	// retries idempotent without requiring a second process-launch approval.
	if spec.Key != "" {
		if s, ok, err := m.st.SessionByKey(spec.Key); err != nil {
			return Session{}, err
		} else if ok {
			return m.Get(s.ID)
		}
	}
	if err := m.authorize(ctx, "agent.start", spec.Machine+":"+spec.Dir, fmt.Sprintf("Start %s on box %s in %s. Command: %s", spec.Kind, spec.Machine, spec.Dir, spec.Command)); err != nil {
		return Session{}, err
	}
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if spec.Key != "" {
		if s, ok, err := m.st.SessionByKey(spec.Key); err != nil {
			return Session{}, err
		} else if ok {
			return m.Get(s.ID)
		}
	}
	a, ok := m.adapters[spec.Kind]
	if !ok {
		return Session{}, fmt.Errorf("unknown agent kind %q", spec.Kind)
	}
	mode := spec.Mode
	modes := a.Modes()
	if !localMachine(spec.Machine) {
		if spec.Kind != "claude" && spec.Kind != "codex" {
			return Session{}, errors.New("this harness has no remote process adapter")
		}
		if spec.NewWorktree {
			return Session{}, errors.New("remote worktree creation is not supported")
		}
		if strings.TrimSpace(spec.Prompt) == "" {
			return Session{}, errors.New("remote process mode requires a task")
		}
		modes = []Mode{ModeProcess}
	}
	if mode == "" {
		mode = modes[0]
	}
	supported := false
	for _, x := range modes {
		supported = supported || x == mode
	}
	if !supported {
		return Session{}, fmt.Errorf("%s cannot run in %s mode on this box", a.Name(), mode)
	}
	if err := validPermissionMode(spec.PermissionMode); err != nil {
		return Session{}, err
	}
	if spec.Kind == "custom" && strings.TrimSpace(spec.Command) == "" {
		return Session{}, errors.New("custom agent needs a command")
	}
	dir := spec.Dir
	if dir == "" {
		return Session{}, errors.New("a project or worktree directory is required")
	}
	if localMachine(spec.Machine) {
		var err error
		dir, err = filepath.Abs(spec.Dir)
		if err != nil {
			return Session{}, err
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			return Session{}, fmt.Errorf("directory %s does not exist", dir)
		}
	}
	if spec.NewWorktree && m.cfg.Worktrees == nil {
		return Session{}, errors.New("worktrees are unavailable")
	}
	m.mu.Lock()
	n, closing := len(m.live), m.closing
	m.mu.Unlock()
	if closing {
		return Session{}, errors.New("daemon is shutting down")
	}
	if n >= m.cfg.MaxLive {
		return Session{}, fmt.Errorf("agent limit reached (%d running); stop one first", m.cfg.MaxLive)
	}

	now := time.Now().UTC()
	s := Session{
		ID: uuid.NewString(), Kind: spec.Kind, Machine: spec.Machine, Project: dir, Dir: dir, Model: spec.Model, Reasoning: spec.Reasoning,
		Title: titleFor(spec, a), Task: strings.TrimSpace(spec.Prompt), Mode: mode, Command: strings.TrimSpace(spec.Command),
		PermissionMode: spec.PermissionMode, Status: StatusIdle, ParentID: spec.ParentID, Group: spec.Group,
		Pending: []PermissionRequest{}, StartedAt: now, UpdatedAt: now, Chain: events.ChainFrom(ctx),
	}
	if localMachine(s.Machine) && a.CanResume() && a.Kind() == "claude" {
		s.AgentSessionID = NewClaudeSessionID()
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	// The row (with its unique key) goes in before any side effect so a retry or a crash
	// can never produce a second session or a second worktree.
	if err := m.st.SaveSession(s, spec.Key); err != nil {
		if spec.Key != "" {
			if ex, ok, e2 := m.st.SessionByKey(spec.Key); e2 == nil && ok {
				return m.Get(ex.ID)
			}
		}
		return Session{}, err
	}
	if spec.NewWorktree {
		if err := ctx.Err(); err != nil {
			return m.failStart(s, err)
		}
		name := spec.Branch
		if name == "" {
			name = newBranchName(spec.Kind, s.ID)
		}
		path, branch, err := m.cfg.Worktrees.Create(ctx, dir, name, spec.BaseRef)
		if err != nil {
			return m.failStart(s, fmt.Errorf("create worktree: %w", err))
		}
		s.Dir, s.Branch, s.WorktreeOwned = path, branch, true
	}
	if localMachine(s.Machine) && m.cfg.Worktrees != nil {
		if head, err := m.cfg.Worktrees.Head(ctx, s.Dir); err == nil {
			s.BaseRef = head
		}
	}
	if err := m.launch(ctx, &s, s.Task, false); err != nil {
		return m.failStart(s, err)
	}
	m.cfg.Audit("agent.start", s.ID, "allowed", fmt.Sprintf("%s mode=%s dir=%s", s.Kind, s.Mode, s.Dir))
	return m.Get(s.ID)
}

func (m *Manager) failStart(s Session, err error) (Session, error) {
	now := time.Now().UTC()
	s.Status, s.Error, s.EndedAt, s.UpdatedAt = StatusFailed, err.Error(), &now, now
	if errors.Is(err, machines.ErrAgentDispatchUncertain) {
		s.Status = StatusLost
	}
	_ = m.st.SaveSession(s, "")
	m.record(s.ID, Event{Kind: EvError, Text: err.Error()})
	m.cfg.Publish("agents.updated", s)
	m.cfg.Audit("agent.start", s.ID, "error", err.Error())
	return s, err
}

// launch starts the process for s and registers it as live. prompt is the first prompt to
// deliver ("" = none). resume continues s.AgentSessionID.
func (m *Manager) launch(ctx context.Context, s *Session, prompt string, resume bool) error {
	a := m.adapters[s.Kind]
	spec := LaunchSpec{SessionID: s.ID, Kind: s.Kind, Machine: s.Machine, Dir: s.Dir, Model: s.Model, Reasoning: s.Reasoning, Mode: s.Mode, Command: s.Command, PermissionMode: s.PermissionMode}
	if resume {
		spec.Resume = s.AgentSessionID
	} else if s.Kind == "claude" {
		spec.NewAgentSessionID = s.AgentSessionID
	}
	if s.Mode == ModeTerminal || s.Mode == ModeProcess {
		spec.Prompt = prompt // delivered on the command line (or via a prompt file)
	}
	// Cancellation is honored until dispatch. Once dispatched the daemon, not the
	// requesting GUI connection, owns the process lifetime.
	if err := ctx.Err(); err != nil {
		return err
	}
	var proc Process
	var err error
	if localMachine(s.Machine) {
		proc, err = a.Start(context.WithoutCancel(ctx), m.cfg.Host, spec)
	} else {
		proc, err = m.startRemote(ctx, spec)
	}
	if err != nil {
		return err
	}
	ls := &liveSess{id: s.ID, proc: proc, done: make(chan struct{}), expectPrompt: prompt}
	s.Pid, s.TerminalID, s.Live = proc.Pid(), proc.TerminalID(), true
	s.Status, s.Error, s.Result, s.ExitCode, s.EndedAt = StatusIdle, "", "", nil, nil
	s.Pending = []PermissionRequest{}
	s.Activity = "Starting " + a.Name()
	if prompt != "" && (s.Mode == ModeTerminal || s.Mode == ModeProcess) {
		s.Status, s.Activity = StatusWorking, "Working on the task"
		s.Prompts++
	}
	s.UpdatedAt = time.Now().UTC()
	ls.s = *s
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		_ = proc.Stop()
		return errors.New("daemon is shutting down")
	}
	m.live[s.ID] = ls
	err = m.st.SaveSession(ls.s, "")
	snap := m.snapshotLocked(ls)
	close(m.changed)
	m.changed = make(chan struct{})
	m.mu.Unlock()
	if err != nil {
		_ = proc.Stop()
		return err
	}
	*s = snap
	m.cfg.Publish("agents.updated", snap)
	go m.consume(ls)
	if prompt != "" {
		if s.Mode == ModeTerminal || s.Mode == ModeProcess {
			m.record(s.ID, Event{Kind: EvPrompt, Text: prompt})
		} else if err := m.sendPrompt(ls, prompt); err != nil {
			_ = proc.Stop()
			return fmt.Errorf("send first prompt: %w", err)
		}
	}
	return nil
}

// sendPrompt flips the session to working, then delivers the prompt.
func (m *Manager) sendPrompt(ls *liveSess, prompt string) error {
	id := ls.id
	m.mu.Lock()
	ls.expectPrompt = prompt
	m.mu.Unlock()
	m.record(id, Event{Kind: EvPrompt, Text: prompt})
	var prevStatus Status
	_, _ = m.mutate(id, func(s *Session) {
		prevStatus = s.Status
		s.Prompts++
		s.Status, s.Activity, s.Error, s.Result = StatusWorking, "Working", "", ""
	})
	if err := ls.proc.Send(prompt); err != nil {
		_, _ = m.mutate(id, func(s *Session) { s.Status, s.Activity = prevStatus, "" })
		return err
	}
	return nil
}

// ---- operations ----

// Prompt sends text to a session. A session whose process has ended is resumed first
// (through the CLI's session id where supported; otherwise restarted fresh).
func (m *Manager) Prompt(ctx context.Context, id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 100<<10 {
		return errors.New("prompt must be 1-100000 bytes")
	}
	if err := m.authorize(ctx, "agent.prompt", id, "Send a task to the agent; it may execute tools under its configured permissions."); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if chain := events.ChainFrom(ctx); len(chain) > 0 {
		if _, err := m.mutate(id, func(s *Session) { s.Chain = chain }); err != nil {
			return err
		}
	}
	m.mu.Lock()
	ls := m.live[id]
	var pending int
	var mode Mode
	if ls != nil {
		pending = len(ls.s.Pending)
		mode = ls.s.Mode
	}
	m.mu.Unlock()
	if ls == nil {
		_, err := m.relaunch(ctx, id, text, false)
		return err
	}
	if mode == ModeProcess {
		return errors.New("remote process mode accepts one task per launch; wait for exit, then restart with a new task")
	}
	if mode == ModeTerminal {
		return errors.New("continue in the agent's attached terminal; programmatic PTY prompts are not supported")
	}
	if pending > 0 {
		return errors.New("the agent is waiting for a permission decision; approve or deny it first")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.sendPrompt(ls, text)
}

// Approve answers a pending permission request. The decision is made here, in the daemon,
// and audited; the UI only relays it.
func (m *Manager) Approve(ctx context.Context, id, requestID string, allow bool, reason string) error {
	m.mu.Lock()
	ls := m.live[id]
	var req *PermissionRequest
	if ls != nil {
		for i := range ls.s.Pending {
			if ls.s.Pending[i].ID == requestID {
				r := ls.s.Pending[i]
				req = &r
			}
		}
	}
	m.mu.Unlock()
	if ls == nil || req == nil {
		return fmt.Errorf("no pending permission request %q for this session", requestID)
	}
	var policyErr error
	if allow && m.cfg.PermissionDecision != nil {
		policyErr = m.cfg.PermissionDecision(ctx, id, *req)
		if policyErr != nil {
			allow, reason = false, policyErr.Error()
		}
	}
	if err := ls.proc.Respond(requestID, allow, reason); err != nil {
		m.cfg.Audit("agent.permission", id, "error", err.Error())
		return err
	}
	outcome := "allowed"
	if !allow {
		outcome = "denied"
	}
	m.cfg.Audit("agent.permission", id, outcome, req.Tool+": "+truncate(req.Summary, 300))
	m.record(id, Event{Kind: EvDecision, Text: outcome + " " + req.Tool, Data: jraw(map[string]any{"id": requestID, "allow": allow, "reason": reason, "tool": req.Tool})})
	_, err := m.mutate(id, func(s *Session) {
		rest := s.Pending[:0:0]
		for _, p := range s.Pending {
			if p.ID != requestID {
				rest = append(rest, p)
			}
		}
		s.Pending = rest
		if len(rest) == 0 && s.Status == StatusNeedsYou {
			s.Status, s.Activity = StatusWorking, "Continuing after "+outcome
		}
	})
	if err != nil {
		return err
	}
	return policyErr
}

// Stop ends a running session's process tree.
func (m *Manager) Stop(ctx context.Context, id string) (Session, error) {
	m.mu.Lock()
	ls := m.live[id]
	if ls != nil {
		ls.stopping = true
	}
	m.mu.Unlock()
	if ls == nil {
		return Session{}, errors.New("agent is not running")
	}
	m.cfg.Audit("agent.stop", id, "allowed", "")
	if err := ls.proc.Stop(); err != nil {
		return Session{}, err
	}
	select {
	case <-ls.done:
	case <-ctx.Done():
		return Session{}, ctx.Err()
	case <-time.After(10 * time.Second):
	}
	return m.Get(id)
}

// WithStoppedSession prevents a session from being relaunched while its committed
// work is inspected or published. The callback must not launch another agent.
// External editors remain outside this lock; callers must pin exact commit IDs.
func (m *Manager) WithStoppedSession(ctx context.Context, id string, fn func() error) error {
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s, err := m.Get(id)
	if err != nil {
		return err
	}
	if !localMachine(s.Machine) {
		return errRemoteOperation
	}
	if s.Live {
		return errors.New("stop the agent before preparing or publishing its work")
	}
	return fn()
}

// Restart (re)launches an ended or running session. Without fresh, a CLI that supports it is
// resumed in its own session; otherwise (or with fresh) the task is started over.
func (m *Manager) Restart(ctx context.Context, id string, fresh bool, prompt string) (Session, error) {
	if err := m.authorize(ctx, "agent.restart", id, "Restart this agent under its configured permissions."); err != nil {
		return Session{}, err
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	ls := m.live[id]
	m.mu.Unlock()
	if ls != nil {
		current, err := m.Get(id)
		if err != nil {
			return Session{}, err
		}
		if !localMachine(current.Machine) {
			return Session{}, errors.New("stop the remote process and verify it ended on the box before starting a fresh task")
		}
		if _, err := m.Stop(ctx, id); err != nil {
			return Session{}, err
		}
	}
	return m.relaunch(ctx, id, strings.TrimSpace(prompt), fresh)
}

func (m *Manager) relaunch(ctx context.Context, id, prompt string, fresh bool) (Session, error) {
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	return m.relaunchLocked(ctx, id, prompt, fresh)
}

// relaunchLocked requires launchMu; scoped delivery uses it to pin the recipient
// across the final identity check and a continuation launch.
func (m *Manager) relaunchLocked(ctx context.Context, id, prompt string, fresh bool) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return Session{}, errors.New("daemon is shutting down")
	}
	_, running := m.live[id]
	n := len(m.live)
	m.mu.Unlock()
	if running {
		return Session{}, errors.New("agent is already running")
	}
	if n >= m.cfg.MaxLive {
		return Session{}, fmt.Errorf("agent limit reached (%d running); stop one first", m.cfg.MaxLive)
	}
	s, err := m.st.GetSession(id)
	if err != nil {
		return Session{}, err
	}
	if !localMachine(s.Machine) && s.Status == StatusLost && !fresh {
		return Session{}, errors.New("remote process termination is unconfirmed; verify the box, then explicitly restart fresh")
	}
	if localMachine(s.Machine) {
		if fi, err := os.Stat(s.Dir); err != nil || !fi.IsDir() {
			return Session{}, fmt.Errorf("working directory %s no longer exists", s.Dir)
		}
	}
	resume := localMachine(s.Machine) && !fresh && m.adapterCanResume(s.Kind) && s.AgentSessionID != ""
	note := "Resumed the CLI session " + s.AgentSessionID
	if !resume {
		note = "Restarted from the task (a new CLI session)"
		if !fresh && !m.adapterCanResume(s.Kind) {
			note = "This agent cannot resume by session id; restarted from the task"
		}
		if localMachine(s.Machine) && s.Kind == "claude" {
			s.AgentSessionID = NewClaudeSessionID()
		} else {
			s.AgentSessionID = ""
		}
		s.Turns = 0
		if prompt == "" {
			prompt = s.Task
		}
	}
	if old := s.TerminalID; old != "" && m.cfg.Host != nil && m.cfg.Host.Terminals != nil {
		_ = m.cfg.Host.Terminals.Close(old)
	}
	s.TerminalID = ""
	m.record(id, Event{Kind: EvSystem, Text: note})
	if err := m.launch(ctx, &s, prompt, resume); err != nil {
		return m.failStart(s, err)
	}
	m.cfg.Audit("agent.restart", id, "allowed", note)
	return m.Get(id)
}

// Remove deletes a session that is not running, with its history and scratch files.
func (m *Manager) Remove(ctx context.Context, id string) error {
	m.mu.Lock()
	_, running := m.live[id]
	m.mu.Unlock()
	if running {
		return errors.New("stop the agent before removing it")
	}
	s, err := m.st.GetSession(id)
	if err != nil {
		return err
	}
	if s.TerminalID != "" && m.cfg.Host != nil && m.cfg.Host.Terminals != nil {
		_ = m.cfg.Host.Terminals.Close(s.TerminalID)
	}
	if err := m.st.DeleteSession(id); err != nil {
		return err
	}
	if m.cfg.Host != nil && m.cfg.Host.DataDir != "" {
		_ = os.RemoveAll(filepath.Join(m.cfg.Host.DataDir, "agents", id))
	}
	m.cfg.Audit("agent.remove", id, "allowed", "")
	m.cfg.Publish("agents.removed", map[string]string{"id": id})
	return nil
}

// Shutdown kills local process trees and cancels remote transports. Remote sessions
// whose termination cannot be confirmed remain lost, not stopped.
func (m *Manager) Shutdown() {
	m.bgCancel()
	m.launchMu.Lock()
	defer m.launchMu.Unlock()
	m.mu.Lock()
	m.closing = true
	var all []*liveSess
	for _, ls := range m.live {
		ls.stopping, ls.shutdown = true, true
		all = append(all, ls)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, ls := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ls.proc.Stop()
			select {
			case <-ls.done:
			case <-time.After(5 * time.Second):
			}
		}()
	}
	wg.Wait()
	m.bgWork.Wait()
}

// ---- event handling ----

func (m *Manager) consume(ls *liveSess) {
	id := ls.id
	defer close(ls.done)
	for e := range ls.proc.Events() {
		if e.Kind == EvExit {
			m.finalize(ls, e)
			return
		}
		m.apply(id, e)
	}
	// Channel closed without an exit event: treat as an unknown exit.
	m.finalize(ls, Event{Kind: EvExit, Data: jraw(map[string]any{"code": -1})})
}

func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")), "\n")
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			keep = append([]string{truncate(l, 200)}, keep...)
		}
	}
	return strings.Join(keep, "\n")
}

// apply folds one process event into the session record.
func (m *Manager) apply(id string, e Event) {
	e = m.record(id, e)
	var data map[string]json.RawMessage
	_ = json.Unmarshal(e.Data, &data)
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(data[k], &s)
		return s
	}
	switch e.Kind {
	case EvInit:
		aid, model := str("agentSessionId"), str("model")
		_, _ = m.mutate(id, func(s *Session) {
			if aid != "" {
				s.AgentSessionID = aid
			}
			if model != "" {
				s.Model = model
			}
		})
	case EvMessage:
		_, _ = m.mutate(id, func(s *Session) {
			s.LastMessage = truncate(e.Text, 32<<10)
			s.Tail = lastLines(e.Text, 3)
			s.Activity = truncate(firstLine(e.Text), 120)
			wakeIdle(s)
		})
	case EvTool:
		_, _ = m.mutate(id, func(s *Session) {
			s.Activity, s.Tail = truncate(e.Text, 160), truncate(e.Text, 200)
			wakeIdle(s)
		})
	case EvToolResult:
		_, _ = m.mutate(id, func(s *Session) { s.Tail = lastLines(e.Text, 3) })
	case EvActivity:
		_, _ = m.mutate(id, func(s *Session) { s.Activity = truncate(e.Text, 160) })
	case EvWorking:
		_, _ = m.mutate(id, func(s *Session) {
			if s.Status != StatusNeedsYou || len(s.Pending) == 0 {
				s.Status = StatusWorking
			}
			if e.Text != "" {
				s.Activity = truncate(e.Text, 160)
			}
		})
	case EvTranscript:
		_, _ = m.mutate(id, func(s *Session) { s.TranscriptRef = e.Text })
	case EvPermission:
		var req PermissionRequest
		if json.Unmarshal(e.Data, &req) != nil || req.ID == "" {
			return
		}
		_, _ = m.mutate(id, func(s *Session) {
			s.Pending = append(s.Pending, req)
			s.Status, s.Activity = StatusNeedsYou, "Waiting for approval: "+req.Tool+" "+truncate(req.Summary, 100)
		})
	case EvPermCancel:
		pid := str("id")
		_, _ = m.mutate(id, func(s *Session) {
			rest := s.Pending[:0:0]
			for _, p := range s.Pending {
				if p.ID != pid {
					rest = append(rest, p)
				}
			}
			s.Pending = rest
			if len(rest) == 0 && s.Status == StatusNeedsYou {
				s.Status, s.Activity = StatusWorking, "Working"
			}
		})
	case EvNeedsInput:
		_, _ = m.mutate(id, func(s *Session) {
			s.Status, s.Activity = StatusNeedsYou, truncate(e.Text, 160)
		})
	case EvTurnDone:
		ok := true
		_ = json.Unmarshal(data["ok"], &ok)
		result := str("result")
		if result == "" {
			result = e.Text
		}
		s, err := m.mutate(id, func(s *Session) {
			s.Turns++
			s.Pending = []PermissionRequest{}
			s.Result = truncate(result, 32<<10)
			if result != "" && s.LastMessage == "" {
				s.LastMessage = truncate(result, 32<<10)
			}
			if s.Review == ReviewSentBack {
				s.Review = ReviewNone
			}
			if ok {
				s.Status, s.Activity, s.Error = StatusDone, "", ""
				if s.Tail == "" {
					s.Tail = lastLines(result, 3)
				}
			} else {
				s.Status, s.Activity, s.Error = StatusFailed, "", truncate(result, 2000)
			}
		})
		if err == nil {
			if m.cfg.Finished != nil {
				m.cfg.Finished(s)
			}
			m.background(func() { m.refreshDiffStat(s.ID) })
		}
	}
}

func wakeIdle(s *Session) {
	if s.Status == StatusIdle || s.Status == StatusDone {
		s.Status = StatusWorking
	}
}

// finalize records the process exit and removes the session from the live set.
func (m *Manager) finalize(ls *liveSess, e Event) {
	id := ls.id
	var d struct {
		Code      int    `json:"code"`
		Stopped   bool   `json:"stopped"`
		Stderr    string `json:"stderr"`
		Uncertain bool   `json:"uncertain"`
	}
	_ = json.Unmarshal(e.Data, &d)
	e.Text = fmt.Sprintf("exit code %d", d.Code)
	if d.Uncertain {
		e.Text = "SSH process control lost; remote exit is unconfirmed"
	}
	m.record(id, e)

	m.mu.Lock()
	stopping, shutdown := ls.stopping || d.Stopped, ls.shutdown
	completedTurn := ls.s.Status == StatusDone || ls.s.Status == StatusFailed
	s, err := m.mutateLocked(id, func(s *Session) {
		now := time.Now().UTC()
		code := d.Code
		s.ExitCode, s.EndedAt, s.Pending, s.Activity = &code, &now, []PermissionRequest{}, ""
		switch {
		case d.Uncertain:
			s.Status, s.Error, s.ExitCode = StatusLost, d.Stderr, nil
		case s.Status == StatusDone || s.Status == StatusFailed:
			// A finished turn stays finished; the process exiting afterwards changes nothing.
		case stopping:
			s.Status = StatusStopped
			if shutdown {
				s.Error = "The daemon shut down; this agent was stopped with it. Resume it to continue."
			}
		case d.Code == 0:
			s.Status = StatusDone
			if s.Result == "" {
				s.Result = s.LastMessage
			}
		default:
			s.Status = StatusFailed
			s.Error = fmt.Sprintf("The agent process exited with code %d.", d.Code)
			if d.Stderr != "" {
				s.Error += " " + truncate(d.Stderr, 1500)
			}
		}
		if s.Status == StatusDone && s.Turns == 0 {
			s.Turns = 1
		}
	})
	delete(m.live, id)
	if err == nil {
		s.Live = false
		close(m.changed)
		m.changed = make(chan struct{})
	}
	m.mu.Unlock()
	if err != nil {
		m.cfg.Log.Warn("finalize failed", "session", id, "err", err)
		return
	}
	m.cfg.Publish("agents.updated", s)
	if !completedTurn && m.cfg.Finished != nil && (s.Status == StatusDone || s.Status == StatusFailed) {
		m.cfg.Finished(s)
	}
	m.background(func() { m.refreshDiffStat(id) })
}

// refreshDiffStat updates the +/- counts shown in Review.
func (m *Manager) refreshDiffStat(id string) {
	m.worktreeMu.RLock()
	defer m.worktreeMu.RUnlock()
	if m.cfg.Worktrees == nil {
		return
	}
	s, err := m.Get(id)
	if err != nil || !localMachine(s.Machine) || s.BaseRef == "" {
		return
	}
	ctx, cancel := context.WithTimeout(m.bg, 60*time.Second)
	defer cancel()
	d, err := m.cfg.Worktrees.Diff(ctx, s.Dir, s.BaseRef)
	if err != nil {
		return
	}
	_, _ = m.mutate(id, func(s *Session) { s.Files, s.Additions, s.Deletions = len(d.Files), d.Additions, d.Deletions })
}

// background starts tracked work only while the manager is accepting it.
func (m *Manager) background(f func()) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return false
	}
	m.bgWork.Add(1)
	go func() { defer m.bgWork.Done(); f() }()
	return true
}

// Diff returns the work a session produced relative to its base commit.
func (m *Manager) Diff(ctx context.Context, id string) (ReviewDiff, error) {
	m.worktreeMu.RLock()
	defer m.worktreeMu.RUnlock()
	s, err := m.Get(id)
	if err != nil {
		return ReviewDiff{}, err
	}
	if !localMachine(s.Machine) {
		return ReviewDiff{}, errRemoteOperation
	}
	out := ReviewDiff{SessionID: id, BaseRef: s.BaseRef, Dir: s.Dir}
	if m.cfg.Worktrees == nil {
		out.Error = "git is unavailable"
		return out, nil
	}
	if s.BaseRef == "" {
		out.Error = "this session's directory is not a git work tree, so there is nothing to diff"
		return out, nil
	}
	d, err := m.cfg.Worktrees.Diff(ctx, s.Dir, s.BaseRef)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	out.Diff, out.Files, out.Additions, out.Deletions, out.Truncated = d, len(d.Files), d.Additions, d.Deletions, d.Truncated
	if d.Files == nil {
		out.Diff.Files = []DiffFile{}
	}
	_, _ = m.mutate(id, func(s *Session) { s.Files, s.Additions, s.Deletions = out.Files, out.Additions, out.Deletions })
	return out, nil
}

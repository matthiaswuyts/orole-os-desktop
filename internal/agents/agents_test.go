package agents_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/terminals"
	"github.com/rayorole/orole-os-desktop/migrations"
)

var fakeBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakeagent")
	if err != nil {
		panic(err)
	}
	name := "fakeagent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fakeBin = filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", fakeBin, "./testdata/fakeagent")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building fake agent: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// ---- harness ----

type recorder struct {
	mu      sync.Mutex
	audits  []string
	updates int
}

func (r *recorder) audit(action, target, outcome, detail string) {
	r.mu.Lock()
	r.audits = append(r.audits, action+" "+outcome+" "+detail)
	r.mu.Unlock()
}

func (r *recorder) has(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.audits {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

type env struct {
	m    *agents.Manager
	st   *agents.Store
	db   *database.DB
	rec  *recorder
	data string
	wt   agents.Worktrees
	term *terminals.Manager
	pubs *pubLog
}

type pubLog struct {
	mu     sync.Mutex
	events []string
}

func (p *pubLog) publish(event string, _ any) {
	p.mu.Lock()
	p.events = append(p.events, event)
	p.mu.Unlock()
}

func (p *pubLog) count(event string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, e := range p.events {
		if e == event {
			n++
		}
	}
	return n
}

type opt func(*envCfg)
type envCfg struct {
	wt                 agents.Worktrees
	hookExe            string
	adapters           []agents.Adapter
	dbPath             string
	data               string
	permissionDecision func(context.Context, string, agents.PermissionRequest) error
	authorize          func(context.Context, string, string, string) error
}

func withWT(w agents.Worktrees) opt { return func(c *envCfg) { c.wt = w } }
func withHook(exe string) opt       { return func(c *envCfg) { c.hookExe = exe } }
func sameDB(path, data string) opt  { return func(c *envCfg) { c.dbPath, c.data = path, data } }

func newEnv(t *testing.T, opts ...opt) *env {
	t.Helper()
	c := &envCfg{authorize: func(context.Context, string, string, string) error { return nil }}
	for _, o := range opts {
		o(c)
	}
	if c.data == "" {
		c.data = t.TempDir()
		c.dbPath = filepath.Join(c.data, "orole.db")
	}
	db, err := database.Open(c.dbPath, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{db: db, st: agents.NewStore(db), rec: &recorder{}, data: c.data, wt: c.wt, pubs: &pubLog{}}
	e.term = terminals.NewManager(terminals.NewBackend(), nil)
	e.m = agents.NewManager(agents.Config{
		Store: e.st, Host: &agents.Host{Terminals: e.term, DataDir: c.data, HookExe: c.hookExe},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Worktrees: c.wt,
		Publish: e.pubs.publish, Audit: e.rec.audit,
		PermissionDecision: c.permissionDecision,
		Authorize:          c.authorize,
		Adapters:           []agents.Adapter{agents.Claude{Bin: fakeBin}, agents.Custom{}},
	})
	t.Cleanup(func() { e.m.Shutdown(); e.term.Shutdown() })
	return e
}

func ctx10(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func (e *env) wait(t *testing.T, id string, what string, pred func(agents.Session) bool) agents.Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := e.m.WaitFor(ctx, id, pred)
	if err != nil {
		t.Fatalf("waiting for %s: %v (last status=%s activity=%q error=%q)", what, err, s.Status, s.Activity, s.Error)
	}
	return s
}

func status(want agents.Status) func(agents.Session) bool {
	return func(s agents.Session) bool { return s.Status == want }
}

func (e *env) start(t *testing.T, spec agents.StartSpec) agents.Session {
	t.Helper()
	if spec.Kind == "" {
		spec.Kind = "claude"
	}
	if spec.Dir == "" {
		spec.Dir = t.TempDir()
	}
	s, err := e.m.Start(ctx10(t), spec)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

func kinds(evs []agents.Event) map[string]int {
	out := map[string]int{}
	for _, e := range evs {
		out[e.Kind]++
	}
	return out
}

// ---- lifecycle ----

func TestLifecycleStartPermissionApproveDone(t *testing.T) {
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Prompt: "NEEDPERM please"})
	if s.Mode != agents.ModeStructured || !s.Live || s.Pid == 0 {
		t.Fatalf("unexpected start state: %+v", s)
	}
	if s.Status != agents.StatusWorking && s.Status != agents.StatusNeedsYou {
		t.Fatalf("status after start = %s", s.Status)
	}
	s = e.wait(t, s.ID, "needs-you", status(agents.StatusNeedsYou))
	if len(s.Pending) != 1 || s.Pending[0].Tool != "Bash" || s.Pending[0].Summary != "echo hi" || s.Pending[0].ID != "req-1" {
		t.Fatalf("pending = %+v", s.Pending)
	}
	if err := e.m.Prompt(ctx10(t), s.ID, "meanwhile"); err == nil {
		t.Fatal("prompting while a permission is pending must be refused")
	}
	if err := e.m.Approve(ctx10(t), s.ID, "nope", true, ""); err == nil {
		t.Fatal("approving an unknown request must fail")
	}
	if err := e.m.Approve(ctx10(t), s.ID, "req-1", true, ""); err != nil {
		t.Fatal(err)
	}
	s = e.wait(t, s.ID, "done", status(agents.StatusDone))
	if !strings.Contains(s.LastMessage, `ran: echo hi input={"command":"echo hi"}`) {
		t.Fatalf("agent did not receive the allow decision with the original input: %q", s.LastMessage)
	}
	if s.Turns != 1 || len(s.Pending) != 0 {
		t.Fatalf("turns=%d pending=%d", s.Turns, len(s.Pending))
	}
	tr, err := e.m.Transcript(s.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	k := kinds(tr)
	for _, want := range []string{agents.EvPrompt, agents.EvTool, agents.EvPermission, agents.EvDecision, agents.EvMessage, agents.EvTurnDone} {
		if k[want] == 0 {
			t.Errorf("transcript missing %s: %v", want, k)
		}
	}
	if !e.rec.has("agent.permission allowed Bash: echo hi") || !e.rec.has("agent.start allowed") {
		t.Fatalf("decisions not audited: %v", e.rec.audits)
	}
	if e.pubs.count("agents.updated") < 3 || e.pubs.count("agents.event") < 3 {
		t.Fatalf("expected live updates/events, got %v", e.pubs.events)
	}
}

func TestPermissionDenyIsRelayedAndAudited(t *testing.T) {
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Prompt: "NEEDPERM"})
	e.wait(t, s.ID, "needs-you", status(agents.StatusNeedsYou))
	if err := e.m.Approve(ctx10(t), s.ID, "req-1", false, "not on my machine"); err != nil {
		t.Fatal(err)
	}
	s = e.wait(t, s.ID, "done", status(agents.StatusDone))
	if !strings.Contains(s.LastMessage, "denied: not on my machine") {
		t.Fatalf("deny reason not delivered: %q", s.LastMessage)
	}
	if !e.rec.has("agent.permission denied Bash") {
		t.Fatalf("deny not audited: %v", e.rec.audits)
	}
}

func TestStopWhileWaitingMarksStopped(t *testing.T) {
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Prompt: "NEEDPERM"})
	s = e.wait(t, s.ID, "needs-you", status(agents.StatusNeedsYou))
	pid := s.Pid
	got, err := e.m.Stop(ctx10(t), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != agents.StatusStopped || got.Live || got.EndedAt == nil || len(got.Pending) != 0 {
		t.Fatalf("after stop: %+v", got)
	}
	if pidAlive(pid) {
		t.Fatalf("process %d still alive after stop", pid)
	}
	if _, err := e.m.Stop(ctx10(t), s.ID); err == nil {
		t.Fatal("stopping a stopped agent must report it is not running")
	}
	if !e.rec.has("agent.stop allowed") {
		t.Fatal("stop not audited")
	}
}

func TestResumeUsesCLISessionID(t *testing.T) {
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Prompt: "first"})
	s = e.wait(t, s.ID, "done", status(agents.StatusDone))
	if !strings.Contains(s.LastMessage, "echo(fresh session="+s.AgentSessionID+")") || s.AgentSessionID == "" {
		t.Fatalf("first turn: %q (agent session %q)", s.LastMessage, s.AgentSessionID)
	}
	agentSID := s.AgentSessionID
	if _, err := e.m.Stop(ctx10(t), s.ID); err != nil {
		t.Fatal(err)
	}
	if cur, _ := e.m.Get(s.ID); cur.Live {
		t.Fatal("still live after stop")
	}
	// Replying to an ended session resumes the CLI session.
	if err := e.m.Prompt(ctx10(t), s.ID, "second"); err != nil {
		t.Fatal(err)
	}
	s = e.wait(t, s.ID, "second turn", func(s agents.Session) bool { return s.Turns >= 2 && s.Status == agents.StatusDone })
	if !strings.Contains(s.LastMessage, "echo(resume session="+agentSID+"): second") {
		t.Fatalf("resume did not pass --resume <id>: %q", s.LastMessage)
	}
	if s.AgentSessionID != agentSID {
		t.Fatalf("agent session id changed on resume: %q -> %q", agentSID, s.AgentSessionID)
	}
	tr, _ := e.m.Transcript(s.ID, 100)
	found := false
	for _, ev := range tr {
		found = found || (ev.Kind == agents.EvSystem && strings.Contains(ev.Text, "Resumed the CLI session "+agentSID))
	}
	if !found {
		t.Fatal("resume not recorded in transcript")
	}

	// Explicit fresh restart starts over from the task with a new CLI session.
	got, err := e.m.Restart(ctx10(t), s.ID, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID == agentSID || got.AgentSessionID == "" {
		t.Fatalf("fresh restart kept the old CLI session: %q", got.AgentSessionID)
	}
	s = e.wait(t, s.ID, "fresh turn", func(s agents.Session) bool {
		return s.Status == agents.StatusDone && strings.Contains(s.LastMessage, "echo(fresh") && s.Turns >= 1
	})
	if !strings.Contains(s.LastMessage, ": first") {
		t.Fatalf("fresh restart should re-run the original task: %q", s.LastMessage)
	}
}

func TestFailedTurnAndCrash(t *testing.T) {
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Prompt: "FAIL now"})
	s = e.wait(t, s.ID, "failed", status(agents.StatusFailed))
	if s.Error != "boom" || s.Turns != 1 {
		t.Fatalf("failed turn: error=%q turns=%d", s.Error, s.Turns)
	}
	// The process is still alive after a failed turn; a follow-up works and recovers the session.
	if err := e.m.Prompt(ctx10(t), s.ID, "try again"); err != nil {
		t.Fatal(err)
	}
	s = e.wait(t, s.ID, "done again", status(agents.StatusDone))
	if s.Error != "" {
		t.Fatalf("error not cleared: %q", s.Error)
	}

	c := e.start(t, agents.StartSpec{Prompt: "CRASH"})
	c = e.wait(t, c.ID, "crash failed", status(agents.StatusFailed))
	if !strings.Contains(c.Error, "code 3") || !strings.Contains(c.Error, "crashing on purpose") || c.ExitCode == nil || *c.ExitCode != 3 {
		t.Fatalf("crash not reported honestly: %+v", c)
	}
}

func TestStartValidation(t *testing.T) {
	e := newEnv(t)
	ctx := ctx10(t)
	for name, spec := range map[string]agents.StartSpec{
		"unknown kind":    {Kind: "nope", Dir: t.TempDir()},
		"missing dir":     {Kind: "claude", Dir: filepath.Join(t.TempDir(), "gone")},
		"no dir":          {Kind: "claude"},
		"bad mode":        {Kind: "claude", Dir: t.TempDir(), Mode: "weird"},
		"bad permission":  {Kind: "claude", Dir: t.TempDir(), PermissionMode: "bypassPermissions"},
		"custom no cmd":   {Kind: "custom", Dir: t.TempDir()},
		"worktree no git": {Kind: "claude", Dir: t.TempDir(), NewWorktree: true},
		"bad model":       {Kind: "claude", Dir: t.TempDir(), Model: "model\ninjected"},
		"long model":      {Kind: "claude", Dir: t.TempDir(), Model: strings.Repeat("m", 129)},
		"bad reasoning":   {Kind: "claude", Dir: t.TempDir(), Reasoning: "invented"},
	} {
		if _, err := e.m.Start(ctx, spec); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDetect(t *testing.T) {
	e := newEnv(t)
	ds := e.m.Detect(ctx10(t), true)
	if len(ds) != 2 || ds[0].Kind != "claude" || !ds[0].Installed || !strings.Contains(ds[0].Version, "FakeAgent") || !ds[0].Resume || !ds[0].Permissions {
		t.Fatalf("detections: %+v", ds)
	}
	m := agents.NewManager(agents.Config{Store: e.st, Adapters: []agents.Adapter{agents.Claude{Bin: "definitely-not-installed-xyz"}, agents.Codex()}})
	d := m.Detect(ctx10(t), true)[0]
	if d.Installed || !strings.Contains(d.Error, "not found") {
		t.Fatalf("missing CLI must be reported honestly: %+v", d)
	}
}

// ---- recovery ----

func TestRecoveryMarksOrphansLostAndNeverRedispatches(t *testing.T) {
	data := t.TempDir()
	dbPath := filepath.Join(data, "orole.db")
	dir := t.TempDir()
	{
		// Previous daemon run: a working session, a done one, a pending journal row, a running run.
		e := newEnv(t, sameDB(dbPath, data))
		now := time.Now().UTC()
		for id, st := range map[string]agents.Status{"orphan": agents.StatusWorking, "idle1": agents.StatusIdle, "waiting": agents.StatusNeedsYou, "finished": agents.StatusDone} {
			if err := e.st.SaveSession(agents.Session{ID: id, Kind: "claude", Status: st, Dir: dir, AgentSessionID: "cli-" + id, Task: "t",
				StartedAt: now, UpdatedAt: now, Mode: agents.ModeStructured}, ""); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := e.st.JournalBegin("dispatch-1", "send-prompt", map[string]string{"session": "orphan", "text": "go"}, "previous-boot"); err != nil {
			t.Fatal(err)
		}
		if err := e.st.SaveRun(agents.Run{ID: "run-1", Kind: "loop-until", Status: agents.RunRunning}); err != nil {
			t.Fatal(err)
		}
	}
	e := newEnv(t, sameDB(dbPath, data))
	lost, err := e.m.Recover()
	if err != nil || lost != 3 {
		t.Fatalf("recover: lost=%d err=%v", lost, err)
	}
	for _, id := range []string{"orphan", "idle1", "waiting"} {
		s, err := e.m.Get(id)
		if err != nil || s.Status != agents.StatusLost || s.Live || s.EndedAt == nil || !strings.Contains(s.Error, "daemon restarted") || !strings.Contains(s.Error, "resumed") {
			t.Fatalf("%s: %+v err=%v", id, s, err)
		}
		evs, _ := e.m.Events(id, 0, 50)
		if len(evs) != 1 || evs[0].Kind != agents.EvSystem || !strings.HasPrefix(evs[0].Text, "Marked lost") {
			t.Fatalf("%s: lost not recorded: %+v", id, evs)
		}
	}
	if s, _ := e.m.Get("finished"); s.Status != agents.StatusDone {
		t.Fatalf("finished work must stay done, got %s", s.Status)
	}
	if j, _ := e.st.Journal("dispatch-1"); j.Status != agents.JournalInterrupted {
		t.Fatalf("journal row = %+v", j)
	}
	runs, _ := e.st.ListRuns(10)
	if len(runs) != 1 || runs[0].Status != agents.RunInterrupted {
		t.Fatalf("runs = %+v", runs)
	}
	if lost2, _ := e.m.Recover(); lost2 != 0 {
		t.Fatalf("recovery is not idempotent: %d", lost2)
	}

	// A lost Claude session resumes through its CLI session id.
	if err := e.m.Prompt(ctx10(t), "orphan", "carry on"); err != nil {
		t.Fatal(err)
	}
	s := e.wait(t, "orphan", "resumed turn", func(s agents.Session) bool { return s.Turns >= 1 && s.Status == agents.StatusDone })
	if !strings.Contains(s.LastMessage, "echo(resume session=cli-orphan): carry on") {
		t.Fatalf("lost session was not resumed by id: %q", s.LastMessage)
	}
	if s.Error != "" || s.EndedAt != nil && s.Live {
		t.Fatalf("resumed session still carries lost state: %+v", s)
	}
}

// ---- terminal mode ----

func requireConPTY(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("terminal-mode agents need the ConPTY backend (Windows)")
	}
}

func attachText(e *env, id string) string {
	a, err := e.term.Attach(id)
	if err != nil {
		return ""
	}
	return string(a.Data)
}

func waitText(t *testing.T, e *env, id, substr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(attachText(e, id), substr) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("terminal never showed %q; screen: %q", substr, attachText(e, id))
}

func TestCustomCLIRunsUnderConPTY(t *testing.T) {
	requireConPTY(t)
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Kind: "custom", Command: `"` + fakeBin + `"`, Prompt: "the task"})
	if s.Mode != agents.ModeTerminal || s.TerminalID == "" || s.Status != agents.StatusWorking || !s.Live {
		t.Fatalf("start: %+v", s)
	}
	var owner string
	for _, in := range e.term.List() {
		if in.ID == s.TerminalID {
			owner = in.Owner
		}
	}
	if owner != "agent:"+s.ID {
		t.Fatalf("terminal owner = %q", owner)
	}
	if _, err := os.Stat(filepath.Join(e.data, "agents", s.ID, "prompt.md")); err != nil {
		t.Fatalf("prompt file missing: %v", err)
	}
	waitText(t, e, s.TerminalID, "fake terminal ready")
	if err := e.m.Prompt(ctx10(t), s.ID, "must not type into the foreground tool"); err == nil {
		t.Fatal("programmatic PTY prompt must be refused")
	}
	if err := e.term.Write(s.TerminalID, []byte("hello there\r")); err != nil {
		t.Fatal(err)
	}
	waitText(t, e, s.TerminalID, "got: hello there")
	if err := e.term.Write(s.TerminalID, []byte("exit\r")); err != nil {
		t.Fatal(err)
	}
	s = e.wait(t, s.ID, "done after clean exit", status(agents.StatusDone))
	if s.Live || s.ExitCode == nil || *s.ExitCode != 0 {
		t.Fatalf("exit: %+v", s)
	}

	c := e.start(t, agents.StartSpec{Kind: "custom", Command: `"` + fakeBin + `"`})
	if c.Status != agents.StatusIdle {
		t.Fatalf("no prompt should start idle, got %s", c.Status)
	}
	waitText(t, e, c.TerminalID, "fake terminal ready")
	if err := e.term.Write(c.TerminalID, []byte("crash\r")); err != nil {
		t.Fatal(err)
	}
	c = e.wait(t, c.ID, "failed after crash", status(agents.StatusFailed))
	if !strings.Contains(c.Error, "code 3") {
		t.Fatalf("error = %q", c.Error)
	}
}

func TestStopKillsTerminalAgentTree(t *testing.T) {
	requireConPTY(t)
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Kind: "custom", Command: `"` + fakeBin + `"`})
	waitText(t, e, s.TerminalID, "fake terminal ready")
	got, err := e.m.Stop(ctx10(t), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != agents.StatusStopped || got.Live {
		t.Fatalf("after stop: %+v", got)
	}
	if pidAlive(s.Pid) {
		t.Fatalf("terminal agent process %d survived stop", s.Pid)
	}
}

func TestClaudeTerminalModeStatusComesFromHooks(t *testing.T) {
	requireConPTY(t)
	hookExe := filepath.Join(t.TempDir(), "orole-daemon.exe")
	e := newEnv(t, withHook(hookExe))
	s := e.start(t, agents.StartSpec{Kind: "claude", Mode: agents.ModeTerminal, Prompt: "do the thing"})
	if s.Mode != agents.ModeTerminal || s.TerminalID == "" || s.Status != agents.StatusWorking {
		t.Fatalf("start: %+v", s)
	}
	settings, err := os.ReadFile(filepath.Join(e.data, "agents", s.ID, "claude-settings.json"))
	if err != nil {
		t.Fatalf("hook settings not written: %v", err)
	}
	if !strings.Contains(string(settings), "agent-hook "+s.ID+" Stop") || !strings.Contains(string(settings), filepath.ToSlash(hookExe)) {
		t.Fatalf("settings: %s", settings)
	}
	waitText(t, e, s.TerminalID, "fake terminal ready")

	hook := func(event string, payload any) {
		t.Helper()
		b, _ := json.Marshal(payload)
		if err := e.m.Hook(s.ID, event, b); err != nil {
			t.Fatalf("hook %s: %v", event, err)
		}
	}
	cur := func() agents.Session { c, _ := e.m.Get(s.ID); return c }

	hook("SessionStart", map[string]any{"session_id": "cli-xyz", "transcript_path": "C:/t/x.jsonl"})
	if c := cur(); c.AgentSessionID != "cli-xyz" || c.TranscriptRef != "C:/t/x.jsonl" {
		t.Fatalf("SessionStart: %+v", c)
	}
	hook("UserPromptSubmit", map[string]any{"prompt": "do the thing"})
	hook("PreToolUse", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "ls -la"}})
	if c := cur(); c.Status != agents.StatusWorking || c.Activity != "Bash: ls -la" {
		t.Fatalf("PreToolUse: status=%s activity=%q", c.Status, c.Activity)
	}
	hook("Notification", map[string]any{"notification_type": "permission_prompt", "message": "Claude needs your permission to use Bash"})
	if c := cur(); c.Status != agents.StatusNeedsYou || !strings.Contains(c.Activity, "permission") {
		t.Fatalf("permission_prompt: status=%s activity=%q", c.Status, c.Activity)
	}
	if err := e.m.Approve(ctx10(t), s.ID, "x", true, ""); err == nil {
		t.Fatal("terminal sessions cannot relay approvals; must not pretend to")
	}
	hook("PostToolUse", map[string]any{"tool_name": "Bash"})
	if c := cur(); c.Status != agents.StatusWorking {
		t.Fatalf("PostToolUse should resume working, got %s", c.Status)
	}
	hook("Notification", map[string]any{"notification_type": "idle_prompt", "message": "waiting"})
	if c := cur(); c.Status != agents.StatusWorking {
		t.Fatalf("idle_prompt must not change status, got %s", c.Status)
	}
	hook("Stop", map[string]any{"last_assistant_message": "All done, tests pass."})
	c := cur()
	if c.Status != agents.StatusDone || c.LastMessage != "All done, tests pass." || c.Turns != 1 || !c.Live {
		t.Fatalf("Stop: %+v", c)
	}
	if err := e.m.Hook(s.ID, "Bogus", nil); err == nil {
		t.Fatal("unknown hook event must be rejected")
	}
	tr, _ := e.m.Transcript(s.ID, 100)
	if n := kinds(tr)[agents.EvPrompt]; n != 1 {
		t.Fatalf("the initial prompt must be recorded once, got %d", n)
	}
	if _, err := e.m.Stop(ctx10(t), s.ID); err != nil {
		t.Fatal(err)
	}
	if got := cur(); got.Status != agents.StatusDone || got.Live {
		t.Fatalf("a finished session stays done when its process is stopped: %+v", got)
	}
	// Structured sessions do not take hooks.
	st := e.start(t, agents.StartSpec{Prompt: "x"})
	if err := e.m.Hook(st.ID, "Stop", nil); err == nil {
		t.Fatal("hook accepted for a structured session")
	}
}

func TestHookSettingsJSON(t *testing.T) {
	b, err := agents.HookSettings(`C:\Program Files\Orole\orole-daemon.exe`, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, b)
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop", "SessionEnd"} {
		h := v.Hooks[ev]
		want := `"C:/Program Files/Orole/orole-daemon.exe" agent-hook sess-1 ` + ev
		if len(h) != 1 || len(h[0].Hooks) != 1 || h[0].Hooks[0].Type != "command" || h[0].Hooks[0].Command != want || h[0].Hooks[0].Timeout == 0 {
			t.Errorf("%s: %+v (want command %s)", ev, h, want)
		}
	}
}

func TestPolicyDenialCannotBeOverriddenByUserAllow(t *testing.T) {
	blocked := errors.New("Bash is blocked by policy")
	e := newEnv(t, func(c *envCfg) {
		c.permissionDecision = func(_ context.Context, _ string, r agents.PermissionRequest) error {
			if r.Tool != "Bash" {
				t.Errorf("policy request tool %q", r.Tool)
			}
			return blocked
		}
	})
	s := e.start(t, agents.StartSpec{Prompt: "NEEDPERM"})
	e.wait(t, s.ID, "permission", status(agents.StatusNeedsYou))
	if err := e.m.Approve(ctx10(t), s.ID, "req-1", true, ""); !errors.Is(err, blocked) {
		t.Fatalf("policy denial was not returned: %v", err)
	}
	s = e.wait(t, s.ID, "done after policy denial", status(agents.StatusDone))
	if !strings.Contains(s.LastMessage, "denied: Bash is blocked by policy") || len(s.Pending) != 0 {
		t.Fatalf("CLI did not receive denial: %+v", s)
	}
	if !e.rec.has("agent.permission denied Bash") {
		t.Fatal("policy denial not audited")
	}
}

func TestExecutionAuthorizationDenialHasNoSideEffects(t *testing.T) {
	denied := errors.New("policy denied")
	e := newEnv(t, func(c *envCfg) {
		c.authorize = func(context.Context, string, string, string) error { return denied }
	})
	if _, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: t.TempDir(), Prompt: "hello"}); !errors.Is(err, denied) {
		t.Fatalf("start must honor policy denial: %v", err)
	}
	sessions, err := e.m.List()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("denied launch created session state: %v %v", sessions, err)
	}
	dir := t.TempDir()
	if _, _, err := e.m.RunCheckCommand(ctx10(t), dir, "echo forbidden", time.Second); !errors.Is(err, denied) {
		t.Fatalf("orchestration check bypassed policy: %v", err)
	}
}

func TestMissingExecutionAuthorizerFailsClosed(t *testing.T) {
	e := newEnv(t, func(c *envCfg) { c.authorize = nil })
	if _, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: t.TempDir()}); err == nil {
		t.Fatal("missing authorizer permitted an agent process")
	}
}

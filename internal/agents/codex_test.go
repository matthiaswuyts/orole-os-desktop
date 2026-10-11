package agents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/terminals"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// buildFakeCodex compiles testdata/fakecodex, the scripted `codex app-server` stand-in.
func buildFakeCodex(t *testing.T) string {
	t.Helper()
	name := "fakecodex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("go", "build", "-o", bin, "./testdata/fakecodex").CombinedOutput(); err != nil {
		t.Fatalf("building fake codex: %v\n%s", err, out)
	}
	return bin
}

type cxLine struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// cxRequests returns what the fake received, in order.
func cxRequests(t *testing.T, log string) []cxLine {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var out []cxLine
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r cxLine
		if json.Unmarshal([]byte(l), &r) == nil && r.Method != "" {
			out = append(out, r)
		}
	}
	return out
}

func cxMethods(rs []cxLine) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Method)
	}
	return out
}

func cxFind(t *testing.T, rs []cxLine, method string) cxLine {
	t.Helper()
	for _, r := range rs {
		if r.Method == method {
			return r
		}
	}
	t.Fatalf("no %s request in %v", method, cxMethods(rs))
	return cxLine{}
}

func newCodexEnv(t *testing.T, fake string) (*env, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "requests.jsonl")
	t.Setenv("FAKECODEX_LOG", log)
	data := t.TempDir()
	db, err := database.Open(filepath.Join(data, "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{db: db, st: agents.NewStore(db), rec: &recorder{}, data: data, pubs: &pubLog{}}
	e.term = terminals.NewManager(terminals.NewBackend(), nil)
	e.m = agents.NewManager(agents.Config{
		Store: e.st, Host: &agents.Host{Terminals: e.term, DataDir: data},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Publish:   e.pubs.publish,
		Audit:     e.rec.audit,
		Authorize: func(context.Context, string, string, string) error { return nil },
		Adapters:  []agents.Adapter{agents.CodexApp{Bin: fake}},
	})
	t.Cleanup(func() { e.m.Shutdown(); e.term.Shutdown() })
	return e, log
}

func (e *env) events(t *testing.T, id string) []agents.Event {
	t.Helper()
	evs, err := e.m.Events(id, 0, 2000)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func cxOfKind(evs []agents.Event, kind string) []agents.Event {
	var out []agents.Event
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func cxHasPending(s agents.Session) bool { return len(s.Pending) > 0 }

func cxData[T any](t *testing.T, e agents.Event) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		t.Fatalf("decoding %s data %s: %v", e.Kind, e.Data, err)
	}
	return v
}

func TestCodexAppServer(t *testing.T) {
	fake := buildFakeCodex(t)

	t.Run("full turn with approvals, tools, image and artifact", func(t *testing.T) {
		e, log := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "TOOLS please", Reasoning: "high"})
		if s.Mode != agents.ModeStructured {
			t.Fatalf("codex must default to structured mode: %+v", s)
		}
		s = e.wait(t, s.ID, "command approval", cxHasPending)
		req := s.Pending[0]
		if req.Tool != "shell" || !strings.Contains(req.Summary, "git status") || !strings.Contains(req.Summary, "needs to inspect the tree") ||
			!strings.Contains(req.InputJSON, `"command":"git status"`) || s.Status != agents.StatusNeedsYou {
			t.Fatalf("command approval must show the command faithfully: %+v", req)
		}
		if err := e.m.Approve(ctx10(t), s.ID, req.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		s = e.wait(t, s.ID, "file approval", cxHasPending)
		req = s.Pending[0]
		if req.Tool != "apply_patch" || !strings.Contains(req.Summary, `a.txt`) || !strings.Contains(req.Summary, `b.txt`) ||
			!strings.Contains(req.InputJSON, `"diff":"hi\n"`) || !strings.Contains(req.InputJSON, `"kind":"update"`) {
			t.Fatalf("file approval must list the changed paths and diffs: %+v", req)
		}
		if err := e.m.Approve(ctx10(t), s.ID, req.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		s = e.wait(t, s.ID, "turn done", status(agents.StatusDone))
		if s.AgentSessionID != "thr-new" || s.Model != "fake-model" || s.LastMessage != "command=accept files=accept" || s.Turns != 1 {
			t.Fatalf("session state: %+v", s)
		}

		evs := e.events(t, s.ID)
		k := kinds(evs)
		for _, want := range []string{agents.EvInit, agents.EvWorking, agents.EvReasoning, agents.EvTool, agents.EvToolResult,
			agents.EvPermission, agents.EvDecision, agents.EvImage, agents.EvMessage, agents.EvTurnDone} {
			if k[want] == 0 {
				t.Errorf("missing %s event: %v", want, k)
			}
		}
		if r := cxOfKind(evs, agents.EvReasoning); len(r) != 1 || r[0].Text != "**Planning the edit**\n\nraw thought" {
			t.Errorf("reasoning: %+v", r)
		}
		tools := map[string]agents.ToolData{}
		for _, ev := range cxOfKind(evs, agents.EvTool) {
			td := cxData[agents.ToolData](t, ev)
			if td.Status != "running" {
				t.Errorf("tool call %s status %q", td.ID, td.Status)
			}
			tools[td.ID] = td
		}
		if shell := tools["cmd-1"]; shell.Name != "shell" || !strings.Contains(string(shell.Input), "git status") {
			t.Errorf("shell tool: %+v", shell)
		}
		if fc := tools["fc-1"]; fc.Name != "apply_patch" || !strings.Contains(string(fc.Input), "a.txt") || !strings.Contains(string(fc.Input), "b.txt") {
			t.Errorf("apply_patch tool: %+v", fc)
		}
		if mcp := tools["mcp-1"]; mcp.Name != "ping" || mcp.Server != "orole" {
			t.Errorf("mcp tool: %+v", mcp)
		}
		results := map[string]agents.ToolData{}
		for _, ev := range cxOfKind(evs, agents.EvToolResult) {
			td := cxData[agents.ToolData](t, ev)
			results[td.ID] = td
		}
		if r := results["cmd-1"]; r.Status != "ok" || r.Output != "On branch main\n" {
			t.Errorf("shell result: %+v", r)
		}
		if r := results["fc-1"]; r.Status != "ok" {
			t.Errorf("apply_patch result: %+v", r)
		}
		if r := results["mcp-1"]; r.Status != "ok" || r.Output != "pong" || r.Server != "orole" {
			t.Errorf("mcp result: %+v", r)
		}
		if done := cxOfKind(evs, agents.EvTurnDone); len(done) != 1 || !strings.Contains(string(done[0].Data), `"ok":true`) {
			t.Errorf("turn done: %+v", done)
		}

		imgs := cxOfKind(evs, agents.EvImage)
		if len(imgs) != 1 {
			t.Fatalf("image events: %+v", imgs)
		}
		img := cxData[agents.ImageData](t, imgs[0])
		wantPath := filepath.Join(e.data, "agents", s.ID, "images", "exec-img-1.png")
		if img.ID != "exec-img-1" || img.Mime != "image/png" || img.Prompt != "a red dot" || imgs[0].Text != "a red dot" || !strings.EqualFold(img.Path, wantPath) {
			t.Errorf("image data: %+v want path %s", img, wantPath)
		}
		a, err := e.m.Artifact(s.ID, img.Path)
		if err != nil || a.Mime != "image/png" || len(a.Data) == 0 {
			t.Fatalf("artifact: %+v %v", a, err)
		}
		if a, err = e.m.Artifact(s.ID, filepath.Join("images", "exec-img-1.png")); err != nil || a.Mime != "image/png" {
			t.Errorf("relative artifact path: %+v %v", a, err)
		}

		rs := cxRequests(t, log)
		ts := cxDecode[map[string]any](t, cxFind(t, rs, "thread/start").Params)
		if ts["cwd"] == "" || ts["approvalPolicy"] != "untrusted" || ts["sandbox"] != "workspace-write" {
			t.Errorf("thread/start: %v", ts)
		}
		turn := cxDecode[map[string]any](t, cxFind(t, rs, "turn/start").Params)
		if turn["effort"] != "high" || turn["summary"] != "auto" || turn["threadId"] != "thr-new" {
			t.Errorf("turn/start: %v", turn)
		}
		init := cxDecode[struct {
			Caps struct {
				OptOut []string `json:"optOutNotificationMethods"`
			} `json:"capabilities"`
		}](t, cxFind(t, rs, "initialize").Params)
		if !slices.Contains(init.Caps.OptOut, "item/commandExecution/outputDelta") {
			t.Errorf("noisy notifications must be opted out: %+v", init)
		}
	})

	t.Run("denial reaches codex and the tool reports declined", func(t *testing.T) {
		e, _ := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "DENYME"})
		s = e.wait(t, s.ID, "approval", cxHasPending)
		if err := e.m.Approve(ctx10(t), s.ID, s.Pending[0].ID, false, "no way"); err != nil {
			t.Fatal(err)
		}
		s = e.wait(t, s.ID, "done", status(agents.StatusDone))
		if s.LastMessage != "command decision: decline" {
			t.Fatalf("codex must receive decline: %q", s.LastMessage)
		}
		var declined bool
		for _, ev := range cxOfKind(e.events(t, s.ID), agents.EvToolResult) {
			td := cxData[agents.ToolData](t, ev)
			declined = declined || (td.ID == "cmd-1" && td.Status == "error" && td.Output == "declined")
		}
		if !declined {
			t.Error("declined command must produce an error tool result")
		}
	})

	t.Run("failed turn", func(t *testing.T) {
		e, _ := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "FAIL now"})
		s = e.wait(t, s.ID, "failure", status(agents.StatusFailed))
		if !strings.Contains(s.Error, "model exploded") || strings.Contains(s.Error, `{"type"`) {
			t.Fatalf("provider JSON must be unwrapped: %q", s.Error)
		}
		if len(cxOfKind(e.events(t, s.ID), agents.EvError)) == 0 {
			t.Error("missing error event")
		}
	})

	t.Run("follow-up queues without steering the running turn", func(t *testing.T) {
		e, log := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "SLOW start"})
		e.wait(t, s.ID, "working", func(s agents.Session) bool { return s.Status == agents.StatusWorking && s.AgentSessionID != "" })
		if err := e.m.Prompt(ctx10(t), s.ID, "change course"); err != nil {
			t.Fatal(err)
		}
		s = e.wait(t, s.ID, "queued", func(s agents.Session) bool { return len(s.Queue) == 1 })
		if s.Queue[0].Text != "change course" || s.Queue[0].State != "queued" {
			t.Fatalf("follow-up queue: %+v", s.Queue)
		}
		methods := cxMethods(cxRequests(t, log))
		if slices.Contains(methods, "turn/steer") {
			t.Fatalf("a queued prompt must not steer: %v", methods)
		}
		if _, err := e.m.Stop(ctx10(t), s.ID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("stop interrupts the turn and ends the process", func(t *testing.T) {
		e, log := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "SLOW forever"})
		e.wait(t, s.ID, "working", func(s agents.Session) bool { return s.Status == agents.StatusWorking && s.AgentSessionID != "" })
		time.Sleep(200 * time.Millisecond) // let turn/started land
		st, err := e.m.Stop(ctx10(t), s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if st.Status != agents.StatusStopped || st.Live {
			t.Fatalf("stop: %+v", st)
		}
		if !slices.Contains(cxMethods(cxRequests(t, log)), "turn/interrupt") {
			t.Errorf("stop must interrupt the running turn first: %v", cxMethods(cxRequests(t, log)))
		}
	})

	t.Run("resume continues the codex thread", func(t *testing.T) {
		e, log := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "first"})
		s = e.wait(t, s.ID, "first turn", status(agents.StatusDone))
		if _, err := e.m.Stop(ctx10(t), s.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.m.Prompt(ctx10(t), s.ID, "second"); err != nil {
			t.Fatal(err)
		}
		s = e.wait(t, s.ID, "second turn", func(s agents.Session) bool { return s.Status == agents.StatusDone && s.LastMessage == "echo: second" })
		if s.AgentSessionID != "thr-new" {
			t.Fatalf("thread id: %+v", s)
		}
		rs := cxRequests(t, log)
		res := cxDecode[map[string]any](t, cxFind(t, rs, "thread/resume").Params)
		if res["threadId"] != "thr-new" || res["excludeTurns"] != true {
			t.Errorf("thread/resume params: %v", res)
		}
		if n := strings.Count(strings.Join(cxMethods(rs), "\n")+"\n", "thread/start\n"); n != 1 {
			t.Errorf("resume must not start a second thread (%d starts): %v", n, cxMethods(rs))
		}
	})

	t.Run("resume of a missing thread fails the start", func(t *testing.T) {
		e, _ := newCodexEnv(t, fake)
		_ = e
		_, err := agents.CodexApp{Bin: fake}.Start(ctx10(t), &agents.Host{DataDir: t.TempDir()}, agents.LaunchSpec{
			SessionID: "s1", Kind: "codex", Dir: t.TempDir(), Mode: agents.ModeStructured, Resume: "thr-missing"})
		if err == nil || !strings.Contains(err.Error(), "no rollout found") {
			t.Fatalf("want the protocol error, got %v", err)
		}
	})

	t.Run("gateway, skills and policy are injected per thread", func(t *testing.T) {
		_, log := newCodexEnv(t, fake)
		host := &agents.Host{DataDir: t.TempDir()}
		p, err := agents.CodexApp{Bin: fake}.Start(ctx10(t), host, agents.LaunchSpec{
			SessionID: "s2", Kind: "codex", Dir: t.TempDir(), Model: "fake-a", Mode: agents.ModeStructured, PermissionMode: "auto",
			SkillsPrompt: "Skills: review, deploy",
			Gateway:      &agents.Gateway{Command: `C:\orole\orole-daemon.exe`, Args: []string{"mcp-gateway"}, Env: map[string]string{"OROLE_SESSION": "s2"}}})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Stop()
		ts := cxDecode[struct {
			Model        string `json:"model"`
			Approval     string `json:"approvalPolicy"`
			Sandbox      string `json:"sandbox"`
			Reviewer     string `json:"approvalsReviewer"`
			Instructions string `json:"developerInstructions"`
			Config       struct {
				MCP map[string]struct {
					Command string            `json:"command"`
					Args    []string          `json:"args"`
					Env     map[string]string `json:"env"`
					Mode    string            `json:"default_tools_approval_mode"`
				} `json:"mcp_servers"`
			} `json:"config"`
		}](t, cxFind(t, cxRequests(t, log), "thread/start").Params)
		g := ts.Config.MCP["orole"]
		if ts.Model != "fake-a" || ts.Approval != "on-request" || ts.Sandbox != "workspace-write" || ts.Reviewer != "auto_review" || ts.Instructions != "Skills: review, deploy" ||
			g.Command != `C:\orole\orole-daemon.exe` || !slices.Equal(g.Args, []string{"mcp-gateway"}) || g.Env["OROLE_SESSION"] != "s2" || g.Mode != "approve" {
			t.Fatalf("thread/start: %+v", ts)
		}
		if err := p.Respond("nope", true, ""); err == nil {
			t.Error("unknown permission id must be rejected")
		}
	})

	t.Run("mcp approvals are relayed and forms that need input are declined", func(t *testing.T) {
		e, _ := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "ELICIT"})
		s = e.wait(t, s.ID, "mcp approval", cxHasPending)
		req := s.Pending[0]
		if req.Tool != "mcp:orole" || !strings.Contains(req.Summary, `run tool "ping"`) || !strings.Contains(req.InputJSON, "tool_params") {
			t.Fatalf("mcp approval: %+v", req)
		}
		if err := e.m.Approve(ctx10(t), s.ID, req.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		s = e.wait(t, s.ID, "done", status(agents.StatusDone))
		if s.LastMessage != "form: decline" {
			t.Fatalf("a form needing input must be declined: %q", s.LastMessage)
		}
		var sys, accepted bool
		for _, ev := range e.events(t, s.ID) {
			sys = sys || (ev.Kind == agents.EvSystem && strings.Contains(ev.Text, "cannot collect"))
			accepted = accepted || (ev.Kind == agents.EvMessage && ev.Text == "elicitation: accept")
		}
		if !sys || !accepted {
			t.Errorf("system note=%v accepted=%v", sys, accepted)
		}
	})

	t.Run("clarifying questions use a native typed answer", func(t *testing.T) {
		e, _ := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "ASK"})
		s = e.wait(t, s.ID, "question", func(s agents.Session) bool { return len(s.Questions) == 1 })
		if len(s.Pending) != 0 || s.Questions[0].Questions[0].Prompt != "Which database?" {
			t.Fatalf("question must not become permission: %+v", s)
		}
		_, err := e.m.AnswerInput(ctx10(t), s.ID, s.Questions[0].ID,
			[]agents.QuestionAnswer{{QuestionID: "a", Values: []string{"Postgres"}}}, false)
		if err != nil {
			t.Fatal(err)
		}
		s = e.wait(t, s.ID, "done", status(agents.StatusDone))
		if len(s.Questions) != 0 || s.LastMessage != "asked" {
			t.Fatalf("question did not resolve: %+v", s)
		}
	})

	t.Run("non-image results are not saved", func(t *testing.T) {
		e, _ := newCodexEnv(t, fake)
		s := e.start(t, agents.StartSpec{Kind: "codex", Prompt: "BIGIMAGE"})
		s = e.wait(t, s.ID, "done", status(agents.StatusDone))
		evs := e.events(t, s.ID)
		if len(cxOfKind(evs, agents.EvImage)) != 0 {
			t.Fatal("garbage must not become an image event")
		}
		var errText string
		for _, ev := range cxOfKind(evs, agents.EvError) {
			errText += ev.Text
		}
		if !strings.Contains(errText, "not a PNG") {
			t.Fatalf("error events: %q", errText)
		}
		if entries, _ := os.ReadDir(filepath.Join(e.data, "agents", s.ID, "images")); len(entries) != 0 {
			t.Fatalf("nothing may be written: %v", entries)
		}
	})

	t.Run("process crash fails the start honestly", func(t *testing.T) {
		e, _ := newCodexEnv(t, fake)
		_, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "codex", Dir: t.TempDir(), Prompt: "CRASH"})
		if err == nil {
			t.Fatal("a crashed app-server must fail the start")
		}
	})

	t.Run("launch options list the models codex reports", func(t *testing.T) {
		e, _ := newCodexEnv(t, fake)
		opts, err := e.m.Options(ctx10(t), "local")
		if err != nil {
			t.Fatal(err)
		}
		var h *agents.HarnessOption
		for i := range opts.Harnesses {
			if opts.Harnesses[i].ID == "codex" {
				h = &opts.Harnesses[i]
			}
		}
		if h == nil || !h.Available || !h.Structured || !h.Interactive || !h.CustomModel {
			t.Fatalf("codex option: %+v", h)
		}
		if len(h.Models) != 2 || h.Models[0].ID != "fake-a" || !h.Models[0].Default || h.Models[1].ID != "fake-b" || !slices.Equal(h.Models[1].Reasoning, []string{"medium", "ultra"}) {
			t.Fatalf("hidden models must be dropped and levels kept per model: %+v", h.Models)
		}
		if !slices.Equal(h.Reasoning, []string{"low", "high", "medium", "ultra"}) {
			t.Fatalf("reasoning union: %v", h.Reasoning)
		}
	})

	t.Run("image task", func(t *testing.T) {
		e, log := newCodexEnv(t, fake)
		if _, err := e.m.StartImage(ctx10(t), "  "); err == nil {
			t.Fatal("empty prompt accepted")
		}
		s, err := e.m.StartImage(ctx10(t), "a lighthouse at dusk")
		if err != nil {
			t.Fatal(err)
		}
		s = e.wait(t, s.ID, "image session", status(agents.StatusDone))
		if s.Kind != "codex" || s.Group != "images" || s.PermissionMode != "plan" || !strings.HasPrefix(s.Title, "Image: a lighthouse") ||
			filepath.Base(s.Dir) != "images-workdir" || filepath.Dir(s.Dir) != e.data {
			t.Fatalf("image session: %+v", s)
		}
		ts := cxDecode[map[string]any](t, cxFind(t, cxRequests(t, log), "thread/start").Params)
		if ts["sandbox"] != "read-only" || ts["model"] != "fake-a" {
			t.Errorf("image tasks must be read-only and use the account's default model: %v", ts)
		}
		if !strings.Contains(s.Task, "image generation tool") || !strings.Contains(s.Task, "a lighthouse at dusk") {
			t.Errorf("task: %q", s.Task)
		}
	})
}

func cxDecode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return v
}

func TestCodexModes(t *testing.T) {
	a := agents.Codex()
	if a.Kind() != "codex" || !a.CanResume() || !slices.Equal(a.Modes(), []agents.Mode{agents.ModeStructured, agents.ModeTerminal}) {
		t.Fatalf("codex adapter contract: kind=%s resume=%v modes=%v", a.Kind(), a.CanResume(), a.Modes())
	}
	if _, err := a.Start(context.Background(), nil, agents.LaunchSpec{Machine: "elsewhere", Mode: agents.ModeTerminal}); err == nil {
		t.Fatal("terminal mode accepted a remote machine")
	}
	if _, err := (agents.CodexApp{Bin: "definitely-not-installed-xyz"}).Start(context.Background(), nil, agents.LaunchSpec{Mode: agents.ModeStructured, PermissionMode: "bogus"}); err == nil {
		t.Fatal("bogus permission mode accepted")
	}
}

func TestArtifactIsConfinedToTheSessionDirectory(t *testing.T) {
	e, _ := newCodexEnv(t, "unused")
	now := time.Now().UTC()
	save := func(id string) string {
		if err := e.st.SaveSession(agents.Session{ID: id, Kind: "codex", Machine: "local", Dir: t.TempDir(), Mode: agents.ModeStructured, Status: agents.StatusDone, StartedAt: now, UpdatedAt: now}, ""); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(e.data, "agents", id)
		if err := os.MkdirAll(filepath.Join(dir, "images"), 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	dir := save("sess-a")
	other := save("sess-b")
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) string {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := write(filepath.Join(dir, "images", "ok.png"), pngBuf.Bytes())
	text := write(filepath.Join(dir, "images", "notes.txt"), []byte("secret"))
	foreign := write(filepath.Join(other, "images", "theirs.png"), pngBuf.Bytes())
	outside := write(filepath.Join(e.data, "outside.png"), pngBuf.Bytes())

	if a, err := e.m.Artifact("sess-a", good); err != nil || a.Mime != "image/png" || !bytes.Equal(a.Data, pngBuf.Bytes()) {
		t.Fatalf("valid artifact: %v", err)
	}
	sep := string(filepath.Separator)
	bad := map[string][2]string{
		"parent traversal":     {"sess-a", filepath.Join(dir, "images", "..", "..", "..", "outside.png")},
		"relative traversal":   {"sess-a", ".." + sep + ".." + sep + "outside.png"},
		"absolute outside":     {"sess-a", outside},
		"other session's file": {"sess-a", foreign},
		"database":             {"sess-a", filepath.Join(e.data, "orole.db")},
		"session dir itself":   {"sess-a", dir},
		"not an image":         {"sess-a", text},
		"directory":            {"sess-a", filepath.Join(dir, "images")},
		"missing file":         {"sess-a", filepath.Join(dir, "images", "nope.png")},
		"empty path":           {"sess-a", ""},
		"nul byte":             {"sess-a", good + "\x00.png"},
		"id traversal":         {"..", good},
		"id with separator":    {"sess-a" + sep + ".." + sep + "sess-b", foreign},
		"unknown session":      {"sess-zzz", good},
		"empty id":             {"", good},
	}
	for name, c := range bad {
		if a, err := e.m.Artifact(c[0], c[1]); err == nil {
			t.Errorf("%s: served %d bytes (%s)", name, len(a.Data), a.Mime)
		}
	}

	// A link inside the session pointing out of it must not be followed.
	link := filepath.Join(dir, "images", "link.png")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := e.m.Artifact("sess-a", link); err == nil {
			t.Error("symlink out of the session directory was served")
		}
	} else {
		t.Logf("symlinks unavailable here (%v); resolution is still covered by EvalSymlinks", err)
	}
}

func TestArtifactDownscalesImagesThatDoNotFitAnIPCFrame(t *testing.T) {
	e, _ := newCodexEnv(t, "unused")
	now := time.Now().UTC()
	if err := e.st.SaveSession(agents.Session{ID: "big", Kind: "codex", Machine: "local", Dir: t.TempDir(), Mode: agents.ModeStructured, Status: agents.StatusDone, StartedAt: now, UpdatedAt: now}, ""); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.data, "agents", "big", "images")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1400, 1400))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(rng.IntN(256)), byte(rng.IntN(256)), byte(rng.IntN(256)), 255
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() <= 3<<20 {
		t.Fatalf("test image too small to exceed the frame budget: %d", buf.Len())
	}
	path := filepath.Join(dir, "big.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := e.m.Artifact("big", path)
	if err != nil {
		t.Fatal(err)
	}
	if a.Mime != "image/jpeg" || len(a.Data) > 3<<20 || len(a.Data) < 1000 {
		t.Fatalf("preview: mime=%s bytes=%d", a.Mime, len(a.Data))
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(a.Data)); err != nil || cfg.Width == 0 {
		t.Fatalf("preview is not a decodable JPEG: %v", err)
	}
}

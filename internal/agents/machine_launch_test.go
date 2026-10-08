package agents_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/agents/orchestrate"
)

type launchCapture struct{ specs []agents.LaunchSpec }

func (*launchCapture) Kind() string         { return "claude" }
func (*launchCapture) Name() string         { return "Capture" }
func (*launchCapture) Modes() []agents.Mode { return []agents.Mode{agents.ModeTerminal} }
func (*launchCapture) CanResume() bool      { return false }
func (*launchCapture) Detect(context.Context) agents.Detection {
	return agents.Detection{Kind: "claude", Name: "Capture", Modes: []agents.Mode{agents.ModeTerminal}}
}
func (a *launchCapture) Start(_ context.Context, _ *agents.Host, spec agents.LaunchSpec) (agents.Process, error) {
	a.specs = append(a.specs, spec)
	ch := make(chan agents.Event, 1)
	ch <- agents.Event{Kind: agents.EvExit, Data: []byte(`{"code":0}`)}
	close(ch)
	return captureProcess{ch}, nil
}

type captureProcess struct{ events chan agents.Event }

func (captureProcess) Pid() int                           { return 0 }
func (captureProcess) TerminalID() string                 { return "" }
func (captureProcess) Send(string) error                  { return errors.New("unexpected send") }
func (captureProcess) Respond(string, bool, string) error { return errors.New("unexpected response") }
func (captureProcess) Stop() error                        { return nil }
func (p captureProcess) Events() <-chan agents.Event      { return p.events }

type forbiddenWorktrees struct{ t *testing.T }

func (w forbiddenWorktrees) Create(context.Context, string, string, string) (string, string, error) {
	w.t.Error("remote launch called local worktrees")
	return "", "", errors.New("forbidden")
}
func (w forbiddenWorktrees) Remove(context.Context, string, string) error {
	w.t.Error("remote review removed a local worktree")
	return errors.New("forbidden")
}
func (w forbiddenWorktrees) Head(context.Context, string) (string, error) {
	w.t.Error("remote launch read local HEAD")
	return "", errors.New("forbidden")
}
func (w forbiddenWorktrees) Diff(context.Context, string, string) (agents.Diff, error) {
	w.t.Error("remote review read a local diff")
	return agents.Diff{}, errors.New("forbidden")
}

func TestLaunchPersistsSelectedReasoningAndMachine(t *testing.T) {
	e := newEnv(t)
	a := &launchCapture{}
	m := agents.NewManager(agents.Config{Store: e.st, Adapters: []agents.Adapter{a}, Authorize: func(context.Context, string, string, string) error { return nil }})
	t.Cleanup(m.Shutdown)
	s, err := m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: t.TempDir(), Prompt: "task", Model: "custom/model", Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.specs) != 1 || a.specs[0].Machine != "local" || a.specs[0].Reasoning != "high" || a.specs[0].Model != "custom/model" {
		t.Fatalf("wrong adapter launch: %+v", a.specs)
	}
	stored, err := e.st.GetSession(s.ID)
	if err != nil || stored.Machine != "local" || stored.Reasoning != "high" {
		t.Fatalf("lost launch selection: %+v %v", stored, err)
	}
}

func TestRemoteLaunchNeverFallsBackToLocalAdapter(t *testing.T) {
	e := newEnv(t)
	a := &launchCapture{}
	m := agents.NewManager(agents.Config{Store: e.st, Adapters: []agents.Adapter{a}, Worktrees: forbiddenWorktrees{t}, Authorize: func(context.Context, string, string, string) error { return nil }})
	t.Cleanup(m.Shutdown)
	s, err := m.Start(ctx10(t), agents.StartSpec{Machine: "remote-box", Kind: "claude", Dir: "/approved/project", Prompt: "task", Model: "remote-model", Reasoning: "medium"})
	if err == nil || !strings.Contains(err.Error(), "machine transport") {
		t.Fatalf("unexpected remote launch: %+v %v", s, err)
	}
	if len(a.specs) != 0 {
		t.Fatal("remote request dispatched local adapter")
	}
	stored, err := e.st.GetSession(s.ID)
	if err != nil || stored.Machine != "remote-box" || stored.Dir != "/approved/project" || stored.Reasoning != "medium" || stored.Mode != agents.ModeProcess {
		t.Fatalf("remote selection not persisted: %+v %v", stored, err)
	}
}

func TestRemoteSessionOperationsNeverTouchLocalPaths(t *testing.T) {
	e := newEnv(t, withWT(forbiddenWorktrees{t}))
	now := time.Now().UTC()
	s := agents.Session{ID: "remote-namespace", Kind: "claude", Machine: "remote-box", Dir: t.TempDir(), Project: t.TempDir(), Mode: agents.ModeProcess, WorktreeOwned: true, BaseRef: "main", Status: agents.StatusDone, LastMessage: "summary", StartedAt: now, UpdatedAt: now}
	if err := e.st.SaveSession(s, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Diff(ctx10(t), s.ID); err == nil {
		t.Fatal("remote diff accepted")
	}
	if _, err := e.m.Check(ctx10(t), s.ID, "echo must-not-run"); err == nil {
		t.Fatal("remote check accepted")
	}
	for _, decision := range []string{"approve", "send-back", "discard"} {
		if _, err := e.m.Review(ctx10(t), s.ID, decision, "feedback"); err == nil {
			t.Fatalf("remote review %s accepted", decision)
		}
	}
	if err := e.m.WithStoppedSession(ctx10(t), s.ID, func() error { t.Fatal("remote publication touched a local directory"); return nil }); err == nil {
		t.Fatal("remote publication accepted")
	}
	if s.HasTurnSignal() {
		t.Fatal("remote text mode advertised structured turn signals")
	}
	o := orchestrate.New(e.m, nil)
	defer o.Shutdown()
	if _, err := o.HandOff(ctx10(t), "handoff-remote", orchestrate.HandOffParams{From: s.ID, Kind: "claude"}); err == nil {
		t.Fatal("remote handoff accepted")
	}
	if _, err := o.LoopUntil(ctx10(t), "loop-remote", orchestrate.LoopParams{Session: s.ID, Command: "echo must-not-run", MaxRounds: 1}); err == nil {
		t.Fatal("remote loop accepted")
	}
	s.Status = agents.StatusLost
	if err := e.st.SaveSession(s, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Prompt(ctx10(t), s.ID, "new task"); err == nil || !strings.Contains(err.Error(), "termination is unconfirmed") {
		t.Fatalf("lost remote auto-restarted: %v", err)
	}
}

func TestLocalOptionsExcludeOtherMachineProjects(t *testing.T) {
	e := newEnv(t)
	m := agents.NewManager(agents.Config{Store: e.st, Adapters: []agents.Adapter{&launchCapture{}}, ProjectOptions: func(context.Context) ([]agents.ProjectOption, error) {
		return []agents.ProjectOption{{ID: "local-project", Machine: "local", Path: "/same/path"}, {ID: "remote-project", Machine: "remote-box", Path: "/same/path"}}, nil
	}})
	defer m.Shutdown()
	options, err := m.Options(ctx10(t), "")
	if err != nil || options.Machine != "local" || len(options.Projects) != 1 || options.Projects[0].ID != "local-project" {
		t.Fatalf("namespace leaked: %+v %v", options, err)
	}
}

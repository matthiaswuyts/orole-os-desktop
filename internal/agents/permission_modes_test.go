package agents_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/events"
)

// whichMode asks the fake CLI which permission mode it is in and with which arguments it was
// launched, and waits for the answer.
func whichMode(t *testing.T, e *env, id string) string {
	t.Helper()
	s, err := e.m.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	turns := s.Turns
	if err := e.m.Prompt(ctx10(t), id, "WHICHMODE"); err != nil {
		t.Fatal(err)
	}
	s = e.wait(t, id, "mode answer", func(s agents.Session) bool { return s.Turns > turns && s.Status == agents.StatusDone })
	return s.LastMessage
}

func TestPermissionModesMapOntoClaudeFlags(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		mode, flag, session string
	}{
		{"", "default", "manual"},
		{"manual", "default", "manual"},
		// The fake CLI lists no auto mode, so Auto is the closest sandboxed equivalent.
		{"auto", "acceptEdits", "auto"},
		{"plan", "plan", "plan"},
	} {
		s := e.start(t, agents.StartSpec{Prompt: "go", PermissionMode: c.mode})
		if s.PermissionMode != c.session {
			t.Errorf("%q stored as %q, want %q", c.mode, s.PermissionMode, c.session)
		}
		e.wait(t, s.ID, "first turn", status(agents.StatusDone))
		got := whichMode(t, e, s.ID)
		if !strings.Contains(got, "--permission-mode "+c.flag) || !strings.HasPrefix(got, "mode="+c.flag+" ") {
			t.Errorf("%q: %s", c.mode, got)
		}
		// A manual or auto conversation can later be moved to YOLO, which the CLI only accepts
		// when it was launched allowing it.
		if c.mode != "plan" && !strings.Contains(got, "--allow-dangerously-skip-permissions") {
			t.Errorf("%q: launch must allow a later switch to YOLO: %s", c.mode, got)
		}
	}
}

func TestYoloStartIsGatedInGo(t *testing.T) {
	enabled := atomic.Bool{}
	enabled.Store(true)
	e := newEnv(t, withYoloSetting(enabled.Load))
	if _, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: t.TempDir(), PermissionMode: "yolo"}); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("yolo without confirmation: %v", err)
	}
	auto := events.WithChain(context.Background(), []string{"automation-1"})
	if _, err := e.m.Start(auto, agents.StartSpec{Kind: "claude", Dir: t.TempDir(), PermissionMode: "yolo", ConfirmYolo: true}); err == nil || !strings.Contains(err.Error(), "automations") {
		t.Fatalf("yolo from an automation: %v", err)
	}
	enabled.Store(false)
	if _, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: t.TempDir(), PermissionMode: "yolo", ConfirmYolo: true}); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("yolo while the setting is off: %v", err)
	}
	if !e.rec.has("agent.yolo denied") || e.rec.has("agent.yolo allowed") {
		t.Fatalf("denials must be audited, no allowance: %v", e.rec.audits)
	}
	enabled.Store(true)
	s := e.start(t, agents.StartSpec{Prompt: "go", PermissionMode: "yolo", ConfirmYolo: true})
	e.wait(t, s.ID, "turn", status(agents.StatusDone))
	if s.PermissionMode != "yolo" || !e.rec.has("agent.yolo allowed") {
		t.Fatalf("yolo start: %+v audits=%v", s, e.rec.audits)
	}
	if got := whichMode(t, e, s.ID); !strings.HasPrefix(got, "mode=bypassPermissions ") {
		t.Fatalf("yolo must run bypassPermissions: %s", got)
	}
}

func TestSetPermissionModeSwitchesLiveClaude(t *testing.T) {
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Prompt: "go"})
	e.wait(t, s.ID, "turn", status(agents.StatusDone))
	if !s.Followup.PermissionMode {
		t.Fatalf("a structured local session can switch: %+v", s.Followup)
	}
	got, err := e.m.SetPermissionMode(ctx10(t), s.ID, "auto", false)
	if err != nil || got.PermissionMode != "auto" {
		t.Fatalf("auto: %+v %v", got, err)
	}
	if mode := whichMode(t, e, s.ID); !strings.HasPrefix(mode, "mode=acceptEdits ") || strings.Contains(mode, "--resume") {
		t.Fatalf("live switch must not restart: %s", mode)
	}
	if _, err := e.m.SetPermissionMode(ctx10(t), s.ID, "yolo", false); err == nil {
		t.Fatal("yolo switch without confirmation")
	}
	if got, _ := e.m.Get(s.ID); got.PermissionMode != "auto" {
		t.Fatalf("a rejected switch must not persist: %q", got.PermissionMode)
	}
	if got, err = e.m.SetPermissionMode(ctx10(t), s.ID, "yolo", true); err != nil || got.PermissionMode != "yolo" {
		t.Fatalf("yolo: %+v %v", got, err)
	}
	if mode := whichMode(t, e, s.ID); !strings.HasPrefix(mode, "mode=bypassPermissions ") {
		t.Fatalf("yolo must apply live: %s", mode)
	}
	if _, err := e.m.SetPermissionMode(ctx10(t), s.ID, "plan", false); err == nil {
		t.Fatal("plan is not user selectable")
	}
	if !e.rec.has("agent.permissionMode allowed") || !e.rec.has("agent.yolo allowed") {
		t.Fatalf("switches must be audited: %v", e.rec.audits)
	}
	tr, _ := e.m.Transcript(s.ID, 100)
	for _, ev := range tr {
		if ev.Kind == agents.EvSystem && strings.Contains(ev.Text, "Permission mode changed") {
			t.Fatalf("a mode switch must not add a notice to the conversation: %q", ev.Text)
		}
	}
}

func TestSetPermissionModeResumesWhenClaudeWasLaunchedWithoutYolo(t *testing.T) {
	enabled := atomic.Bool{}
	e := newEnv(t, withYoloSetting(enabled.Load)) // off at launch, so the CLI is not told to allow bypass
	s := e.start(t, agents.StartSpec{Prompt: "go"})
	e.wait(t, s.ID, "turn", status(agents.StatusDone))
	enabled.Store(true)
	got, err := e.m.SetPermissionMode(ctx10(t), s.ID, "yolo", true)
	if err != nil || got.PermissionMode != "yolo" {
		t.Fatalf("yolo: %+v %v", got, err)
	}
	e.wait(t, s.ID, "relaunched", func(s agents.Session) bool { return s.Live && s.Status != agents.StatusWorking })
	mode := whichMode(t, e, s.ID)
	if !strings.HasPrefix(mode, "mode=bypassPermissions ") || !strings.Contains(mode, "--resume "+s.AgentSessionID) {
		t.Fatalf("must resume the same conversation in YOLO: %s", mode)
	}
}

func TestSetPermissionModeOnEndedSessionAppliesToNextLaunch(t *testing.T) {
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Prompt: "go", PermissionMode: "auto"})
	e.wait(t, s.ID, "turn", status(agents.StatusDone))
	if _, err := e.m.Stop(ctx10(t), s.ID); err != nil {
		t.Fatal(err)
	}
	e.wait(t, s.ID, "stopped", func(s agents.Session) bool { return !s.Live })
	if got, err := e.m.SetPermissionMode(ctx10(t), s.ID, "manual", false); err != nil || got.PermissionMode != "manual" || got.Live {
		t.Fatalf("ended session: %+v %v", got, err)
	}
	if _, err := e.m.Restart(ctx10(t), s.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	e.wait(t, s.ID, "live", func(s agents.Session) bool { return s.Live })
	if mode := whichMode(t, e, s.ID); !strings.HasPrefix(mode, "mode=default ") {
		t.Fatalf("restart must use the stored mode: %s", mode)
	}
}

func TestSetPermissionModeRefusesTerminalAndRemote(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	for name, s := range map[string]agents.Session{
		"terminal": {ID: "term", Kind: "claude", Machine: "local", Mode: agents.ModeTerminal},
		"remote":   {ID: "remote", Kind: "claude", Machine: "box", Mode: agents.ModeProcess},
		"review":   {ID: "review", Kind: "claude", Machine: "local", Mode: agents.ModeStructured, PermissionMode: "plan"},
	} {
		s.Status, s.StartedAt, s.UpdatedAt = agents.StatusDone, now, now
		if s.PermissionMode == "" {
			s.PermissionMode = "manual"
		}
		if err := e.st.SaveSession(s, ""); err != nil {
			t.Fatal(err)
		}
		got, err := e.m.Get(s.ID)
		if err != nil || got.Followup.PermissionMode || got.Followup.PermissionModeReason == "" {
			t.Errorf("%s: capability must be refused with a reason: %+v %v", name, got.Followup, err)
		}
		if _, err := e.m.SetPermissionMode(ctx10(t), s.ID, "auto", false); err == nil {
			t.Errorf("%s: switch accepted", name)
		}
	}
}

func TestDetectReportsHowEachModeIsRealised(t *testing.T) {
	enabled := atomic.Bool{}
	enabled.Store(true)
	e := newEnv(t, withYoloSetting(enabled.Load))
	modes := func() map[string]agents.PermissionModeSupport {
		out := map[string]agents.PermissionModeSupport{}
		for _, m := range e.m.Detect(ctx10(t), false)[0].PermissionModes {
			out[m.Mode] = m
		}
		return out
	}
	got := modes()
	if len(got) != 3 || !got["manual"].Available || got["manual"].Approximated || !got["yolo"].Available {
		t.Fatalf("modes: %+v", got)
	}
	// The fake CLI has no auto mode: Auto is available but honestly reported as approximated.
	if !got["auto"].Available || !got["auto"].Approximated || got["auto"].Detail == "" {
		t.Fatalf("auto must be approximated: %+v", got["auto"])
	}
	enabled.Store(false)
	if y := modes()["yolo"]; y.Available || y.Detail == "" {
		t.Fatalf("yolo must be unavailable while the setting is off: %+v", y)
	}
	enabled.Store(true)
	if !modes()["yolo"].Available {
		t.Fatal("re-enabling the setting must not need a cache refresh")
	}
}

func TestTerminalModesAreOnlyHonouredWhereTheCLIAcceptsThem(t *testing.T) {
	e := newEnv(t)
	_, err := e.m.Start(ctx10(t), agents.StartSpec{Kind: "custom", Command: "echo", Dir: t.TempDir(), Mode: agents.ModeTerminal, PermissionMode: "yolo", ConfirmYolo: true})
	if err == nil || !strings.Contains(err.Error(), "own approvals") {
		t.Fatalf("a terminal CLI that ignores the mode must be refused: %v", err)
	}
}

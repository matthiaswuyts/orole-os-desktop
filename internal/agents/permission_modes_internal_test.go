package agents

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCodexSetPermissionModeAppliesFromNextTurn(t *testing.T) {
	p, _ := chatCodexFixture()
	p.calls = make(map[int64]chan rpcResp)
	var frames []map[string]any
	p.stdin = followupWireWriter(func(b []byte) (int, error) {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if err := json.Unmarshal(b, &frame); err != nil {
			return 0, err
		}
		frames = append(frames, frame.Params)
		p.dispatch(jraw(map[string]any{"id": frame.ID, "result": map[string]any{"turn": map[string]string{"id": "t"}}}))
		return len(b), nil
	})
	p.policy = codexPolicy{"untrusted", "workspace-write", "user"}

	if err := p.SendConfigured("first", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := frames[0]["approvalPolicy"]; ok {
		t.Fatalf("an unchanged policy must not be re-sent: %v", frames[0])
	}
	p.mu.Lock()
	p.turn, p.lastDone = "", "t"
	p.mu.Unlock()

	next, err := p.SetPermissionMode("auto")
	if err != nil || !next {
		t.Fatalf("codex applies from the next turn: %v %v", next, err)
	}
	if err := p.SendConfigured("second", "", ""); err != nil {
		t.Fatal(err)
	}
	sandbox, _ := frames[1]["sandboxPolicy"].(map[string]any)
	if frames[1]["approvalPolicy"] != "on-request" || frames[1]["approvalsReviewer"] != "auto_review" || sandbox["type"] != "workspaceWrite" {
		t.Fatalf("auto overrides: %v", frames[1])
	}
	p.mu.Lock()
	p.turn, p.lastDone = "", "t"
	p.mu.Unlock()

	if _, err := p.SetPermissionMode("yolo"); err != nil {
		t.Fatal(err)
	}
	if err := p.SendConfigured("third", "", ""); err != nil {
		t.Fatal(err)
	}
	sandbox, _ = frames[2]["sandboxPolicy"].(map[string]any)
	if frames[2]["approvalPolicy"] != "never" || frames[2]["approvalsReviewer"] != "user" || sandbox["type"] != "dangerFullAccess" {
		t.Fatalf("yolo overrides: %v", frames[2])
	}
	p.mu.Lock()
	dirty := p.policyDirty
	p.mu.Unlock()
	if dirty {
		t.Fatal("overrides were delivered; they must not repeat")
	}
	if _, err := p.SetPermissionMode("bypassPermissions"); err == nil {
		t.Fatal("legacy mode accepted")
	}
}

func TestCodexPolicyFor(t *testing.T) {
	for mode, want := range map[string]codexPolicy{
		"":       {"untrusted", "workspace-write", "user"},
		"manual": {"untrusted", "workspace-write", "user"},
		"auto":   {"on-request", "workspace-write", "auto_review"},
		"yolo":   {"never", "danger-full-access", "user"},
		"plan":   {"on-request", "read-only", "user"},
	} {
		if got, err := codexPolicyFor(mode); err != nil || got != want {
			t.Errorf("%q = %+v %v, want %+v", mode, got, err, want)
		}
	}
	for _, mode := range []string{"default", "acceptEdits", "bypassPermissions"} {
		if _, err := codexPolicyFor(mode); err == nil {
			t.Errorf("legacy %q accepted", mode)
		}
	}
}

func TestClaudeSetPermissionModeControl(t *testing.T) {
	for _, c := range []struct {
		mode       string
		autoNative bool
		want       string
	}{{"manual", false, "default"}, {"auto", true, "auto"}, {"auto", false, "acceptEdits"}, {"yolo", true, "bypassPermissions"}} {
		pipe := &pipeProc{exited: make(chan struct{})}
		var frame map[string]any
		pipe.stdin = followupWireWriter(func(b []byte) (int, error) {
			if err := json.Unmarshal(b, &frame); err != nil {
				return 0, err
			}
			pipe.acceptControlResponse(jraw(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": frame["request_id"]}}), true)
			return len(b), nil
		})
		p := &claudeProc{pipeProc: pipe, autoNative: c.autoNative}
		if next, err := p.SetPermissionMode(c.mode); err != nil || next {
			t.Fatalf("%+v: %v %v", c, next, err)
		}
		request, _ := frame["request"].(map[string]any)
		if frame["type"] != "control_request" || request["subtype"] != "set_permission_mode" || request["mode"] != c.want {
			t.Errorf("%+v: wrong control request %v", c, frame)
		}
	}

	pipe := &pipeProc{exited: make(chan struct{})}
	pipe.stdin = followupWireWriter(func(b []byte) (int, error) {
		var frame map[string]any
		_ = json.Unmarshal(b, &frame)
		pipe.acceptControlResponse(jraw(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": frame["request_id"],
			"error": "Cannot set permission mode to bypassPermissions because the session was not launched with --dangerously-skip-permissions"}}), true)
		return len(b), nil
	})
	if _, err := (&claudeProc{pipeProc: pipe}).SetPermissionMode("yolo"); !errors.Is(err, ErrRestartRequired) {
		t.Fatalf("an unlaunchable bypass must ask for a restart: %v", err)
	}
	pipe = &pipeProc{exited: make(chan struct{})}
	pipe.stdin = followupWireWriter(func(b []byte) (int, error) {
		var frame map[string]any
		_ = json.Unmarshal(b, &frame)
		pipe.acceptControlResponse(jraw(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": frame["request_id"], "error": "nope"}}), true)
		return len(b), nil
	})
	if _, err := (&claudeProc{pipeProc: pipe}).SetPermissionMode("auto"); err == nil || errors.Is(err, ErrRestartRequired) || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("a rejection must surface: %v", err)
	}
}

func TestClaudeInitNotesAFallbackMode(t *testing.T) {
	d := claudeDecoder{wantMode: "auto"}
	evs := d.decode(nil, jraw(map[string]any{"type": "system", "subtype": "init", "session_id": "s", "permissionMode": "default"}))
	var note string
	for _, e := range evs {
		if e.Kind == EvSystem {
			note = e.Text
		}
	}
	if !strings.Contains(note, "started in default mode instead of the requested auto") {
		t.Fatalf("a silent downgrade must be visible: %v", evs)
	}
	for _, e := range (&claudeDecoder{wantMode: "auto"}).decode(nil, jraw(map[string]any{"type": "system", "subtype": "init", "permissionMode": "auto"})) {
		if e.Kind == EvSystem {
			t.Fatalf("no note when the mode matches: %+v", e)
		}
	}
}

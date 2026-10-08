package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type configuredChatProcess struct {
	*chatTestProcess
	configs   [][2]string
	configErr error
	events    chan Event
}

func (*configuredChatProcess) FollowupOptions() (bool, bool) { return true, true }
func (p *configuredChatProcess) Events() <-chan Event        { return p.events }
func (p *configuredChatProcess) SendConfigured(text, model, reasoning string) error {
	p.mu.Lock()
	p.configs = append(p.configs, [2]string{model, reasoning})
	p.mu.Unlock()
	if p.configErr != nil {
		return p.configErr
	}
	return p.Send(text)
}

func TestSendMessageQueuesConfigurationUntilItsTurn(t *testing.T) {
	m, ls, original := chatFixture(t, StatusWorking)
	p := &configuredChatProcess{chatTestProcess: original}
	ls.proc = p
	ls.s.Kind, ls.s.Model, ls.s.Reasoning = "codex", "old-model", "low"
	for _, pair := range [][2]string{{"next-model", "high"}, {"last-model", "medium"}} {
		if _, err := m.SendMessage(context.Background(), "chat", pair[0], nil, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := m.st.GetSession("chat")
	if err != nil || len(stored.Queue) != 2 || stored.Queue[0].Model != "next-model" || stored.Queue[0].Reasoning != "high" || stored.Model != "old-model" {
		t.Fatalf("queued options are not durable or were applied too early: %+v %v", stored, err)
	}
	if len(p.configs) != 0 || len(p.prompts()) != 0 {
		t.Fatal("queued follow-up steered the busy process")
	}
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	s := chatWait(t, m, func(s Session) bool { return s.Model == "next-model" && len(s.Queue) == 1 })
	if s.Reasoning != "high" {
		t.Fatal("accepted effort was not reflected in the session")
	}
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	chatWait(t, m, func(s Session) bool { return s.Model == "last-model" && len(s.Queue) == 0 })
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.configs) != 2 || p.configs[0] != [2]string{"next-model", "high"} || p.configs[1] != [2]string{"last-model", "medium"} {
		t.Fatalf("configuration reordered: %+v", p.configs)
	}
}

func TestSendMessageAttachmentOnlySurvivesStagingDiscard(t *testing.T) {
	m, ls, p := chatFixture(t, StatusWorking)
	m.cfg.Host = &Host{DataDir: t.TempDir()}
	ls.s.Dir = t.TempDir()
	ctx := context.Background()
	attachment := uploadForTest(t, m, "reference.txt", []byte("exact follow-up input"))
	for range 2 {
		if _, err := m.SendMessage(ctx, "chat", "", []string{attachment.ID}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := m.Get("chat")
	if len(s.Queue) != 2 || s.Queue[0].Text == s.Queue[1].Text || !strings.Contains(s.Queue[0].Text, "reference.txt") {
		t.Fatalf("each attachment follow-up needs its own durable handoff: %+v", s.Queue)
	}
	if err := m.DiscardAttachment(ctx, attachment.ID); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(ls.s.Dir, ".orole-attachments-*", "1-"+attachment.ID+".txt"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("missing durable inputs: %v %v", paths, err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "exact follow-up input" {
			t.Fatalf("handoff bytes changed: %q %v", data, err)
		}
		ignored, err := os.ReadFile(filepath.Join(filepath.Dir(path), ".gitignore"))
		if err != nil || string(ignored) != "*\n" {
			t.Fatal("follow-up inputs are not protected from publication")
		}
	}
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	chatWait(t, m, func(s Session) bool { return s.Prompts == 1 && len(s.Queue) == 1 })
	if got := p.prompts(); len(got) != 1 || got[0] != s.Queue[0].Text {
		t.Fatal("queue did not deliver the exact accepted file references")
	}
}

func TestSendMessageRejectsUnsupportedAndInvalidInputsBeforeDelivery(t *testing.T) {
	m, ls, p := chatFixture(t, StatusIdle)
	m.cfg.Host = &Host{DataDir: t.TempDir()}
	ls.s.Dir = t.TempDir()
	cases := []struct {
		text          string
		ids           []string
		model, effort string
	}{
		{text: ""},
		{text: "hello", ids: []string{"../secret"}},
		{text: "hello", model: "unsupported-live-model"},
		{text: "hello", effort: "high"},
		{text: "hello", model: "bad\nmodel"},
	}
	for _, tc := range cases {
		if _, err := m.SendMessage(context.Background(), "chat", tc.text, tc.ids, tc.model, tc.effort); err == nil {
			t.Fatalf("invalid follow-up accepted: %+v", tc)
		}
	}
	if len(p.prompts()) != 0 || len(ls.s.Queue) != 0 || ls.s.Prompts != 0 {
		t.Fatal("invalid inputs reached the agent or durable queue")
	}
	ls.s.Model, ls.s.Reasoning = "current", "low"
	if _, err := m.SendMessage(context.Background(), "chat", "same settings", nil, "current", "low"); err != nil {
		t.Fatal("unchanged settings should not require live setters:", err)
	}
}

func TestSendMessageConfigurationFailureNeverReplays(t *testing.T) {
	m, ls, base := chatFixture(t, StatusWorking)
	p := &configuredChatProcess{chatTestProcess: base, configErr: errors.New("configuration acknowledgement lost")}
	ls.proc = p
	if _, err := m.SendMessage(context.Background(), "chat", "next", nil, "new-model", "high"); err != nil {
		t.Fatal(err)
	}
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	s := chatWait(t, m, func(s Session) bool { return s.Status == StatusNeedsYou })
	if len(s.Queue) != 1 || s.Queue[0].State != queueUncertain || s.Model == "new-model" {
		t.Fatalf("uncertain configuration was declared successful: %+v", s)
	}
	if _, err := m.ResumeQueue(context.Background(), "chat"); err == nil {
		t.Fatal("uncertain follow-up was replayed")
	}
	if len(p.prompts()) != 0 {
		t.Fatal("prompt sent after configuration failure")
	}
}

type followupResumeAdapter struct {
	spec LaunchSpec
	p    *configuredChatProcess
}

func (*followupResumeAdapter) Kind() string                     { return "claude" }
func (*followupResumeAdapter) Name() string                     { return "Continuation fixture" }
func (*followupResumeAdapter) Modes() []Mode                    { return []Mode{ModeStructured} }
func (*followupResumeAdapter) CanResume() bool                  { return true }
func (*followupResumeAdapter) Detect(context.Context) Detection { return Detection{} }
func (a *followupResumeAdapter) Start(_ context.Context, _ *Host, spec LaunchSpec) (Process, error) {
	a.spec = spec
	return a.p, nil
}

func TestSendMessageContinuesSameNativeSessionWithSettings(t *testing.T) {
	m, ls, base := chatFixture(t, StatusStopped)
	delete(m.live, "chat")
	ls.s.Dir, ls.s.AgentSessionID, ls.s.Model = t.TempDir(), "native-session", "old"
	ls.s.Queue = []QueuedPrompt{{ID: "paused", Text: "wait for explicit resume", State: queuePaused}}
	if err := m.st.SaveSession(ls.s, ""); err != nil {
		t.Fatal(err)
	}
	p := &configuredChatProcess{chatTestProcess: base, events: make(chan Event, 1)}
	a := &followupResumeAdapter{p: p}
	m.adapters["claude"] = a
	s, err := m.SendMessage(context.Background(), "chat", "continue here", nil, "new-model", "high")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		current := m.controlSession("chat")
		close(p.events)
		if current != nil {
			<-current.done
		}
	})
	if a.spec.Resume != "native-session" || a.spec.NewAgentSessionID != "" || a.spec.Model != "new-model" || a.spec.Reasoning != "high" || s.AgentSessionID != "native-session" {
		t.Fatalf("continuation identity/settings lost: %+v %+v", a.spec, s)
	}
	if got := p.prompts(); len(got) != 1 || got[0] != "continue here" {
		t.Fatal("continuation prompt was not sent exactly once")
	}
	if len(s.Queue) != 1 || s.Queue[0].ID != "paused" || s.Queue[0].State != queuePaused {
		t.Fatalf("continuation released the paused follow-up queue: %+v", s.Queue)
	}
}

func TestSendMessageNeverFallsBackToFreshConversation(t *testing.T) {
	m, _, _ := chatFixture(t, StatusStopped)
	delete(m.live, "chat")
	s, err := m.Get("chat")
	if err != nil || s.Followup.Attachments || s.Followup.Model || s.Followup.Reason == "" {
		t.Fatalf("missing continuation must be explicit: %+v %v", s.Followup, err)
	}
	if _, err := m.SendMessage(context.Background(), "chat", "do not restart fresh", nil, "", ""); err == nil {
		t.Fatal("missing native conversation accepted")
	}
	if m.controlSession("chat") != nil {
		t.Fatal("fresh process launched")
	}
}

type followupWireWriter func([]byte) (int, error)

func (f followupWireWriter) Write(b []byte) (int, error) { return f(b) }
func (followupWireWriter) Close() error                  { return nil }

func TestFollowupPipeControlsAcknowledgeBeforePrompt(t *testing.T) {
	for _, kind := range []string{"claude", "omp"} {
		t.Run(kind, func(t *testing.T) {
			pipe := &pipeProc{exited: make(chan struct{}), encodePrompt: func(text string) []byte { return jraw(map[string]string{"type": "user", "text": text}) }}
			var frames []map[string]any
			pipe.stdin = followupWireWriter(func(b []byte) (int, error) {
				var frame map[string]any
				if err := json.Unmarshal(b, &frame); err != nil {
					return 0, err
				}
				frames = append(frames, frame)
				if kind == "claude" && frame["type"] == "control_request" {
					decoder := claudeDecoder{}
					decoder.decode(pipe, jraw(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": frame["request_id"]}}))
				} else if kind == "omp" && frame["type"] == "get_state" {
					state := ompState{}
					state.frame(pipe, "response", jraw(map[string]any{"type": "response", "id": frame["id"], "success": true, "data": map[string]any{"model": map[string]string{"provider": "openai", "id": "gpt-model"}, "thinkingLevel": "high"}}))
				} else if kind == "omp" && (frame["type"] == "set_model" || frame["type"] == "set_thinking_level") {
					state := ompState{}
					state.frame(pipe, "response", jraw(map[string]any{"type": "response", "id": frame["id"], "success": true}))
				}
				return len(b), nil
			})
			if kind == "claude" {
				p := &claudeProc{pipeProc: pipe}
				if err := p.SendConfigured("next", "sonnet", ""); err != nil {
					t.Fatal(err)
				}
				if len(frames) != 2 || frames[0]["request"].(map[string]any)["subtype"] != "set_model" || frames[1]["type"] != "user" {
					t.Fatalf("wrong control order: %+v", frames)
				}
				if model, effort := p.FollowupOptions(); !model || effort {
					t.Fatal("Claude advertised unsupported live effort")
				}
			} else {
				p := &ompProc{pipeProc: pipe}
				if err := p.SendConfigured("next", "openai/gpt-model", "high"); err != nil {
					t.Fatal(err)
				}
				if len(frames) != 4 || frames[0]["type"] != "set_model" || frames[0]["provider"] != "openai" || frames[0]["modelId"] != "gpt-model" || frames[1]["type"] != "set_thinking_level" || frames[1]["level"] != "high" || frames[2]["type"] != "get_state" || frames[3]["type"] != "prompt" {
					t.Fatalf("wrong control order: %+v", frames)
				}
			}
		})
	}
}

func TestFollowupClaudeRejectedControlDoesNotSendPrompt(t *testing.T) {
	pipe := &pipeProc{exited: make(chan struct{})}
	writes := 0
	pipe.stdin = followupWireWriter(func(b []byte) (int, error) {
		writes++
		var frame map[string]any
		if err := json.Unmarshal(b, &frame); err != nil {
			return 0, err
		}
		pipe.acceptControlResponse(jraw(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": frame["request_id"], "error": "unsupported model"}}), true)
		return len(b), nil
	})
	p := &claudeProc{pipeProc: pipe}
	if err := p.SendConfigured("must not send", "unknown", ""); err == nil || !strings.Contains(err.Error(), "unsupported model") {
		t.Fatal("rejected model was accepted", err)
	}
	if writes != 1 {
		t.Fatal("prompt sent after rejected control")
	}
}

func TestFollowupCodexOverridesTurnWithoutReplacingThread(t *testing.T) {
	p, _ := chatCodexFixture()
	p.calls = make(map[int64]chan rpcResp)
	var frame struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params map[string]any  `json:"params"`
	}
	p.stdin = followupWireWriter(func(b []byte) (int, error) {
		if err := json.Unmarshal(b, &frame); err != nil {
			return 0, err
		}
		p.dispatch(jraw(map[string]any{"id": frame.ID, "result": map[string]any{"turn": map[string]string{"id": "next-turn"}}}))
		return len(b), nil
	})
	if err := p.SendConfigured("next", "new-model", "high"); err != nil {
		t.Fatal(err)
	}
	if frame.Method != "turn/start" || frame.Params["threadId"] != "thread" || frame.Params["model"] != "new-model" || frame.Params["effort"] != "high" || p.effort != "high" {
		t.Fatalf("incorrect native turn overrides: %+v", frame)
	}
	if err := p.SendConfigured("busy", "other-model", "low"); err == nil {
		t.Fatal("configuration steered a busy Codex turn")
	}
}

func TestFollowupOMPDoesNotClaimClampedEffortWasApplied(t *testing.T) {
	pipe := &pipeProc{exited: make(chan struct{})}
	writes := 0
	pipe.stdin = followupWireWriter(func(b []byte) (int, error) {
		writes++
		var frame map[string]any
		if err := json.Unmarshal(b, &frame); err != nil {
			return 0, err
		}
		pipe.acceptControlResponse(jraw(map[string]any{"type": "response", "id": frame["id"], "success": true, "data": map[string]any{"thinkingLevel": "medium"}}), false)
		return len(b), nil
	})
	p := &ompProc{pipeProc: pipe}
	if err := p.SendConfigured("must not send", "", "high"); err == nil || !strings.Contains(err.Error(), "medium") {
		t.Fatal("silently clamped effort was reported as requested", err)
	}
	if writes != 2 {
		t.Fatal("prompt sent despite the effort mismatch")
	}
}

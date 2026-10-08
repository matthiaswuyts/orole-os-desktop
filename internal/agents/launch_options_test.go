package agents

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/machines"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func TestModelAndReasoningValidation(t *testing.T) {
	for _, value := range []string{"", "provider/model-v2", "custom model", "モデル"} {
		if err := validateModel(value); err != nil {
			t.Fatalf("valid model %q: %v", value, err)
		}
	}
	for _, value := range []string{strings.Repeat("x", 129), "a\nb", "a\x00b", "a\u202eb", string([]byte{0xff})} {
		if validateModel(value) == nil {
			t.Fatalf("accepted invalid model %q", value)
		}
	}
	for _, kind := range []string{"claude", "codex"} {
		for _, value := range reasoningValues(kind) {
			if err := validateReasoning(kind, value); err != nil {
				t.Fatal(err)
			}
		}
		if validateReasoning(kind, "invented") == nil {
			t.Fatal("invented reasoning accepted")
		}
	}
	if validateReasoning("gemini", "high") == nil {
		t.Fatal("unsupported harness reasoning accepted")
	}
}

func TestReasoningOptionsUseAdvertisedFlags(t *testing.T) {
	if got := advertisedReasoning("claude", "--model <model>"); len(got) != 0 {
		t.Fatalf("invented effort support: %v", got)
	}
	got := advertisedReasoning("claude", "--effort <level>\n Effort (low, medium, high)\n --model <model>")
	if strings.Join(got, ",") != "low,medium,high" {
		t.Fatalf("did not filter advertised values: %v", got)
	}
	if got := advertisedReasoning("codex", "no config support"); len(got) != 0 {
		t.Fatalf("invented config support: %v", got)
	}
}

func TestPTYProgrammaticPromptDoesNotWriteInput(t *testing.T) {
	// A nil terminal manager proves Send does not touch unrelated terminal stdin.
	if err := (&ptyProc{}).Send("execute this"); err == nil {
		t.Fatal("PTY accepted programmatic input")
	}
	if err := (&remoteProcess{}).Send("execute this"); err == nil {
		t.Fatal("one-shot remote process accepted follow-up input")
	}
}

func TestRemoteOutputIsBoundedAndDrained(t *testing.T) {
	ch := make(chan Event, 600)
	w := &remoteOutput{events: ch, tail: tailBuf{max: 16 << 10}}
	input := []byte(strings.Repeat("x", 1<<20))
	if n, err := w.Write(input); n != len(input) || err != nil {
		t.Fatalf("write: %d %v", n, err)
	}
	if w.bytes != 512<<10 || !w.truncated || len(w.tail.String()) != 16<<10 {
		t.Fatal("output retention was not bounded")
	}
	before := len(ch)
	_, _ = w.Write(input)
	if len(ch) != before {
		t.Fatal("truncated output continued emitting events")
	}
}

func TestLocalAdaptersRejectRemoteNamespaceBeforeDispatch(t *testing.T) {
	for _, adapter := range []Adapter{Claude{}, Codex(), OpenCode(), Gemini(), Custom{}} {
		if _, err := adapter.Start(context.Background(), nil, LaunchSpec{Machine: "remote-box"}); err == nil {
			t.Fatalf("%s accepted a remote namespace", adapter.Kind())
		}
	}
}

func TestRemoteLossCannotBeRecordedAsStopped(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "agents.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewManager(Config{Store: NewStore(db)})
	defer m.Shutdown()
	now := time.Now().UTC()
	s := Session{ID: "remote-lost", Kind: "claude", Machine: "remote-box", Mode: ModeProcess, Status: StatusWorking, StartedAt: now, UpdatedAt: now}
	ls := &liveSess{id: s.ID, s: s, stopping: true, shutdown: true}
	m.live[s.ID] = ls
	m.finalize(ls, Event{Kind: EvExit, Data: jraw(map[string]any{"code": -1, "uncertain": true, "stderr": "Remote process termination is unconfirmed"})})
	stored, err := m.Get(s.ID)
	if err != nil || stored.Status != StatusLost || stored.ExitCode != nil || stored.Live {
		t.Fatalf("transport loss lied about process termination: %+v %v", stored, err)
	}
	s.ID = "remote-dispatch-lost"
	stored, err = m.failStart(s, machines.ErrAgentDispatchUncertain)
	if err == nil || stored.Status != StatusLost {
		t.Fatalf("uncertain dispatch was not lost: %+v %v", stored, err)
	}
}

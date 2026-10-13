package agents

import (
	"sync"
	"testing"
)

// A turn that ends while the harness still runs background agents is not the task's end: the
// session stays working, Finished waits, and the task completes when the last agent reports.
func TestTurnDoneWaitsForBackgroundAgents(t *testing.T) {
	m, _, _ := persistFixture(t)
	var mu sync.Mutex
	var finished []Session
	m.cfg.Finished = func(s Session) {
		mu.Lock()
		finished = append(finished, s)
		mu.Unlock()
	}
	subagent := func(id, status string) {
		m.apply("s", Event{Kind: EvSubagent, Data: jraw(SubagentData{ID: id, Name: "scout", Status: status})})
	}
	m.apply("s", Event{Kind: EvWorking})
	subagent("a", "started")
	subagent("b", "started")
	subagent("a", "started") // repeated lifecycle frames count once
	s, _ := m.Get("s")
	if s.Subagents != 2 {
		t.Fatalf("subagents = %d, want 2", s.Subagents)
	}

	m.apply("s", Event{Kind: EvTurnDone, Data: jraw(map[string]any{"ok": true, "result": "progress note"})})
	s, _ = m.Get("s")
	if s.Status != StatusWorking || s.Turns != 1 || s.Result != "progress note" || s.Activity != backgroundActivity(2) {
		t.Fatalf("turn with background agents ended the task: %+v", s)
	}
	mu.Lock()
	n := len(finished)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("Finished fired %d times while agents still ran", n)
	}

	subagent("a", "completed")
	s, _ = m.Get("s")
	if s.Status != StatusWorking || s.Subagents != 1 || s.Activity != backgroundActivity(1) {
		t.Fatalf("one agent still running: %+v", s)
	}
	subagent("b", "failed")
	s, _ = m.Get("s")
	if s.Status != StatusDone || s.Subagents != 0 || s.Activity != "" {
		t.Fatalf("last agent reporting did not finish the task: %+v", s)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(finished) != 1 || finished[0].Status != StatusDone || finished[0].Result != "progress note" {
		t.Fatalf("Finished = %+v", finished)
	}
}

// A new turn (the harness delivering the agents' results) clears the hold; its own end finishes.
func TestFollowupTurnAfterBackgroundAgentsFinishesNormally(t *testing.T) {
	m, _, _ := persistFixture(t)
	var mu sync.Mutex
	count := 0
	m.cfg.Finished = func(Session) {
		mu.Lock()
		count++
		mu.Unlock()
	}
	m.apply("s", Event{Kind: EvWorking})
	m.apply("s", Event{Kind: EvSubagent, Data: jraw(SubagentData{ID: "a", Status: "started"})})
	m.apply("s", Event{Kind: EvTurnDone, Data: jraw(map[string]any{"ok": true, "result": "first"})})
	m.apply("s", Event{Kind: EvWorking}) // harness started the follow-up turn before the lifecycle frame
	m.apply("s", Event{Kind: EvSubagent, Data: jraw(SubagentData{ID: "a", Status: "completed"})})
	s, _ := m.Get("s")
	if s.Status != StatusWorking {
		t.Fatalf("agent report during a running turn ended it: %+v", s)
	}
	m.apply("s", Event{Kind: EvTurnDone, Data: jraw(map[string]any{"ok": true, "result": "final"})})
	s, _ = m.Get("s")
	mu.Lock()
	defer mu.Unlock()
	if s.Status != StatusDone || s.Result != "final" || count != 1 {
		t.Fatalf("follow-up turn: status=%s result=%q finished=%d", s.Status, s.Result, count)
	}
}

func TestSubagentRunningVocabulary(t *testing.T) {
	for status, want := range map[string]bool{"started": true, "running": true, "pendingInit": true, "completed": false, "failed": false, "errored": false, "shutdown": false, "": false} {
		if got := subagentRunning(status); got != want {
			t.Errorf("subagentRunning(%q) = %v", status, got)
		}
	}
}

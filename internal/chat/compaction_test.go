package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
)

// compactionScript answers summariser requests (recognised by their system prompt) with a fixed summary.
func compactionScript(_ int, req llm.Request) []llm.Delta {
	if req.System == summarySystem {
		return []llm.Delta{llmtest.Text("SUMMARY-OF-EARLIER-CHAT")}
	}
	return []llm.Delta{llmtest.Text("ok")}
}

func TestCompactionSummarisesOldestMessagesWithTheMemoryModel(t *testing.T) {
	e := newEnv(t, compactionScript)
	e.llm.models = []llm.Model{{ID: "m1", Name: "M1", Caps: llm.Caps{Tools: true, ContextWindow: 1000}}}
	e.llm.defaults.MemoryModel = "fake/memory-small"
	b := e.bot("Long")
	ctx := context.Background()

	var noticed bool
	for i := range 6 {
		e.send(b.ID, fmt.Sprintf("message-%d %s", i, strings.Repeat("filler ", 100)))
		e.idle(b.ID)
		if len(partsOfType(e.lastReply(b.ID).Parts, PartNotice)) > 0 {
			noticed = true
		}
	}
	if !noticed {
		t.Fatal("summarising must be announced with a notice part")
	}
	sm, err := e.m.store.getSummary(ctx, b.ID)
	if err != nil || sm.Text != "SUMMARY-OF-EARLIER-CHAT" || sm.UptoSeq == 0 {
		t.Fatalf("stored summary: %+v %v", sm, err)
	}

	var summariser, last llm.Request
	for _, c := range e.fake.Calls() {
		if c.System == summarySystem {
			if summariser.System == "" {
				summariser = c // the first compaction saw the oldest messages
			}
		} else {
			last = c
		}
	}
	if summariser.Model != "memory-small" || len(summariser.Tools) != 0 {
		t.Fatalf("the memory model must do the summarising without tools: %+v", summariser)
	}
	if in := summariser.Messages[0].Parts[0].Text; !strings.Contains(in, `<untrusted source="transcript">`) || !strings.Contains(in, "message-0") {
		t.Fatalf("transcript must be fenced and contain the oldest messages: %q", in)
	}

	// The next request carries the summary (fenced) and no longer the summarised messages.
	e.send(b.ID, "message-final")
	e.idle(b.ID)
	calls := e.fake.Calls()
	last = calls[len(calls)-1]
	if !strings.Contains(last.System, `<untrusted source="conversation_summary">`) || !strings.Contains(last.System, "SUMMARY-OF-EARLIER-CHAT") {
		t.Fatalf("system prompt must carry the fenced summary: %q", last.System)
	}
	for _, m := range last.Messages {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "message-0") {
				t.Fatal("summarised messages must not be sent again")
			}
		}
	}
	if !strings.Contains(last.Messages[len(last.Messages)-1].Parts[0].Text, "message-final") {
		t.Fatalf("current message missing: %+v", last.Messages)
	}

	// Nothing is deleted: the full conversation stays visible.
	if n := len(e.history(b.ID)); n != 14 {
		t.Fatalf("compaction must not delete messages: %d", n)
	}
}

func TestCompactionWithUnknownWindowTriggersOnMessageCount(t *testing.T) {
	e := newEnv(t, compactionScript) // testModel reports no context window
	b := e.bot("Counted")
	ctx := context.Background()
	for i := range unknownWindowMessages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msg := Message{ID: newID(), BotID: b.ID, Role: role, Author: role, Status: StatusComplete,
			Parts: []MsgPart{{Type: PartText, Text: fmt.Sprintf("old-%d", i)}}}
		if err := e.m.store.insertMessage(ctx, &msg); err != nil {
			t.Fatal(err)
		}
	}
	e.send(b.ID, "below-threshold")
	e.idle(b.ID)
	if sm, _ := e.m.store.getSummary(ctx, b.ID); sm.Text != "" {
		t.Fatalf("no summary expected at exactly the threshold: %+v", sm)
	}

	e.send(b.ID, "over-threshold")
	e.idle(b.ID)
	sm, err := e.m.store.getSummary(ctx, b.ID)
	if err != nil || sm.Text != "SUMMARY-OF-EARLIER-CHAT" {
		t.Fatalf("summary after exceeding %d messages: %+v %v", unknownWindowMessages, sm, err)
	}
	if len(partsOfType(e.lastReply(b.ID).Parts, PartNotice)) == 0 {
		t.Fatal("notice expected")
	}
}

func TestCompactionFailureKeepsTheConversationAndSaysSo(t *testing.T) {
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta {
		if req.System == summarySystem {
			return []llm.Delta{{Kind: llm.DeltaError, Err: &llm.Error{Code: llm.ErrAuth, Message: "no key"}}}
		}
		return []llm.Delta{llmtest.Text("ok")}
	})
	b := e.bot("Failing")
	ctx := context.Background()
	for i := range unknownWindowMessages + 2 {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msg := Message{ID: newID(), BotID: b.ID, Role: role, Author: role, Status: StatusComplete,
			Parts: []MsgPart{{Type: PartText, Text: "old"}}}
		if err := e.m.store.insertMessage(ctx, &msg); err != nil {
			t.Fatal(err)
		}
	}
	e.send(b.ID, "hello")
	e.idle(b.ID)
	r := e.lastReply(b.ID)
	if r.Status != StatusComplete || len(partsOfType(r.Parts, PartNotice)) == 0 {
		t.Fatalf("a failed summary must not fail the reply, but must be reported: %+v", r)
	}
	if sm, _ := e.m.store.getSummary(ctx, b.ID); sm.Text != "" {
		t.Fatalf("no summary may be stored: %+v", sm)
	}
	if n := len(e.history(b.ID)); n != unknownWindowMessages+2+2 {
		t.Fatalf("messages must stay: %d", n)
	}
}

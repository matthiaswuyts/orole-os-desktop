package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
)

func seedMessage(t *testing.T, e *env, botID, role string, status string, parts ...MsgPart) Message {
	t.Helper()
	msg := Message{ID: newID(), BotID: botID, Role: role, Author: role, Status: status, CreatedAt: 1, Parts: parts}
	if err := e.m.store.insertMessage(context.Background(), &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestHistoryProjectionMatchesTheFullMessageForTheModel(t *testing.T) {
	e := newEnv(t, compactionScript)
	b := e.bot("Lean")
	big := strings.Repeat("é", 6000) // multi-byte text well beyond the summariser's clip
	seeded := []Message{
		seedMessage(t, e, b.ID, "user", StatusComplete, MsgPart{Type: PartText, Text: "hello"}, MsgPart{Type: PartAttachment, Name: "notes.pdf", MIME: "application/pdf", AttachmentID: "a1", Size: 9}),
		seedMessage(t, e, b.ID, "assistant", StatusComplete,
			MsgPart{Type: PartReasoning, Text: strings.Repeat("secret thoughts ", 500)},
			MsgPart{Type: PartTool, ID: "t1", Name: "web_search", Status: "done", Summary: "searched"},
			MsgPart{Type: PartText, Text: "answer " + big},
			MsgPart{Type: PartCitation, URL: "https://example.com", Title: "t", Snippet: strings.Repeat("s", 4000)},
			MsgPart{Type: PartUI, Name: "table", Spec: strings.Repeat("{}", 5000)},
			MsgPart{Type: PartReport, Text: "# Report\nbody"},
			MsgPart{Type: PartQuestion, Text: "Which one?", Choices: []Choice{{Label: "A"}, {Label: "B"}}}),
		seedMessage(t, e, b.ID, "user", StatusComplete, MsgPart{Type: PartText, Text: setupKickoff, Hidden: true}),
		seedMessage(t, e, b.ID, "user", StatusCanceled, MsgPart{Type: PartText, Text: "never sent"}),
		seedMessage(t, e, b.ID, "assistant", StatusRunning, MsgPart{Type: PartText, Text: "partial"}),
	}
	tail, err := e.m.store.tail(context.Background(), b.ID, 0, seeded[len(seeded)-1].Seq+1, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.msgs) != len(seeded) || tail.older {
		t.Fatalf("loaded %d of %d messages (older=%v)", len(tail.msgs), len(seeded), tail.older)
	}
	for i, got := range tail.msgs {
		want, err := e.m.store.getMessage(context.Background(), seeded[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		if historyText(got) != historyText(want) || got.Role != want.Role || got.Author != want.Author || got.Status != want.Status || got.Seq != want.Seq {
			t.Errorf("message %d: projected %q (%s/%s/%s) differs from full %q", i, historyText(got), got.Role, got.Author, got.Status, historyText(want))
		}
		if tail.tokens[i] != messageTokens(want) {
			t.Errorf("message %d: cost %d, want %d", i, tail.tokens[i], messageTokens(want))
		}
		for _, p := range got.Parts {
			if p.Type == PartReasoning || p.Type == PartTool || p.Type == PartCitation || p.Type == PartUI {
				t.Errorf("message %d: a %s part was loaded for history", i, p.Type)
			}
		}
	}
	if !strings.Contains(historyText(tail.msgs[2]), "The user just created you") {
		t.Fatal("the hidden setup kickoff is part of the model's history and must survive the projection")
	}
}

func TestTranscriptClipsTextTheSameWayTheSummariserWould(t *testing.T) {
	e := newEnv(t, compactionScript)
	b := e.bot("Clip")
	long := strings.Repeat("日本語", 2000)
	m1 := seedMessage(t, e, b.ID, "user", StatusComplete, MsgPart{Type: PartText, Text: "short"}, MsgPart{Type: PartText, Text: long})
	m2 := seedMessage(t, e, b.ID, "assistant", StatusComplete, MsgPart{Type: PartText, Text: long})
	var got []Message
	for m, err := range e.m.store.transcript(context.Background(), b.ID, 0, m2.Seq) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, m)
	}
	if len(got) != 2 || got[0].Seq != m1.Seq {
		t.Fatalf("transcript = %+v", got)
	}
	full, _ := e.m.store.getMessage(context.Background(), m1.ID)
	clip := func(m Message) string {
		text := historyText(m)
		cut := summaryMessageBytes
		for cut > 0 && !isRuneStart(text[cut]) {
			cut--
		}
		return text[:cut]
	}
	if clip(got[0]) != clip(full) {
		t.Fatal("the clipped transcript text must cut exactly where the full text would")
	}
	if n := len(historyText(got[1])); n > 3*2001+8 {
		t.Fatalf("transcript kept %d bytes of a long reply", n)
	}
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func TestTailStopsAtTheTokenBudgetAndReportsOlderMessages(t *testing.T) {
	e := newEnv(t, compactionScript)
	b := e.bot("Budget")
	var last Message
	for i := range 300 {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		last = seedMessage(t, e, b.ID, role, StatusComplete, MsgPart{Type: PartText, Text: fmt.Sprintf("m%03d %s", i, strings.Repeat("x", 395))})
	}
	ctx := context.Background()
	tail, err := e.m.store.tail(ctx, b.ID, 0, last.Seq+1, 1000, 5000)
	if err != nil {
		t.Fatal(err)
	}
	// Each message costs about 104 tokens: the tail holds the newest ones up to and including the one that crosses 1000.
	if !tail.older || len(tail.msgs) < 9 || len(tail.msgs) > 11 || tail.total < 1000 {
		t.Fatalf("tail: %d messages, %d tokens, older=%v", len(tail.msgs), tail.total, tail.older)
	}
	if got := tail.msgs[len(tail.msgs)-1].Seq; got != last.Seq {
		t.Fatalf("newest loaded seq = %d, want %d", got, last.Seq)
	}
	for i := 1; i < len(tail.msgs); i++ {
		if tail.msgs[i].Seq <= tail.msgs[i-1].Seq {
			t.Fatal("tail must be ascending")
		}
	}
	n, err := e.m.store.countBetween(ctx, b.ID, 0, tail.msgs[0].Seq)
	if err != nil || n != 300-len(tail.msgs) {
		t.Fatalf("older count = %d, err %v", n, err)
	}
	capped, err := e.m.store.tail(ctx, b.ID, 0, last.Seq+1, 0, 7)
	if err != nil || len(capped.msgs) != 7 || !capped.older {
		t.Fatalf("row cap: %d messages, older=%v, err %v", len(capped.msgs), capped.older, err)
	}
	all, err := e.m.store.tail(ctx, b.ID, 0, last.Seq+1, 0, 1000)
	if err != nil || len(all.msgs) != 300 || all.older {
		t.Fatalf("unbounded: %d messages, older=%v, err %v", len(all.msgs), all.older, err)
	}
}

func TestCompactionSummarisesTheWholeBacklogEvenWhenOnlyTheTailIsLoaded(t *testing.T) {
	e := newEnv(t, compactionScript)
	e.llm.models = []llm.Model{{ID: "m1", Name: "M1", Caps: llm.Caps{Tools: true, ContextWindow: 2000}}}
	b := e.bot("Backlog")
	ctx := context.Background()
	const n = 450 // more than two transcript pages
	for i := range n {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		seedMessage(t, e, b.ID, role, StatusComplete, MsgPart{Type: PartText, Text: fmt.Sprintf("backlog-%03d %s", i, strings.Repeat("y", 100))})
	}
	e.send(b.ID, "now")
	e.idle(b.ID)

	var transcript strings.Builder
	var summaries int
	for _, c := range e.fake.Calls() {
		if c.System == summarySystem {
			summaries++
			transcript.WriteString(c.Messages[0].Parts[0].Text)
		}
	}
	if summaries == 0 {
		t.Fatal("no summary requested")
	}
	for _, i := range []int{0, 1, 199, 200, 201, 399, 400} {
		if !strings.Contains(transcript.String(), fmt.Sprintf("backlog-%03d", i)) {
			t.Errorf("message %d never reached the summariser: the older backlog must be summarised, not skipped", i)
		}
	}
	sm, err := e.m.store.getSummary(ctx, b.ID)
	if err != nil || sm.Text == "" || sm.UptoSeq < 400 {
		t.Fatalf("summary %+v err %v", sm, err)
	}
	if got, err := e.m.store.countBetween(ctx, b.ID, 0, 1<<40); err != nil || got != n+2 {
		t.Fatalf("nothing may be deleted: %d messages, err %v", got, err)
	}
}

func TestFailedSummaryIsNotRetriedEveryTurnAndBacksOffExponentially(t *testing.T) {
	var summariser int
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta {
		if req.System == summarySystem {
			summariser++
			return []llm.Delta{{Kind: llm.DeltaError, Err: &llm.Error{Code: llm.ErrRateLimit, Message: "slow down"}}}
		}
		return []llm.Delta{llmtest.Text("ok")}
	})
	b := e.bot("Backoff")
	for i := range unknownWindowMessages + 2 {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		seedMessage(t, e, b.ID, role, StatusComplete, MsgPart{Type: PartText, Text: "old"})
	}
	turn := func() Message {
		e.send(b.ID, "again")
		e.idle(b.ID)
		return e.lastReply(b.ID)
	}
	calls := func() int { return summariser }

	first := turn()
	if calls() != 1 || len(partsOfType(first.Parts, PartNotice)) == 0 {
		t.Fatalf("first turn: %d summariser calls, notices %+v", calls(), first.Parts)
	}
	for range 3 {
		r := turn()
		notices := partsOfType(r.Parts, PartNotice)
		if calls() != 1 || len(notices) == 0 || !strings.Contains(notices[0].Text, "could not be summarised") {
			t.Fatalf("within the backoff the summariser must not run and the user must still be told: calls %d, notices %+v", calls(), notices)
		}
	}
	e.advance(summaryRetryBase + time.Second)
	turn()
	if calls() != 2 {
		t.Fatalf("after the first backoff: %d calls, want a retry", calls())
	}
	e.advance(summaryRetryBase + time.Second) // the second wait is twice as long
	turn()
	if calls() != 2 {
		t.Fatalf("the second backoff must be longer than the first: %d calls", calls())
	}
	e.advance(summaryRetryBase)
	turn()
	if calls() != 3 {
		t.Fatalf("after the doubled backoff: %d calls, want a retry", calls())
	}
	if sm, _ := e.m.store.getSummary(context.Background(), b.ID); sm.Text != "" {
		t.Fatalf("no summary may be stored: %+v", sm)
	}
}

func TestSummaryBackoffClearsAfterASuccessAndIsPerBot(t *testing.T) {
	fail := true
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta {
		if req.System == summarySystem {
			if fail {
				return []llm.Delta{{Kind: llm.DeltaError, Err: &llm.Error{Code: llm.ErrServer, Message: "down"}}}
			}
			return []llm.Delta{llmtest.Text("SUMMARY")}
		}
		return []llm.Delta{llmtest.Text("ok")}
	})
	seed := func(b Bot) {
		for i := range unknownWindowMessages + 2 {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			seedMessage(t, e, b.ID, role, StatusComplete, MsgPart{Type: PartText, Text: "old"})
		}
	}
	a, c := e.bot("A"), e.bot("C")
	seed(a)
	e.send(a.ID, "hi")
	e.idle(a.ID)
	if _, wait := e.m.summaryBackoff(a.ID); !wait {
		t.Fatal("a failure must start a backoff")
	}
	if _, wait := e.m.summaryBackoff(c.ID); wait {
		t.Fatal("another bot must not inherit the backoff")
	}
	fail = false
	seed(c)
	e.send(c.ID, "hi")
	e.idle(c.ID)
	if sm, _ := e.m.store.getSummary(context.Background(), c.ID); sm.Text != "SUMMARY" {
		t.Fatalf("the other bot must summarise normally: %+v", sm)
	}
	e.advance(summaryRetryBase + time.Second)
	e.send(a.ID, "again")
	e.idle(a.ID)
	if _, wait := e.m.summaryBackoff(a.ID); wait {
		t.Fatal("a successful summary must clear the backoff")
	}
	if len(e.m.sumFail) != 0 {
		t.Fatalf("backoff state leaked: %+v", e.m.sumFail)
	}
}

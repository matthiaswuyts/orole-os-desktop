package chat

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// wire is what a subscriber sees: snapshots and deltas in publish order.
type wire struct {
	mu     sync.Mutex
	events []any // Message | MessageDelta
}

func (w *wire) snap(m Message) { w.mu.Lock(); w.events = append(w.events, m); w.mu.Unlock() }
func (w *wire) delta(d MessageDelta) {
	w.mu.Lock()
	w.events = append(w.events, d)
	w.mu.Unlock()
}

// replay applies the events the way the frontend store does and returns the resulting message.
func (w *wire) replay(t *testing.T) Message {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	var cur Message
	for i, ev := range w.events {
		switch e := ev.(type) {
		case Message:
			cur = e
			cur.Parts = append([]MsgPart(nil), e.Parts...)
		case MessageDelta:
			if e.Part >= len(cur.Parts) || cur.Parts[e.Part].Type != e.Type || utf16Len(cur.Parts[e.Part].Text) != e.From {
				t.Fatalf("event %d: delta %+v does not line up with the replayed state", i, e)
			}
			cur.Parts[e.Part].Text += e.Text
		}
	}
	return cur
}

func fakeClock() (func() time.Time, func(time.Duration)) {
	now := time.Unix(1000, 0)
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

func TestSinkStreamsTextAsDeltasAndReplaysToTheFinalMessage(t *testing.T) {
	var w wire
	s := newSink(Message{ID: "m", BotID: "b", Role: "assistant", Status: StatusRunning}, nil, w.snap, w.delta)
	now, advance := fakeClock()
	s.now = now

	var want strings.Builder
	feed := func(kind, text string) {
		advance(publishInterval)
		s.appendText(kind, text)
		if kind == PartText {
			want.WriteString(text)
		}
	}
	feed(PartReasoning, "thinking")
	feed(PartReasoning, " hard")
	feed(PartText, "Hello ")
	feed(PartText, "wörld ")
	feed(PartText, "😀 ")
	feed(PartText, "again")
	s.upsertTool(MsgPart{Type: PartTool, ID: "t", Name: "web_search", Status: "running"})
	feed(PartText, " after the tool") // a new text part: the tool row ends the previous one
	if err := s.finish(StatusComplete, ""); err != nil {
		t.Fatal(err)
	}

	got := w.replay(t)
	if got.Status != StatusComplete || len(got.Parts) != 4 {
		t.Fatalf("replayed message: %+v", got)
	}
	if got.Parts[0].Text != "thinking hard" || got.Parts[1].Text != "Hello wörld 😀 again" || got.Parts[3].Text != " after the tool" {
		t.Fatalf("parts: %+v", got.Parts)
	}
	var snaps, deltas int
	for _, ev := range w.events {
		switch ev.(type) {
		case Message:
			snaps++
		case MessageDelta:
			deltas++
		}
	}
	if deltas < 4 {
		t.Fatalf("only %d deltas (%d snapshots): text must stream as deltas", deltas, snaps)
	}
	if _, ok := w.events[len(w.events)-1].(Message); !ok {
		t.Fatal("the final event must be a full snapshot")
	}
	if want.String() != got.Parts[1].Text+got.Parts[3].Text {
		t.Fatalf("text %q vs %q", want.String(), got.Parts[1].Text+got.Parts[3].Text)
	}
}

func TestSinkNonTextChangesAndCheckpointsAreFullSnapshots(t *testing.T) {
	var w wire
	s := newSink(Message{ID: "m", BotID: "b", Status: StatusRunning}, nil, w.snap, w.delta)
	now, advance := fakeClock()
	s.now = now
	advance(publishInterval)
	s.appendText(PartText, "abc") // new part: snapshot
	advance(publishInterval)
	s.appendText(PartText, "def") // delta
	advance(publishInterval)
	one := int64(1)
	s.addUsage(llm.Usage{InputTokens: &one}) // usage cannot ride a delta: no event of its own, the next one is a snapshot
	advance(publishInterval)
	s.appendText(PartText, "ghi") // full snapshot carrying the usage
	advance(publishInterval)
	s.appendText(PartText, "jkl") // delta
	advance(publishInterval)
	s.addCitation(llm.Citation{URL: "https://example.com"})
	advance(publishInterval)
	if err := s.checkpoint(); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range w.events {
		switch ev.(type) {
		case Message:
			kinds = append(kinds, "S")
		case MessageDelta:
			kinds = append(kinds, "D")
		}
	}
	if got := strings.Join(kinds, ""); got != "SDSDSS" {
		t.Fatalf("event kinds = %s, want SDSDSS", got)
	}
	last := w.replay(t)
	if last.Parts[0].Text != "abcdefghijkl" || last.Usage == nil || len(last.Parts) != 2 {
		t.Fatalf("replayed: %+v", last)
	}
}

func TestSinkSendsAFullSnapshotAtLeastEveryResyncInterval(t *testing.T) {
	var w wire
	s := newSink(Message{ID: "m", BotID: "b", Status: StatusRunning}, nil, w.snap, w.delta)
	now, advance := fakeClock()
	s.now = now
	for range int(3*resyncInterval/publishInterval) + 2 {
		advance(publishInterval)
		s.appendText(PartText, "x")
	}
	var snaps int
	for _, ev := range w.events {
		if _, ok := ev.(Message); ok {
			snaps++
		}
	}
	if snaps < 3 { // the first, then one per resync interval
		t.Fatalf("%d snapshots over 3 resync intervals: a subscriber that missed a delta would never recover", snaps)
	}
	w.replay(t)
}

func TestSinkWithoutADeltaPublisherSendsOnlySnapshots(t *testing.T) {
	var w wire
	s := newSink(Message{ID: "m", BotID: "b", Status: StatusRunning}, nil, w.snap, nil)
	now, advance := fakeClock()
	s.now = now
	for range 5 {
		advance(publishInterval)
		s.appendText(PartText, "ab")
	}
	if len(w.events) != 5 {
		t.Fatalf("%d events, want a snapshot per interval", len(w.events))
	}
	if got := w.replay(t).Parts[0].Text; got != strings.Repeat("ab", 5) {
		t.Fatalf("text = %q", got)
	}
}

func TestSinkPersistsAndReturnsTheFullTextFromTheBuilder(t *testing.T) {
	var stored Message
	s := newSink(Message{ID: "m", Status: StatusRunning}, func(m Message) error { stored = m; return nil }, nil, nil)
	for range 1000 {
		s.appendText(PartText, "token ")
	}
	s.addPart(MsgPart{Type: PartNotice, Text: "n"})
	s.appendText(PartText, "tail")
	if err := s.finish(StatusComplete, ""); err != nil {
		t.Fatal(err)
	}
	if got := stored.Parts[0].Text; got != strings.Repeat("token ", 1000) || stored.Parts[2].Text != "tail" {
		t.Fatalf("stored %d bytes / %q", len(got), stored.Parts[2].Text)
	}
	if got := s.finalText(); got != "tail" {
		t.Fatalf("finalText = %q", got)
	}
}

func TestUTF16Len(t *testing.T) {
	for in, want := range map[string]int{"": 0, "abc": 3, "wörld": 5, "😀": 2, "a😀b": 4, "\xff": 1, "日本": 2} {
		if got := utf16Len(in); got != want {
			t.Errorf("utf16Len(%q) = %d, want %d", in, got, want)
		}
	}
}

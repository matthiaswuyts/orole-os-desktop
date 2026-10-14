package chat

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

const (
	publishInterval = 50 * time.Millisecond
	// resyncInterval bounds how long a subscriber that missed a delta (it opened the chat mid-reply,
	// or the event bus dropped one) can show stale text: a full snapshot goes out at least this often.
	resyncInterval = 2 * time.Second
)

// sink builds the assistant message incrementally, persists it at checkpoints and publishes it
// coalesced to at most one event per publishInterval. The final snapshot is always published.
//
// Streamed text is published as MessageDelta events that carry only the appended text; everything
// else (new parts, tool rows, citations, usage, checkpoints, the final state) and a periodic resync
// are full snapshots. Without a delta publisher every event is a full snapshot.
type sink struct {
	mu           sync.Mutex
	msg          Message
	persist      func(Message) error // nil: not stored (drafts, delegated runs)
	publish      func(Message)       // nil: silent; must not call back into the sink
	publishDelta func(MessageDelta)  // nil: stream text as full snapshots
	now          func() time.Time    // test seam; time.Now
	clock        func() time.Time    // stamps FinishedAt; the manager's clock, time.Now by default

	lastPub  time.Time
	lastFull time.Time
	timer    *time.Timer
	done     bool
	gen      uint64 // bumped whenever the pending timer is cancelled or fires, so stale callbacks do nothing

	// buf accumulates the text of the trailing text/reasoning part (Parts[bufPart]) so appending a
	// token is O(token), not O(reply). Parts[bufPart].Text always aliases buf's current contents.
	buf     strings.Builder
	bufPart int
	bufU16  int // length of buf in UTF-16 units, the unit a JavaScript string reports

	// dirty: something other than appended text changed since the last full snapshot, so the next
	// publish is a full one. pend is the text appended to Parts[bufPart] since the last publish and
	// pendFrom its starting offset in UTF-16 units.
	dirty    bool
	pend     strings.Builder
	pendFrom int
}

func newSink(msg Message, persist func(Message) error, publish func(Message), publishDelta func(MessageDelta)) *sink {
	if msg.Parts == nil {
		msg.Parts = []MsgPart{}
	}
	return &sink{msg: msg, persist: persist, publish: publish, publishDelta: publishDelta, now: time.Now, clock: time.Now, bufPart: -1, dirty: true}
}

// snapshotLocked deep-copies the message (parts and usage are mutated in place by the sink).
func (s *sink) snapshotLocked() Message {
	m := s.msg
	m.Parts = slices.Clone(s.msg.Parts)
	if s.msg.Usage != nil {
		u := *s.msg.Usage
		m.Usage = &u
	}
	return m
}

func (s *sink) snapshot() Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// publishLocked sends a full snapshot.
func (s *sink) publishLocked() {
	if s.publish == nil {
		return
	}
	s.stopTimerLocked() // a snapshot just went out: any pending scheduled one is redundant
	s.lastPub = s.now()
	s.lastFull = s.lastPub
	s.dirty = false
	s.pend.Reset()
	s.publish(s.snapshotLocked())
}

// flushLocked publishes what changed since the last event: the appended text when that is all there
// is, else a full snapshot.
func (s *sink) flushLocked() {
	now := s.now()
	if s.publishDelta == nil || s.dirty || s.pend.Len() == 0 || now.Sub(s.lastFull) >= resyncInterval {
		s.publishLocked()
		return
	}
	s.stopTimerLocked()
	s.lastPub = now
	d := MessageDelta{BotID: s.msg.BotID, MessageID: s.msg.ID, Part: s.bufPart, Type: s.msg.Parts[s.bufPart].Type, From: s.pendFrom, Text: s.pend.String()}
	s.pend.Reset()
	s.publishDelta(d)
}

// touchLocked publishes now when the last publish is old enough, else schedules one.
func (s *sink) touchLocked() {
	if s.publish == nil || s.done {
		return
	}
	since := s.now().Sub(s.lastPub)
	if since >= publishInterval {
		s.flushLocked()
		return
	}
	if s.timer == nil {
		gen := s.gen
		s.timer = time.AfterFunc(publishInterval-since, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.gen != gen || s.done { // already stopped or superseded by a direct publish
				return
			}
			s.flushLocked()
		})
	}
}

// changedLocked records a change that a delta cannot express and schedules a full snapshot.
func (s *sink) changedLocked() {
	s.dirty = true
	s.pend.Reset()
	s.touchLocked()
}

func (s *sink) stopTimerLocked() {
	s.gen++
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

// appendText extends the trailing text/reasoning part or starts a new one.
func (s *sink) appendText(kind, text string) {
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.msg.Parts)
	if n == 0 || s.msg.Parts[n-1].Type != kind {
		s.msg.Parts = append(s.msg.Parts, MsgPart{Type: kind, Text: text})
		s.buf.Reset()
		s.buf.WriteString(text)
		s.bufPart, s.bufU16 = n, utf16Len(text)
		s.changedLocked()
		return
	}
	last := &s.msg.Parts[n-1]
	if s.bufPart != n-1 || s.buf.Len() != len(last.Text) {
		// The part was not built by the buffer (it pre-existed, or was replaced): continue from its text.
		s.buf.Reset()
		s.buf.WriteString(last.Text)
		s.bufPart, s.bufU16 = n-1, utf16Len(last.Text)
		s.dirty = true
		s.pend.Reset()
	}
	if s.pend.Len() == 0 {
		s.pendFrom = s.bufU16
	}
	s.buf.WriteString(text)
	s.bufU16 += utf16Len(text)
	last.Text = s.buf.String()
	if !s.dirty {
		s.pend.WriteString(text)
	}
	s.touchLocked()
}

// utf16Len is the length JavaScript reports for s.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}

// addPart appends a UI part.
func (s *sink) addPart(p MsgPart) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg.Parts = append(s.msg.Parts, p)
	s.changedLocked()
}

// addCitation appends a citation unless the URL is already cited on this message.
func (s *sink) addCitation(c llm.Citation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.msg.Parts {
		if p.Type == PartCitation && p.URL == c.URL {
			return
		}
	}
	s.msg.Parts = append(s.msg.Parts, MsgPart{Type: PartCitation, URL: c.URL, Title: c.Title, Snippet: c.Snippet})
	s.changedLocked()
}

// upsertTool inserts a part, or replaces the part with the same Type and ID (parts without an ID
// always append). RunContext.Emit is bound to it so tools can update live status rows in place.
func (s *sink) upsertTool(p MsgPart) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := slices.IndexFunc(s.msg.Parts, func(x MsgPart) bool { return x.Type == p.Type && x.ID == p.ID }); i >= 0 && p.ID != "" {
		s.msg.Parts[i] = p
	} else {
		s.msg.Parts = append(s.msg.Parts, p)
	}
	s.changedLocked()
}

func (s *sink) setModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg.Model = model
	s.changedLocked()
}

// addUsage accumulates provider-reported usage across the steps of a run.
func (s *sink) addUsage(u llm.Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msg.Usage == nil {
		s.msg.Usage = &MessageUsage{}
	}
	s.msg.Usage.InputTokens = addInt(s.msg.Usage.InputTokens, u.InputTokens)
	s.msg.Usage.OutputTokens = addInt(s.msg.Usage.OutputTokens, u.OutputTokens)
	if u.CostUSD != nil {
		c := *u.CostUSD
		if s.msg.Usage.CostUSD != nil {
			c += *s.msg.Usage.CostUSD
		}
		s.msg.Usage.CostUSD = &c
	}
	s.dirty = true
	s.pend.Reset()
}

func addInt(acc, v *int64) *int64 {
	if v == nil {
		return acc
	}
	n := *v
	if acc != nil {
		n += *acc
	}
	return &n
}

// checkpoint persists and publishes a full snapshot immediately (tool boundaries).
func (s *sink) checkpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopTimerLocked()
	var err error
	if s.persist != nil {
		err = s.persist(s.snapshotLocked())
	}
	if !s.done {
		s.publishLocked()
	}
	return err
}

// finish stores the terminal state and publishes the final snapshot. A final status records the
// finish time once.
func (s *sink) finish(status, errText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopTimerLocked()
	s.msg.Status = status
	s.msg.Error = errText
	if finalStatus(status) && s.msg.FinishedAt == 0 {
		s.msg.FinishedAt = s.clock().UnixMilli()
	}
	var err error
	if s.persist != nil {
		err = s.persist(s.snapshotLocked())
	}
	s.done = true
	if s.publish != nil {
		s.publishLocked()
	}
	return err
}

// finalText is the last text part, used as the answer of a delegated run.
func (s *sink) finalText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.msg.Parts) - 1; i >= 0; i-- {
		if s.msg.Parts[i].Type == PartText {
			return s.msg.Parts[i].Text
		}
	}
	return ""
}

package chat

import (
	"slices"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

const publishInterval = 50 * time.Millisecond

// sink builds the assistant message incrementally, persists it at checkpoints and publishes
// snapshots coalesced to at most one per publishInterval. The final snapshot is always published.
type sink struct {
	mu      sync.Mutex
	msg     Message
	persist func(Message) error // nil: not stored (drafts, delegated runs)
	publish func(Message)       // nil: silent; must not call back into the sink

	lastPub time.Time
	timer   *time.Timer
	done    bool
}

func newSink(msg Message, persist func(Message) error, publish func(Message)) *sink {
	if msg.Parts == nil {
		msg.Parts = []MsgPart{}
	}
	return &sink{msg: msg, persist: persist, publish: publish}
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

func (s *sink) publishLocked() {
	if s.publish == nil {
		return
	}
	s.lastPub = time.Now()
	s.publish(s.snapshotLocked())
}

// touchLocked publishes now when the last publish is old enough, else schedules one.
func (s *sink) touchLocked() {
	if s.publish == nil || s.done {
		return
	}
	since := time.Since(s.lastPub)
	if since >= publishInterval {
		s.publishLocked()
		return
	}
	if s.timer == nil {
		s.timer = time.AfterFunc(publishInterval-since, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.timer = nil
			if !s.done {
				s.publishLocked()
			}
		})
	}
}

func (s *sink) stopTimerLocked() {
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
	if n := len(s.msg.Parts); n > 0 && s.msg.Parts[n-1].Type == kind {
		s.msg.Parts[n-1].Text += text
	} else {
		s.msg.Parts = append(s.msg.Parts, MsgPart{Type: kind, Text: text})
	}
	s.touchLocked()
}

// addPart appends a UI part.
func (s *sink) addPart(p MsgPart) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg.Parts = append(s.msg.Parts, p)
	s.touchLocked()
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
	s.touchLocked()
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
	s.touchLocked()
}

func (s *sink) setModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg.Model = model
	s.touchLocked()
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

// checkpoint persists and publishes immediately (tool boundaries).
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

// finish stores the terminal state and publishes the final snapshot.
func (s *sink) finish(status, errText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopTimerLocked()
	s.msg.Status = status
	s.msg.Error = errText
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

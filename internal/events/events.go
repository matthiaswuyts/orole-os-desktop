// Package events is the daemon's in-process application event bus.
//
// Modules publish facts ("agent.finished", "github.ci.completed", "mcp.tool.called",
// "app.started", ...) and other modules, notably the automation engine, subscribe
// by topic pattern. Delivery is best-effort and never blocks the publisher: every
// subscription owns a bounded queue, and when it is full the newest event is dropped
// and counted. Event data is untrusted content when it originates from outside the
// machine (issue titles, mail subjects, MCP results); subscribers treat it as data.
//
// Provenance: an event carries Chain, the ordered automation IDs whose runs
// (directly or indirectly) caused it. Code that publishes events as a consequence
// of an automation step reads the chain from its context (ChainFrom) and copies it
// into the event, which is how the automation engine refuses loops.
package events

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Event is one published fact.
type Event struct {
	Topic string         `json:"topic"`
	Time  time.Time      `json:"time"`
	Data  map[string]any `json:"data,omitempty"`
	// Chain lists the automation IDs that caused this event, oldest first.
	Chain []string `json:"chain,omitempty"`
}

// DefaultQueue is the per-subscription queue bound used when none is given.
const DefaultQueue = 256

// Bus fans events out to subscriptions.
type Bus struct {
	mu   sync.RWMutex
	subs map[*Subscription]struct{}
	now  func() time.Time
}

// NewBus returns an empty bus.
func NewBus() *Bus { return &Bus{subs: map[*Subscription]struct{}{}, now: time.Now} }

// Subscription receives matching events on C until Close.
type Subscription struct {
	C       <-chan Event
	ch      chan Event
	pattern string
	bus     *Bus
	dropped atomic.Uint64
	once    sync.Once
}

// Subscribe registers a subscription. pattern is an exact topic, "prefix.*"
// (matches prefix and anything below it), or "*" for everything. queue <= 0
// selects DefaultQueue.
func (b *Bus) Subscribe(pattern string, queue int) *Subscription {
	if queue <= 0 {
		queue = DefaultQueue
	}
	ch := make(chan Event, queue)
	s := &Subscription{C: ch, ch: ch, pattern: pattern, bus: b}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Dropped is the number of events discarded because the queue was full.
func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

// Close unsubscribes. The channel is closed after the last queued event.
func (s *Subscription) Close() {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s)
		s.bus.mu.Unlock()
		// No publisher can still hold s: Publish holds the read lock while sending.
		close(s.ch)
	})
}

// Publish delivers ev to every matching subscription without blocking. A zero
// Time is filled in. The event's Data map and Chain slice are shared with
// subscribers and must not be mutated after publishing.
func (b *Bus) Publish(ev Event) {
	if ev.Topic == "" {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = b.now()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if !Match(s.pattern, ev.Topic) {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			s.dropped.Add(1)
		}
	}
}

// Match reports whether topic matches pattern ("*", "prefix.*", or exact).
func Match(pattern, topic string) bool {
	switch {
	case pattern == "*":
		return true
	case strings.HasSuffix(pattern, ".*"):
		p := strings.TrimSuffix(pattern, ".*")
		return topic == p || strings.HasPrefix(topic, p+".")
	default:
		return pattern == topic
	}
}

// ValidPattern reports whether pattern is a usable subscription pattern.
func ValidPattern(pattern string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == "" || len(pattern) > 128 || strings.ContainsAny(pattern, " \t\r\n") {
		return false
	}
	p := strings.TrimSuffix(pattern, ".*")
	return p != "" && !strings.Contains(p, "*")
}

type chainKey struct{}

// WithChain returns a context carrying the automation provenance chain.
func WithChain(ctx context.Context, chain []string) context.Context {
	return context.WithValue(ctx, chainKey{}, append([]string(nil), chain...))
}

// ChainFrom returns the provenance chain in ctx (nil when none). Modules that
// publish events as a consequence of an automation-driven call copy it into
// Event.Chain.
func ChainFrom(ctx context.Context) []string {
	c, _ := ctx.Value(chainKey{}).([]string)
	return append([]string(nil), c...)
}

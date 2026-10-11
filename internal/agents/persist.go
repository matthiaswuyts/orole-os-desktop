package agents

import (
	"log/slog"
	"sync"
	"time"
)

// sessionFlushDelay is the longest a hot-path change to a live session stays only in memory.
// Status changes, exits, shutdown and every durable mutation flush immediately, so what a
// daemon killed outright can lose is at most this much display state (activity, tail, last
// message): never a transition, a queue entry or a pending request.
const sessionFlushDelay = 250 * time.Millisecond

// sessionWriter writes live sessions to SQLite outside Manager.mu.
//
// Callers snapshot a session under Manager.mu and hand it over with mark; the writer owns the
// snapshot from then on. Writes are coalesced per session: a burst of hot events produces one
// write per sessionFlushDelay carrying the newest state. The newest snapshot is always taken
// under writeMu, so concurrent flushes can never write an older state over a newer one.
type sessionWriter struct {
	st  *Store
	log *slog.Logger

	writeMu sync.Mutex // serialises writes; newest snapshot is taken while holding it

	mu     sync.Mutex // guards the fields below; never held across a database call
	dirty  map[string]*dirtySession
	closed bool
}

type dirtySession struct {
	s     Session
	timer *time.Timer
}

func newSessionWriter(st *Store, log *slog.Logger) *sessionWriter {
	return &sessionWriter{st: st, log: log, dirty: map[string]*dirtySession{}}
}

// persistable detaches a snapshot from the live session: slices the manager edits in place
// are copied, and runtime-only state the store never keeps is dropped.
func persistable(s Session) Session {
	s.Pending, s.Questions = nil, nil
	s.Queue = append([]QueuedPrompt(nil), s.Queue...)
	s.Usage = cloneUsage(s.Usage)
	s.Chain = append([]string(nil), s.Chain...)
	return s
}

// mark records s as the newest state of its session and schedules the debounced write. It
// returns false when the writer is closed; the caller then flushes immediately.
func (w *sessionWriter) mark(s Session) bool {
	s = persistable(s)
	w.mu.Lock()
	defer w.mu.Unlock()
	d := w.dirty[s.ID]
	if d == nil {
		d = &dirtySession{}
		w.dirty[s.ID] = d
	}
	d.s = s
	if w.closed {
		return false
	}
	if d.timer == nil {
		id := s.ID
		d.timer = time.AfterFunc(sessionFlushDelay, func() {
			if err := w.flush(id); err != nil {
				w.log.Warn("agent session write failed", "session", id, "err", err)
			}
		})
	}
	return true
}

// take removes and returns the pending snapshot of id.
func (w *sessionWriter) take(id string) (Session, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := w.dirty[id]
	if d == nil {
		return Session{}, false
	}
	if d.timer != nil {
		d.timer.Stop()
	}
	delete(w.dirty, id)
	return d.s, true
}

// flush writes the pending snapshot of id, if any.
func (w *sessionWriter) flush(id string) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	s, ok := w.take(id)
	if !ok {
		return nil
	}
	return w.st.SaveSession(s, "")
}

// write saves s now and discards any older pending snapshot of the session. It is for
// callers that must know the write succeeded before they proceed.
func (w *sessionWriter) write(s Session) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	w.take(s.ID)
	return w.st.SaveSession(s, "")
}

// forget drops a pending snapshot without writing it (the session was deleted).
func (w *sessionWriter) forget(id string) {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	w.take(id)
}

// close writes everything pending and makes later marks flush on the caller's goroutine.
func (w *sessionWriter) close() error {
	w.mu.Lock()
	w.closed = true
	ids := make([]string, 0, len(w.dirty))
	for id := range w.dirty {
		ids = append(ids, id)
	}
	w.mu.Unlock()
	var first error
	for _, id := range ids {
		if err := w.flush(id); err != nil && first == nil {
			first = err
		}
	}
	return first
}

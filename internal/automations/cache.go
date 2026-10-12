package automations

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/automations/expr"
	"github.com/rayorole/orole-os-desktop/internal/events"
)

var errCredentialsUnavailable = errors.New("OS credential store unavailable")

// defEntry is one saved definition with everything the hot paths derive from it
// parsed once. The immutable fields are shared across goroutines; the schedule
// bookkeeping (last, next, nextOK) is touched only by the scheduler goroutine.
type defEntry struct {
	def    Definition
	raw    string // the stored JSON, reused as the run's definition snapshot
	issues []Issue
	order  int // position in name order, so event dispatch stays deterministic

	filter    *expr.Expr // parsed event/webhook trigger filter, nil when absent or invalid
	filterBad bool
	sched     Schedule // parsed schedule, nil when absent or invalid

	created int64
	last    int64
	next    time.Time
	hasNext bool
	nextOK  bool
}

// defSnapshot is an immutable index over every saved definition. Save and Delete
// drop it; the next reader rebuilds it from one query.
type defSnapshot struct {
	byID      map[string]*defEntry
	exact     map[string][]*defEntry // enabled event triggers by literal topic
	wildcards []*defEntry            // enabled event triggers using "*" or "prefix.*"
	schedules []*defEntry            // enabled schedule triggers
	watches   []*defEntry            // enabled, valid watch triggers
}

func (e *Engine) invalidateDefinitions() {
	e.defMu.Lock()
	e.defSnap = nil
	e.defMu.Unlock()
	select {
	case e.watchWake <- struct{}{}:
	default:
	}
}

func (e *Engine) snapshot(ctx context.Context) (*defSnapshot, error) {
	e.defMu.Lock()
	defer e.defMu.Unlock()
	if e.defSnap != nil {
		return e.defSnap, nil
	}
	rows, err := e.db.QueryContext(ctx, `SELECT definition,last_fire_at,created_at FROM automations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	s := &defSnapshot{byID: map[string]*defEntry{}, exact: map[string][]*defEntry{}}
	for rows.Next() {
		var raw string
		it := &defEntry{order: len(s.byID)}
		if err = rows.Scan(&raw, &it.last, &it.created); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(raw), &it.def) != nil {
			continue
		}
		if it.def.SchemaVersion != SchemaVersion {
			// The run snapshot must be the document that runs, so store the upgraded form.
			it.def = Upgrade(it.def)
			b, err := json.Marshal(it.def)
			if err != nil {
				continue
			}
			raw = string(b)
		}
		it.raw = raw
		it.issues = Validate(it.def)
		s.byID[it.def.ID] = it
		if !it.def.Enabled {
			continue
		}
		t := it.def.Trigger
		if t.Filter != "" && (t.Type == "event" || t.Type == "webhook" || t.Type == "watch") {
			if it.filter, err = expr.Parse(t.Filter); err != nil {
				it.filter, it.filterBad = nil, true
			}
		}
		switch t.Type {
		case "event":
			if t.Event == "*" || strings.HasSuffix(t.Event, ".*") {
				s.wildcards = append(s.wildcards, it)
			} else {
				s.exact[t.Event] = append(s.exact[t.Event], it)
			}
		case "schedule":
			if sc, err := ParseSchedule(t.Schedule, nil); err == nil {
				it.sched = sc
				s.schedules = append(s.schedules, it)
			}
		case "watch":
			if len(it.issues) == 0 && !it.filterBad && t.Watch != nil {
				s.watches = append(s.watches, it)
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	e.defSnap = s
	return s, nil
}

// matching returns the enabled event automations whose pattern covers topic, in
// name order. A topic nothing subscribes to costs one map lookup.
func (s *defSnapshot) matching(topic string) []*defEntry {
	exact := s.exact[topic]
	var out []*defEntry
	for _, it := range s.wildcards {
		if events.Match(it.def.Trigger.Event, topic) {
			out = append(out, it)
		}
	}
	if out == nil {
		return exact
	}
	out = append(out, exact...)
	slices.SortFunc(out, func(a, b *defEntry) int { return a.order - b.order })
	return out
}

// definitionEntry returns the cached, parsed definition for id.
func (e *Engine) definitionEntry(ctx context.Context, id string) (*defEntry, error) {
	s, err := e.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	it := s.byID[id]
	if it == nil {
		return nil, ErrNotFound
	}
	return it, nil
}

func (e *Engine) definition(ctx context.Context, id string) (Definition, error) {
	it, err := e.definitionEntry(ctx, id)
	if err != nil {
		return Definition{}, err
	}
	return it.def, nil
}

// webhookSecret returns the HMAC key, reading the OS credential store only on
// the first use after start or rotation.
func (e *Engine) webhookSecret() ([]byte, error) {
	e.secretMu.RLock()
	s := e.secret
	e.secretMu.RUnlock()
	if s != nil {
		return s, nil
	}
	if e.credentials == nil {
		return nil, errCredentialsUnavailable
	}
	b, err := e.credentials.Get(webhookCredential)
	if err != nil {
		return nil, err
	}
	e.secretMu.Lock()
	e.secret = b
	e.secretMu.Unlock()
	return b, nil
}

func (e *Engine) setWebhookSecret(b []byte) {
	e.secretMu.Lock()
	e.secret = b
	e.secretMu.Unlock()
}

// liveRun returns a queued, running or waiting-approval run from memory.
func (e *Engine) liveRun(id string) (Run, bool) {
	e.liveMu.Lock()
	r, ok := e.live[id]
	e.liveMu.Unlock()
	return r, ok
}

func (e *Engine) rememberRun(r Run) {
	e.liveMu.Lock()
	if terminal(r.Status) {
		delete(e.live, r.ID)
	} else {
		e.live[r.ID] = r
	}
	e.liveMu.Unlock()
}

// runState returns the current run, from memory when it is still in flight and
// from SQLite otherwise. A non-terminal run loaded from disk (for example one
// that was waiting for approval across a restart) is remembered.
func (e *Engine) runState(ctx context.Context, id string) (Run, error) {
	if r, ok := e.liveRun(id); ok {
		return r, nil
	}
	r, err := e.loadRun(ctx, id)
	if err == nil && !terminal(r.Status) {
		e.rememberRun(r)
	}
	return r, err
}

// applyStatus mirrors a committed run status update into memory and returns the
// resulting run, falling back to SQLite for runs that were never loaded.
func (e *Engine) applyStatus(id, status, message string, now int64, finish int64) (Run, error) {
	e.liveMu.Lock()
	r, ok := e.live[id]
	if ok {
		r.Status = status
		r.Error = message
		if r.StartedAt == 0 && status == "running" {
			r.StartedAt = now
		}
		r.FinishedAt = finish
		if terminal(status) {
			delete(e.live, id)
		} else {
			e.live[id] = r
		}
	}
	e.liveMu.Unlock()
	if ok {
		return r, nil
	}
	r, err := e.loadRun(context.Background(), id)
	if err == nil && !terminal(r.Status) {
		e.rememberRun(r)
	}
	return r, err
}

// markQueued mirrors Approve's committed queued transition into memory.
func (e *Engine) markQueued(id string) {
	e.liveMu.Lock()
	r, ok := e.live[id]
	if ok {
		r.Status = "queued"
		e.live[id] = r
	}
	e.liveMu.Unlock()
	if !ok {
		_, _ = e.runState(context.Background(), id) // the row is already queued
	}
}

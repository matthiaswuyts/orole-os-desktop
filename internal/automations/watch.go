package automations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

const (
	watchTick        = 5 * time.Second
	watchPollTimeout = 30 * time.Second
	watchMaxBackoff  = 30 * time.Minute
	watchSeenMax     = 500 // item keys remembered per watch
	watchMinInterval = 30
)

var (
	repoName     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	githubKinds  = []string{"push", "pr_opened", "pr_merged", "workflow_failed", "issue_opened"}
	watchSources = []string{"github", "mcp"}
)

// SetWatchSource registers the poller for a Watch.Source ("github", "mcp").
func (e *Engine) SetWatchSource(source string, s WatchSource) {
	e.deps.Lock()
	if e.watchers == nil {
		e.watchers = map[string]WatchSource{}
	}
	e.watchers[source] = s
	e.deps.Unlock()
}

func (e *Engine) watchSource(source string) WatchSource {
	e.deps.RLock()
	defer e.deps.RUnlock()
	return e.watchers[source]
}

// validateWatch reports a watch trigger's problems as field paths under trigger.watch.
func validateWatch(w *Watch, add func(path, msg string)) {
	if w == nil {
		add("trigger.watch", "Choose what to watch")
		return
	}
	if !slices.Contains(watchSources, w.Source) {
		add("trigger.watch.source", "Choose GitHub or an integration")
		return
	}
	if w.IntervalSec != 0 && (w.IntervalSec < watchMinInterval || w.IntervalSec > 86400) {
		add("trigger.watch.intervalSec", fmt.Sprintf("Use %d–86400 seconds", watchMinInterval))
	}
	switch w.Source {
	case "github":
		if !slices.Contains(githubKinds, w.Kind) {
			add("trigger.watch.kind", "Choose push, pull request opened or merged, workflow failed, or issue opened")
		}
		if !repoName.MatchString(w.Repo) {
			add("trigger.watch.repo", "Use owner/repository")
		}
	case "mcp":
		if w.Kind != "deployment" {
			add("trigger.watch.kind", "Integrations can be watched for deployments")
		}
		if w.Server == "" {
			add("trigger.watch.server", "Choose a connected integration")
		}
		if w.Tool == "" {
			add("trigger.watch.tool", "Choose the tool that lists deployments")
		}
	}
}

func watchInterval(w Watch) time.Duration {
	if w.IntervalSec > 0 {
		return time.Duration(w.IntervalSec) * time.Second
	}
	if w.Source == "mcp" {
		return 120 * time.Second
	}
	return 60 * time.Second
}

// watchLoop polls due watch triggers. Polls run one at a time on this goroutine, so a
// slow integration delays the others but never piles up concurrent requests.
func (e *Engine) watchLoop() {
	defer e.wg.Done()
	next := map[string]time.Time{} // automation id -> next poll
	failures := map[string]int{}
	for {
		snap, err := e.snapshot(e.ctx)
		if err == nil {
			now := e.clock.Now()
			live := map[string]bool{}
			for _, it := range snap.watches {
				id := it.def.ID
				live[id] = true
				if due, ok := next[id]; ok && now.Before(due) {
					continue
				}
				w := *it.def.Trigger.Watch
				if err := e.pollWatch(it, w); err != nil {
					failures[id]++
					backoff := min(watchInterval(w)<<min(failures[id], 6), watchMaxBackoff)
					next[id] = now.Add(backoff)
				} else {
					delete(failures, id)
					next[id] = now.Add(watchInterval(w))
				}
				if e.ctx.Err() != nil {
					return
				}
			}
			for id := range next {
				if !live[id] {
					delete(next, id)
					delete(failures, id)
				}
			}
		}
		// Without watch triggers there is nothing to poll: sleep until definitions change
		// instead of arming a timer every few seconds.
		var tick <-chan time.Time
		if err != nil || len(snap.watches) > 0 {
			tick = e.clock.After(watchTick)
		}
		select {
		case <-e.ctx.Done():
			return
		case <-e.watchWake:
		case <-tick:
		}
	}
}

// pollWatch polls one watch and runs the automation once per unseen item, oldest
// first. The trigger key is durable, so an item is never run twice even if the
// cursor write is lost.
func (e *Engine) pollWatch(it *defEntry, w Watch) error {
	id := it.def.ID
	cfg, _ := json.Marshal(w)
	src := e.watchSource(w.Source)
	if src == nil {
		return e.saveWatch(id, string(cfg), nil, "This integration is not available in this daemon", false)
	}
	ctx, cancel := context.WithTimeout(e.ctx, watchPollTimeout)
	items, err := src.Poll(ctx, "automation:"+id, w)
	cancel()
	if err != nil {
		if e.ctx.Err() != nil {
			return err
		}
		_ = e.saveWatch(id, string(cfg), nil, e.redact.Text(err.Error()), false)
		return err
	}
	var stored, seenJSON string
	err = e.db.QueryRowContext(e.ctx, `SELECT watch,seen FROM automation_watch_state WHERE automation_id=?`, id).Scan(&stored, &seenJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var seen []string
	if err == nil && stored == string(cfg) {
		_ = json.Unmarshal([]byte(seenJSON), &seen)
	} else {
		// A new or changed watch: what exists now is the baseline, nothing runs.
		for i := len(items) - 1; i >= 0; i-- {
			seen = append(seen, items[i].Key)
		}
		return e.saveWatch(id, string(cfg), seen, "", true)
	}
	known := make(map[string]bool, len(seen))
	for _, k := range seen {
		known[k] = true
	}
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Key == "" || known[item.Key] {
			continue
		}
		known[item.Key] = true
		seen = append(seen, item.Key)
		data := map[string]any{"topic": w.Source + "." + w.Kind, "data": item.Data, "time": e.clock.Now().Format(time.RFC3339)}
		if it.filter != nil {
			yes, err := it.filter.EvalBool(map[string]any{"event": data})
			if err != nil || !yes {
				continue
			}
		}
		if _, err := e.trigger(e.ctx, id, "watch", data, nil, "watch|"+id+"|"+item.Key); err != nil {
			e.log.Warn("automation watch item not run", "automation", id, "error", e.redact.Text(err.Error()))
		}
	}
	return e.saveWatch(id, string(cfg), seen, "", true)
}

// saveWatch stores the cursor (when ok) or just the poll outcome.
func (e *Engine) saveWatch(id, cfg string, seen []string, errText string, ok bool) error {
	now := e.clock.Now().UnixMilli()
	if !ok {
		_, err := e.db.ExecContext(e.ctx, `INSERT INTO automation_watch_state(automation_id,watch,error,polled_at) VALUES(?,?,?,?)
			ON CONFLICT(automation_id) DO UPDATE SET error=excluded.error, polled_at=excluded.polled_at`, id, "", errText, now)
		if err == nil && errText != "" {
			return errors.New(errText)
		}
		return err
	}
	if len(seen) > watchSeenMax {
		seen = seen[len(seen)-watchSeenMax:]
	}
	b, _ := json.Marshal(seen)
	_, err := e.db.ExecContext(e.ctx, `INSERT INTO automation_watch_state(automation_id,watch,seen,error,polled_at) VALUES(?,?,?,?,?)
		ON CONFLICT(automation_id) DO UPDATE SET watch=excluded.watch, seen=excluded.seen, error='', polled_at=excluded.polled_at`, id, cfg, string(b), "", now)
	return err
}

// watchStatus returns the last poll of every watching automation.
func (e *Engine) watchStatus(ctx context.Context) (map[string]WatchStatus, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT automation_id,seen,error,polled_at FROM automation_watch_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]WatchStatus{}
	for rows.Next() {
		var id, seen string
		var s WatchStatus
		if err := rows.Scan(&id, &seen, &s.Error, &s.PolledAt); err != nil {
			return nil, err
		}
		var keys []string
		_ = json.Unmarshal([]byte(seen), &keys)
		s.Seen = len(keys)
		out[id] = s
	}
	return out, rows.Err()
}

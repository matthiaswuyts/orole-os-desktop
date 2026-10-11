package automations

import (
	"context"
	"time"
)

// Retention bounds the finished run history. Journal rows are removed with their
// run by the foreign-key cascade. Runs that are queued, running or waiting for
// approval are never selected, whatever their age.
const (
	DefaultRetentionDays    = 30
	DefaultRetentionMaxRuns = 500
	MinRetentionDays        = 1
	MaxRetentionDays        = 3650
	MinRetentionMaxRuns     = 10
	MaxRetentionMaxRuns     = 100000

	pruneInterval = time.Hour
	// pruneGrace keeps a just-finished run for at least this long even when the
	// per-automation cap is exceeded: it covers the webhook replay window (a
	// trigger_key is what deduplicates a re-sent delivery) and late journal writes.
	pruneGrace = time.Hour
	pruneBatch = 500

	terminalStatuses = `('succeeded','failed','canceled','uncertain')`
)

// Prune deletes finished runs older than the configured age and, per automation,
// finished runs beyond the configured count. It returns the number of runs removed.
func (e *Engine) Prune(ctx context.Context) (int64, error) {
	e.mu.Lock()
	days, keep := e.config.RetentionDays, e.config.RetentionMaxRuns
	e.mu.Unlock()
	if days < MinRetentionDays {
		days = DefaultRetentionDays
	}
	if keep < MinRetentionMaxRuns {
		keep = DefaultRetentionMaxRuns
	}
	now := e.clock.Now()
	aged := now.Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	grace := now.Add(-pruneGrace).UnixMilli()
	var total int64
	// Both statements delete in small batches so the single SQLite writer is never
	// held for long; the loop ends when a batch comes back short.
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM automation_runs WHERE id IN (SELECT id FROM automation_runs WHERE status IN ` + terminalStatuses + ` AND coalesce(nullif(finished_at,0),created_at) < ? LIMIT ?)`, []any{aged, pruneBatch}},
		{`DELETE FROM automation_runs WHERE id IN (SELECT id FROM (SELECT id, coalesce(nullif(finished_at,0),created_at) AS ended, ROW_NUMBER() OVER (PARTITION BY automation_id ORDER BY created_at DESC, id DESC) AS rank FROM automation_runs WHERE status IN ` + terminalStatuses + `) WHERE rank > ? AND ended < ? LIMIT ?)`, []any{keep, grace, pruneBatch}},
	} {
		for {
			res, err := e.db.ExecContext(ctx, q.sql, q.args...)
			if err != nil {
				return total, err
			}
			n, _ := res.RowsAffected()
			total += n
			if n < pruneBatch {
				break
			}
		}
	}
	return total, nil
}

// pruneAsync runs Prune off the scheduler goroutine, at most one at a time.
func (e *Engine) pruneAsync() {
	if !e.pruning.CompareAndSwap(false, true) {
		return
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer e.pruning.Store(false)
		n, err := e.Prune(e.ctx)
		switch {
		case err != nil && e.ctx.Err() == nil:
			e.log.Warn("automation retention failed", "error", err)
		case n > 0:
			e.log.Info("automation retention removed finished runs", "runs", n)
		}
	}()
}

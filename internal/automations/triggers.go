package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"time"
)

func (e *Engine) loop() {
	defer e.wg.Done()
	sub := e.bus.Subscribe("*", 256)
	defer sub.Close()
	e.scheduleTick()
	// One timer drives the scheduler. It is re-armed only after it fires, so a
	// burst of events or wake-ups can never push the next tick back.
	var tick <-chan time.Time
	for {
		if tick == nil {
			tick = e.clock.After(time.Second)
		}
		select {
		case <-e.ctx.Done():
			return
		case ev := <-sub.C:
			e.event(ev)
		case <-e.wakeC:
			e.scheduleTick()
		case <-tick:
			tick = nil
			e.scheduleTick()
		}
	}
}
func (e *Engine) event(ev events.Event) {
	snap, err := e.snapshot(e.ctx)
	if err != nil {
		return
	}
	candidates := snap.matching(ev.Topic)
	if len(candidates) == 0 {
		return
	}
	env := map[string]any{"event": map[string]any{"topic": ev.Topic, "data": ev.Data, "time": ev.Time.Format(time.RFC3339)}}
	for _, it := range candidates {
		if it.filterBad {
			continue
		}
		if it.filter != nil {
			yes, err := it.filter.EvalBool(env)
			if err != nil || !yes {
				continue
			}
		}
		_, err = e.trigger(e.ctx, it.def.ID, "event", env["event"], ev.Chain, "")
		if err != nil {
			e.log.Warn("automation event ignored", "automation", it.def.ID, "error", e.redact.Text(err.Error()))
		}
	}
}
func (e *Engine) scheduleTick() {
	e.expireApprovals()
	now := e.clock.Now()
	if !now.Before(e.nextPrune) {
		e.nextPrune = now.Add(pruneInterval)
		e.pruneAsync()
	}
	snap, err := e.snapshot(e.ctx)
	if err != nil {
		return
	}
	for _, it := range snap.schedules {
		if !it.nextOK {
			anchor := time.Unix(it.last, 0)
			if it.last == 0 {
				anchor = time.UnixMilli(it.created)
			}
			it.next, it.hasNext = it.sched.Next(anchor)
			it.nextOK = true
		}
		if !it.hasNext || it.next.After(now) {
			continue
		}
		next := it.next
		missed := now.Sub(next) > 90*time.Second
		if !missed || it.def.Trigger.CatchUp == "run-once" {
			_, err = e.trigger(e.ctx, it.def.ID, "schedule", map[string]any{"scheduledAt": next.Format(time.RFC3339), "catchUp": missed}, nil, fmt.Sprintf("schedule:%s:%d", it.def.ID, next.Unix()))
			if err != nil {
				e.log.Warn("scheduled automation skipped", "error", e.redact.Text(err.Error()))
			}
		}
		if _, err = e.db.ExecContext(e.ctx, `UPDATE automations SET last_fire_at=? WHERE id=?`, now.Unix(), it.def.ID); err == nil {
			it.last = now.Unix()
			it.nextOK = false
		}
	}
}
func (e *Engine) Preview(schedule string) ([]string, error) {
	s, err := ParseSchedule(schedule, nil)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, t := range NextRuns(s, e.clock.Now(), 5) {
		out = append(out, t.Format(time.RFC3339))
	}
	return out, nil
}
func (e *Engine) Config() Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.config
	if e.credentials != nil {
		_, err := e.webhookSecret()
		c.HasSecret = err == nil
	}
	return c
}
func (e *Engine) SetConfig(ctx context.Context, c Config) (Config, error) {
	if c.GlobalConcurrency < 1 || c.GlobalConcurrency > 32 || c.PerAutomation < 1 || c.PerAutomation > c.GlobalConcurrency {
		return Config{}, fmt.Errorf("concurrency: global 1–32, per-automation 1–global")
	}
	if c.WebhookPort < 1024 || c.WebhookPort > 65535 {
		return Config{}, fmt.Errorf("webhook port must be 1024–65535")
	}
	if c.RetentionDays == 0 {
		c.RetentionDays = DefaultRetentionDays
	}
	if c.RetentionMaxRuns == 0 {
		c.RetentionMaxRuns = DefaultRetentionMaxRuns
	}
	if c.RetentionDays < MinRetentionDays || c.RetentionDays > MaxRetentionDays || c.RetentionMaxRuns < MinRetentionMaxRuns || c.RetentionMaxRuns > MaxRetentionMaxRuns {
		return Config{}, fmt.Errorf("retention: keep runs for %d–%d days and %d–%d runs per automation", MinRetentionDays, MaxRetentionDays, MinRetentionMaxRuns, MaxRetentionMaxRuns)
	}
	e.mu.Lock()
	old := e.config
	e.mu.Unlock()
	if c.WebhookEnabled && (old.WebhookPort != c.WebhookPort || !old.WebhookEnabled) {
		e.stopWebhook()
		if err := e.startWebhook(c.WebhookPort); err != nil {
			if old.WebhookEnabled {
				_ = e.startWebhook(old.WebhookPort)
			}
			return Config{}, err
		}
	} else if !c.WebhookEnabled {
		e.stopWebhook()
	}
	c.HasSecret = false
	b, _ := json.Marshal(c)
	_, err := e.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES('automations.config',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, string(b), e.clock.Now().Unix())
	if err != nil {
		return Config{}, err
	}
	e.mu.Lock()
	e.config = c
	e.pumpLocked()
	e.mu.Unlock()
	return e.Config(), nil
}

package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rayorole/orole-os-desktop/internal/automations/expr"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"time"
)

func (e *Engine) loop() {
	defer e.wg.Done()
	sub := e.bus.Subscribe("*", 256)
	defer sub.Close()
	e.scheduleTick()
	for {
		select {
		case <-e.ctx.Done():
			return
		case ev := <-sub.C:
			e.event(ev)
		case <-e.wakeC:
			e.scheduleTick()
		case <-e.clock.After(time.Second):
			e.scheduleTick()
		}
	}
}
func (e *Engine) event(ev events.Event) {
	list, err := e.List(e.ctx)
	if err != nil {
		return
	}
	env := map[string]any{"event": map[string]any{"topic": ev.Topic, "data": ev.Data, "time": ev.Time.Format(time.RFC3339)}}
	for _, item := range list {
		d := item.Definition
		if !d.Enabled || d.Trigger.Type != "event" || !events.Match(d.Trigger.Event, ev.Topic) {
			continue
		}
		if d.Trigger.Filter != "" {
			ex, err := expr.Parse(d.Trigger.Filter)
			if err != nil {
				continue
			}
			yes, err := ex.EvalBool(env)
			if err != nil || !yes {
				continue
			}
		}
		_, err = e.trigger(e.ctx, d.ID, "event", env["event"], ev.Chain, "")
		if err != nil {
			e.log.Warn("automation event ignored", "automation", d.ID, "error", e.redact.Text(err.Error()))
		}
	}
}
func (e *Engine) scheduleTick() {
	e.expireApprovals()
	rows, err := e.db.QueryContext(e.ctx, `SELECT definition,last_fire_at,created_at FROM automations WHERE enabled=1 AND trigger_type='schedule'`)
	if err != nil {
		return
	}
	type item struct {
		d       Definition
		last    int64
		created int64
	}
	list := []item{}
	for rows.Next() {
		var it item
		var b string
		if err = rows.Scan(&b, &it.last, &it.created); err != nil {
			break
		}
		if json.Unmarshal([]byte(b), &it.d) == nil {
			list = append(list, it)
		}
	}
	rows.Close()
	now := e.clock.Now()
	for _, it := range list {
		sc, err := ParseSchedule(it.d.Trigger.Schedule, nil)
		if err != nil {
			continue
		}
		anchor := time.Unix(it.last, 0)
		if it.last == 0 {
			anchor = time.UnixMilli(it.created)
		}
		next, ok := sc.Next(anchor)
		if !ok || next.After(now) {
			continue
		}
		missed := now.Sub(next) > 90*time.Second
		if !missed || it.d.Trigger.CatchUp == "run-once" {
			_, err = e.trigger(e.ctx, it.d.ID, "schedule", map[string]any{"scheduledAt": next.Format(time.RFC3339), "catchUp": missed}, nil, fmt.Sprintf("schedule:%s:%d", it.d.ID, next.Unix()))
			if err != nil {
				e.log.Warn("scheduled automation skipped", "error", e.redact.Text(err.Error()))
			}
		}
		_, _ = e.db.ExecContext(e.ctx, `UPDATE automations SET last_fire_at=? WHERE id=?`, now.Unix(), it.d.ID)
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
		_, err := e.credentials.Get(webhookCredential)
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

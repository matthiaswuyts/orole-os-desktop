package daemon

import (
	"context"
	"fmt"
	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

type automationNotifier struct{ d *Daemon }

func (n automationNotifier) Notify(ctx context.Context, p automations.NotificationRequest) (any, error) {
	return n.d.Notify(p.Source, p.Title, p.Body, notifications.Level(p.Level))
}
func (d *Daemon) registerAutomations() {
	e := automations.New(automations.Options{DB: d.DB.DB, Bus: d.Events, Credentials: credentials.New(), Log: d.Log, Audit: d.audit, Notifier: automationNotifier{d}, Authorize: func(ctx context.Context, request policy.Request) error {
		if d.Policy == nil {
			return fmt.Errorf("automation policy is unavailable")
		}
		return d.Policy.Require(ctx, request)
	}, Changed: func(r automations.Run) { d.IPC.Publish("automations.run", r) }})
	d.Automations = e
	handle(d, "automations.list", func(ctx context.Context, _ none) ([]automations.Summary, error) { return e.List(ctx) })
	handle(d, "automations.save", func(ctx context.Context, p automations.Definition) (automations.SaveResult, error) {
		return e.Save(ctx, p)
	})
	handle(d, "automations.delete", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (none, error) {
		return none{}, e.Delete(ctx, p.ID)
	})
	handle(d, "automations.run", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (automations.Run, error) {
		return e.Run(ctx, p.ID)
	})
	handle(d, "automations.cancel", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (none, error) {
		return none{}, e.Cancel(ctx, p.ID)
	})
	handle(d, "automations.runs", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) ([]automations.Run, error) {
		return e.Runs(ctx, p.ID)
	})
	handle(d, "automations.run.detail", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (automations.Detail, error) {
		return e.Detail(ctx, p.ID)
	})
	handle(d, "automations.approvals.list", func(ctx context.Context, _ none) ([]automations.Approval, error) { return e.Approvals(ctx) })
	handle(d, "automations.approve", func(ctx context.Context, p struct {
		RunID    string `json:"runId"`
		StepID   string `json:"stepId"`
		Approved bool   `json:"approved"`
		Note     string `json:"note"`
	}) (none, error) {
		return none{}, e.Approve(ctx, p.RunID, p.StepID, p.Approved, p.Note)
	})
	handle(d, "automations.stepTypes", func(ctx context.Context, _ none) ([]automations.StepType, error) { return e.StepTypes(), nil })
	handle(d, "automations.config", func(ctx context.Context, _ none) (automations.Config, error) { return e.Config(), nil })
	handle(d, "automations.config.set", func(ctx context.Context, p automations.Config) (automations.Config, error) {
		return e.SetConfig(ctx, p)
	})
	handle(d, "automations.webhook.generateSecret", func(ctx context.Context, _ none) (none, error) { return none{}, e.GenerateSecret() })
	handle(d, "automations.schedule.preview", func(ctx context.Context, p struct {
		Schedule string `json:"schedule"`
	}) ([]string, error) {
		return e.Preview(p.Schedule)
	})
	handle(d, "automations.templates", func(ctx context.Context, _ none) ([]automations.Definition, error) {
		return automations.Templates(), nil
	})
}

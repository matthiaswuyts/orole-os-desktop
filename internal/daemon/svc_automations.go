package daemon

import (
	"context"
	"errors"
	"fmt"
	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

type automationNotifier struct{ d *Daemon }

func (n automationNotifier) Notify(ctx context.Context, p automations.NotificationRequest) (any, error) {
	res, err := n.d.notifyResult(ctx, notifications.Input{Source: p.Source, Kind: notifications.Message, Title: p.Title, Body: p.Body, Level: notifications.Level(p.Level), Target: "automation:" + p.AutomationID})
	if err != nil || !res.Stored {
		return notifications.Notification{}, err
	}
	return res.Notification, nil
}
func (d *Daemon) registerAutomations() {
	finished := newTerminalRuns()
	e := automations.New(automations.Options{DB: d.DB.DB, Bus: d.Events, Credentials: credentials.New(), Log: d.Log, Audit: d.audit, Notifier: automationNotifier{d}, Authorize: func(ctx context.Context, request policy.Request) error {
		if d.Policy == nil {
			return fmt.Errorf("automation policy is unavailable")
		}
		return d.Policy.Require(ctx, request)
	}, Changed: func(r automations.Run) {
		d.IPC.Publish("automations.run", r)
		d.notices.run(r)
		d.jarvisRun(finished, r)
	}})
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
	handle(d, "automations.events", func(ctx context.Context, _ none) ([]events.Topic, error) {
		return events.Catalog(), nil
	})
	handle(d, "automations.proposals.get", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (automations.Proposal, error) {
		out, err := e.Proposal(ctx, p.ID)
		return out, proposalError(err)
	})
	// The user's decision on a bot's draft card. Approval saves the stored draft with the bot as
	// its creator; the engine audits every decision.
	handle(d, "automations.proposals.resolve", func(ctx context.Context, p automations.ProposalDecision) (automations.Proposal, error) {
		out, err := e.ResolveProposal(ctx, p)
		return out, proposalError(err)
	})
}

// proposalError maps proposal errors to IPC errors; their messages are written for the user.
func proposalError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, automations.ErrInvalid):
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	case errors.Is(err, automations.ErrNotFound):
		return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	case errors.Is(err, automations.ErrChanged):
		return &ipc.Error{Code: "conflict", Message: err.Error()}
	}
	return err
}

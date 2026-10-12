package app

import (
	"context"
	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/events"
)

// Automations forwards typed requests to the daemon-owned automation engine.
type Automations struct{ L *Link }

func (s Automations) List(ctx context.Context) ([]automations.Summary, error) {
	var out []automations.Summary
	err := s.L.call(ctx, "automations.list", nil, &out)
	return out, err
}
func (s Automations) Save(ctx context.Context, definition automations.Definition) (automations.SaveResult, error) {
	var out automations.SaveResult
	err := s.L.call(ctx, "automations.save", definition, &out)
	return out, err
}
func (s Automations) Delete(ctx context.Context, id string) error {
	return s.L.call(ctx, "automations.delete", map[string]string{"id": id}, nil)
}
func (s Automations) Run(ctx context.Context, id string) (automations.Run, error) {
	var out automations.Run
	err := s.L.call(ctx, "automations.run", map[string]string{"id": id}, &out)
	return out, err
}
func (s Automations) Cancel(ctx context.Context, id string) error {
	return s.L.call(ctx, "automations.cancel", map[string]string{"id": id}, nil)
}
func (s Automations) Runs(ctx context.Context, id string) ([]automations.Run, error) {
	var out []automations.Run
	err := s.L.call(ctx, "automations.runs", map[string]string{"id": id}, &out)
	return out, err
}
func (s Automations) Detail(ctx context.Context, id string) (automations.Detail, error) {
	var out automations.Detail
	err := s.L.call(ctx, "automations.run.detail", map[string]string{"id": id}, &out)
	return out, err
}
func (s Automations) Approvals(ctx context.Context) ([]automations.Approval, error) {
	var out []automations.Approval
	err := s.L.call(ctx, "automations.approvals.list", nil, &out)
	return out, err
}
func (s Automations) Approve(ctx context.Context, runId, stepId string, approved bool, note string) error {
	return s.L.call(ctx, "automations.approve", map[string]any{"runId": runId, "stepId": stepId, "approved": approved, "note": note}, nil)
}
func (s Automations) StepTypes(ctx context.Context) ([]automations.StepType, error) {
	var out []automations.StepType
	err := s.L.call(ctx, "automations.stepTypes", nil, &out)
	return out, err
}
func (s Automations) Config(ctx context.Context) (automations.Config, error) {
	var out automations.Config
	err := s.L.call(ctx, "automations.config", nil, &out)
	return out, err
}
func (s Automations) SetConfig(ctx context.Context, config automations.Config) (automations.Config, error) {
	var out automations.Config
	err := s.L.call(ctx, "automations.config.set", config, &out)
	return out, err
}
func (s Automations) GenerateSecret(ctx context.Context) error {
	return s.L.call(ctx, "automations.webhook.generateSecret", nil, nil)
}
func (s Automations) Preview(ctx context.Context, schedule string) ([]string, error) {
	var out []string
	err := s.L.call(ctx, "automations.schedule.preview", map[string]string{"schedule": schedule}, &out)
	return out, err
}
func (s Automations) Templates(ctx context.Context) ([]automations.Definition, error) {
	var out []automations.Definition
	err := s.L.call(ctx, "automations.templates", nil, &out)
	return out, err
}

// Events lists the application event topics flows can start on.
func (s Automations) Events(ctx context.Context) ([]events.Topic, error) {
	var out []events.Topic
	err := s.L.call(ctx, "automations.events", nil, &out)
	return out, err
}

// Proposal returns a bot's automation draft in any state, for its card in the chat.
func (s Automations) Proposal(ctx context.Context, id string) (automations.Proposal, error) {
	var out automations.Proposal
	err := s.L.call(ctx, "automations.proposals.get", map[string]string{"id": id}, &out)
	return out, err
}

// ResolveProposal approves (saving the draft, optionally turned on) or dismisses a bot's draft.
func (s Automations) ResolveProposal(ctx context.Context, decision automations.ProposalDecision) (automations.Proposal, error) {
	var out automations.Proposal
	err := s.L.call(ctx, "automations.proposals.resolve", decision, &out)
	return out, err
}

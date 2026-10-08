package daemon

import (
	"context"
	"fmt"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/agents/orchestrate"
	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/events"
)

// agentAutomation shares the desktop runtime and its durable dispatch journal.
type agentAutomation struct {
	d    *Daemon
	orch *orchestrate.Orchestrator
}

func (a agentAutomation) StartAgent(ctx context.Context, r automations.AgentRequest) (any, error) {
	p, err := a.d.Projects.Get(ctx, r.ProjectID)
	if err != nil {
		return nil, err
	}
	kind := r.Adapter
	if kind == "" {
		kind = "claude"
	}
	ctx = events.WithChain(ctx, r.Chain)
	return a.d.Agents.Start(ctx, agents.StartSpec{Kind: kind, Dir: p.Path, Prompt: r.Prompt, Key: r.Key})
}

func (a agentAutomation) SendPrompt(ctx context.Context, r automations.AgentRequest) (any, error) {
	return a.orch.SendPrompt(events.WithChain(ctx, r.Chain), r.Key, r.AgentID, r.Prompt)
}

func (a agentAutomation) WaitForAgent(ctx context.Context, id string) (any, error) {
	s, err := a.d.Agents.WaitFor(ctx, id, func(s agents.Session) bool { return s.Status == agents.StatusDone || s.Status.Terminal() })
	if err != nil {
		return nil, err
	}
	if s.Status != agents.StatusDone {
		return s, fmt.Errorf("agent %s ended %s: %s", id, s.Status, s.Error)
	}
	return s, nil
}

var _ automations.AgentControl = agentAutomation{}

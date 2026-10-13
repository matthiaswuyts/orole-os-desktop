package daemon

import (
	"context"
	"errors"
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
	switch r.PermissionMode {
	case "", agents.PermManual, agents.PermAuto:
	default:
		return nil, fmt.Errorf("automations start tasks in manual or auto permission mode only")
	}
	ctx = events.WithChain(ctx, r.Chain)
	s, err := a.d.Agents.Start(ctx, agents.StartSpec{
		Kind: kind, ProjectID: p.ID, Machine: p.MachineID, Prompt: r.Prompt, Title: r.Title, Model: r.Model,
		PermissionMode: r.PermissionMode, NewWorktree: r.NewWorktree, Key: r.Key,
	})
	if err != nil {
		return nil, err
	}
	return taskOutput(s), nil
}

func (a agentAutomation) SendPrompt(ctx context.Context, r automations.AgentRequest) (any, error) {
	return a.orch.SendPrompt(events.WithChain(ctx, r.Chain), r.Key, r.AgentID, r.Prompt)
}

// WaitForAgent returns when the task finished its turn or ended; with until=attention it
// also returns when the task needs a decision or an answer. A task that ended any other
// way than done fails the step, with the task's state still in the output.
func (a agentAutomation) WaitForAgent(ctx context.Context, r automations.AgentRequest) (any, error) {
	attention := r.Until == "attention"
	s, err := a.d.Agents.WaitFor(ctx, r.AgentID, func(s agents.Session) bool {
		return s.Status == agents.StatusDone || s.Status.Terminal() || (attention && s.Status == agents.StatusNeedsYou)
	})
	if err != nil {
		if errors.Is(err, agents.ErrNotFound) {
			return nil, fmt.Errorf("task %s does not exist", r.AgentID)
		}
		return nil, err
	}
	out := taskOutput(s)
	if s.Status != agents.StatusDone && s.Status != agents.StatusNeedsYou {
		return out, fmt.Errorf("task %s ended %s: %s", r.AgentID, s.Status, s.Error)
	}
	return out, nil
}

// taskOutput is a task step's output: the event payload fields plus the final result and
// diff size, so later blocks address steps.<id>.output.status, .lastMessage, .prUrl...
func taskOutput(s agents.Session) map[string]any {
	out := taskData(s)
	out["result"] = clip(s.Result)
	out["files"], out["additions"], out["deletions"] = s.Files, s.Additions, s.Deletions
	return out
}

var _ automations.AgentControl = agentAutomation{}

// botAutomation delivers flow messages into bot conversations.
type botAutomation struct{ d *Daemon }

func (b botAutomation) TellBot(ctx context.Context, r automations.BotRequest) (any, error) {
	if b.d.Chat == nil {
		return nil, errors.New("bots are unavailable")
	}
	out, err := b.d.Chat.Deliver(ctx, r.BotID, r.AutomationID, r.AutomationName, r.Message, r.Chain, r.Wait)
	return out, chatError(err)
}

var _ automations.BotMessenger = botAutomation{}

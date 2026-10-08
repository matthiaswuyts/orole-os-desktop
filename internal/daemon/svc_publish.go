package daemon

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/github"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

// Publishing a session's work (docs/architecture/github.md, "Publishing an agent task").
//
// IPC method: agents.publish {id, spec: PublishSpec} -> Session. Event: agents.updated.
// The commit/push/PR sequence lives in agents.Manager.Publish; this file supplies its GitHub
// client, which is the same policy-gated, confirmation-requiring, audited client every other
// GitHub write uses, and refuses automation callers (publishing is a user action).

// publishClient hands the agent runtime the daemon's GitHub client lazily, so signing in or
// out of GitHub after the runtime was built takes effect.
func (d *Daemon) publishClient() (*github.Client, error) {
	if d.gh == nil {
		return nil, errors.New("GitHub integration is unavailable")
	}
	return d.gh.conn()
}

func (d *Daemon) registerPublish() {
	handle(d, "agents.publish", func(ctx context.Context, p struct {
		ID   string             `json:"id"`
		Spec agents.PublishSpec `json:"spec"`
	}) (agents.Session, error) {
		if len(events.ChainFrom(ctx)) > 0 {
			return agents.Session{}, &ipc.Error{Code: ipc.CodeDenied, Message: "publishing is a user action and cannot be started by an automation"}
		}
		s, err := d.Agents.Publish(ctx, p.ID, p.Spec)
		if errors.Is(err, agents.ErrNotFound) {
			return s, &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
		}
		return s, err
	})
}

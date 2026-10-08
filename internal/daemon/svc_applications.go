package daemon

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/applications"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func applicationError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, applications.ErrInvalid):
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	case errors.Is(err, applications.ErrNotFound):
		return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	case errors.Is(err, applications.ErrChanged):
		return &ipc.Error{Code: "conflict", Message: err.Error()}
	case errors.Is(err, policy.ErrDenied), errors.Is(err, policy.ErrApprovalTimeout):
		return &ipc.Error{Code: "denied", Message: err.Error()}
	default:
		return err
	}
}

// registerApplications must follow registerMCP, which initializes d.Policy.
// The manager is retained by these closures; no desktop-owned durable state.
func (d *Daemon) registerApplications() {
	manager := applications.New(d.DB.DB, d.Policy)
	handle(d, "applications.list", func(ctx context.Context, _ none) ([]applications.Shortcut, error) {
		return manager.List(ctx)
	})
	handle(d, "applications.save", func(ctx context.Context, req applications.SaveRequest) (applications.Shortcut, error) {
		s, err := manager.Save(ctx, req)
		if err == nil {
			d.audit("applications.save", s.ID, "allowed", "")
			d.IPC.Publish("applications.changed", map[string]string{"id": s.ID})
		}
		return s, applicationError(err)
	})
	handle(d, "applications.remove", func(ctx context.Context, req struct {
		ID string `json:"id"`
	}) (none, error) {
		err := manager.Remove(ctx, req.ID)
		if err == nil {
			d.audit("applications.remove", req.ID, "allowed", "")
			d.IPC.Publish("applications.changed", map[string]string{"id": req.ID})
		}
		return none{}, applicationError(err)
	})
	handle(d, "applications.launch", func(ctx context.Context, req struct {
		ID string `json:"id"`
	}) (applications.LaunchResult, error) {
		result, err := manager.Launch(ctx, req.ID)
		if err != nil {
			d.audit("applications.launch.result", req.ID, "failed", "Native launch did not complete")
		} else {
			d.audit("applications.launch.result", req.ID, "allowed", "Native process started")
		}
		return result, applicationError(err)
	})
}

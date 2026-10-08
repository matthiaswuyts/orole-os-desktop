package daemon

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/plugins"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func pluginError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, plugins.ErrInvalid):
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	case errors.Is(err, plugins.ErrNotFound):
		return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	case errors.Is(err, plugins.ErrChanged):
		return &ipc.Error{Code: "conflict", Message: err.Error()}
	case errors.Is(err, plugins.ErrUntrusted), errors.Is(err, policy.ErrDenied), errors.Is(err, policy.ErrApprovalTimeout):
		return &ipc.Error{Code: "denied", Message: err.Error()}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, plugins.ErrClosed):
		return &ipc.Error{Code: "canceled", Message: err.Error()}
	default:
		return err
	}
}

// registerPlugins follows registerMCP. The daemon closes Plugins before MCP.
func (d *Daemon) registerPlugins() {
	d.Plugins = plugins.New(d.DB.DB, d.Policy, d.MCP)
	handle(d, "plugins.list", func(ctx context.Context, _ none) ([]plugins.PluginRecord, error) {
		out, err := d.Plugins.List(ctx)
		return out, pluginError(err)
	})
	handle(d, "plugins.save", func(ctx context.Context, req plugins.PluginSaveRequest) (plugins.PluginRecord, error) {
		out, err := d.Plugins.Save(ctx, req)
		if err == nil {
			d.audit("plugins.save", out.Manifest.ID, "allowed", "Declarative manifest saved")
			d.IPC.Publish("plugins.changed", plugins.PluginChange{ID: out.Manifest.ID})
		}
		return out, pluginError(err)
	})
	handle(d, "plugins.trust", func(ctx context.Context, req plugins.PluginTrustRequest) (plugins.PluginRecord, error) {
		out, err := d.Plugins.Trust(ctx, req)
		if err == nil {
			d.IPC.Publish("plugins.changed", plugins.PluginChange{ID: out.Manifest.ID})
		}
		return out, pluginError(err)
	})
	handle(d, "plugins.remove", func(ctx context.Context, req plugins.PluginRef) (none, error) {
		err := d.Plugins.Remove(ctx, req)
		if err == nil {
			d.audit("plugins.remove", req.ID, "allowed", "Declarative plugin removed")
			d.IPC.Publish("plugins.changed", plugins.PluginChange{ID: req.ID})
		}
		return none{}, pluginError(err)
	})
	handle(d, "plugins.run", func(ctx context.Context, req plugins.PluginRunRequest) (plugins.PluginRunResult, error) {
		out, err := d.Plugins.Run(ctx, req)
		return out, pluginError(err)
	})
}

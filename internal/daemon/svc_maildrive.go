package daemon

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/drive"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/mail"
	"github.com/rayorole/orole-os-desktop/internal/maildrive"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

type mailDriveID struct {
	ID string `json:"id"`
}

func mailDriveHandler[P, R any](d *Daemon, name string, fn func(context.Context, P) (R, error)) {
	handle(d, name, func(ctx context.Context, p P) (R, error) {
		out, err := fn(ctx, p)
		if err == nil {
			return out, nil
		}
		code := "adapter_error"
		switch {
		case errors.Is(err, maildrive.ErrNotConfigured):
			code = "NotConfigured"
		case errors.Is(err, maildrive.ErrUnsupported):
			code = "Unsupported"
		case errors.Is(err, mcp.ErrNotConnected):
			code = "not_connected"
		case errors.Is(err, mail.ErrBadRequest), errors.Is(err, drive.ErrBadRequest):
			code = ipc.CodeBadRequest
		case errors.Is(err, policy.ErrDenied):
			code = "permission_denied"
		case errors.Is(err, policy.ErrApprovalTimeout):
			code = "approval_timeout"
		case errors.Is(err, maildrive.ErrInvalidResult), errors.Is(err, maildrive.ErrTool):
			code = "invalid_result"
		}
		return out, &ipc.Error{Code: code, Message: untrusted.Line(err.Error(), 500)}
	})
}

func (d *Daemon) registerMailDrive() {
	mailAdapter := mail.New(d.MCP, d.Policy)
	driveAdapter := drive.New(d.MCP, d.Policy)
	mailDriveHandler(d, "mail.status", func(ctx context.Context, _ none) (mail.Status, error) { return mailAdapter.Status(ctx) })
	mailDriveHandler(d, "mail.folders", func(ctx context.Context, _ none) ([]mail.Folder, error) { return mailAdapter.Folders(ctx) })
	mailDriveHandler(d, "mail.list", mailAdapter.List)
	mailDriveHandler(d, "mail.search", mailAdapter.Search)
	mailDriveHandler(d, "mail.thread", func(ctx context.Context, p mailDriveID) (mail.Thread, error) { return mailAdapter.Thread(ctx, p.ID) })
	mailDriveHandler(d, "mail.send", mailAdapter.Send)
	mailDriveHandler(d, "mail.reply", mailAdapter.Reply)
	mailDriveHandler(d, "mail.trash", func(ctx context.Context, p mailDriveID) (mail.Mutation, error) { return mailAdapter.Trash(ctx, p.ID) })
	mailDriveHandler(d, "drive.status", func(ctx context.Context, _ none) (drive.Status, error) { return driveAdapter.Status(ctx) })
	mailDriveHandler(d, "drive.list", driveAdapter.List)
	mailDriveHandler(d, "drive.search", driveAdapter.Search)
	mailDriveHandler(d, "drive.stat", func(ctx context.Context, p mailDriveID) (drive.File, error) { return driveAdapter.Stat(ctx, p.ID) })
	mailDriveHandler(d, "drive.preview", func(ctx context.Context, p mailDriveID) (drive.Preview, error) {
		return driveAdapter.Preview(ctx, p.ID)
	})
	mailDriveHandler(d, "drive.trash", func(ctx context.Context, p mailDriveID) (drive.Mutation, error) { return driveAdapter.Trash(ctx, p.ID) })
	// Resolve Agents at invocation time: its registration follows Mail/Drive.
	handoff := func() driveHandoff { return driveHandoff{adapter: driveAdapter, agents: d.Agents, policy: d.Policy} }
	mailDriveHandler(d, "drive.recipients", func(ctx context.Context, p none) ([]drive.Recipient, error) {
		return handoff().recipients(ctx, p)
	})
	mailDriveHandler(d, "drive.share", func(ctx context.Context, p drive.ShareRequest) (drive.ShareResult, error) {
		return handoff().share(ctx, p)
	})
}

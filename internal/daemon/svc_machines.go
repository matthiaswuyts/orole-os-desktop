package daemon

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/machines"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func machineError(err error) error {
	if err == nil {
		return nil
	}
	code := "machine_error"
	switch {
	case errors.Is(err, machines.ErrHostKeyChanged):
		code = "host_key_changed"
	case errors.Is(err, machines.ErrAccessChanged):
		code = "access_changed"
	case errors.Is(err, machines.ErrDenied), errors.Is(err, policy.ErrDenied):
		code = ipc.CodeDenied
	case errors.Is(err, machines.ErrInvalid):
		code = ipc.CodeBadRequest
	case errors.Is(err, sql.ErrNoRows):
		code = ipc.CodeNotFound
	case errors.Is(err, context.Canceled):
		code = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	}
	return &ipc.Error{Code: code, Message: err.Error()}
}
func machineFileError(err error) error {
	if errors.Is(err, machines.ErrDenied) || errors.Is(err, machines.ErrAccessChanged) || errors.Is(err, machines.ErrInvalid) || errors.Is(err, policy.ErrDenied) || errors.Is(err, machines.ErrHostKeyChanged) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return machineError(err)
	}
	return mapFileErr(err)
}
func (d *Daemon) registerMachines() {
	m := machines.New(d.DB.DB, credentials.New(), d.Policy)
	d.Machines = m
	m.SetHostMetrics(d.Metrics)
	handle(d, "machines.list", func(ctx context.Context, _ none) ([]machines.Machine, error) {
		v, e := m.List(ctx)
		return v, machineError(e)
	})
	handle(d, "machines.probe", func(ctx context.Context, p machines.ProbeRequest) (machines.Identity, error) {
		v, e := m.Probe(ctx, "user", p)
		return v, machineError(e)
	})
	handle(d, "machines.pair", func(ctx context.Context, p machines.PairRequest) (machines.Machine, error) {
		v, e := m.Pair(ctx, "user", p)
		if e == nil {
			d.IPC.Publish("machines.changed", v.ID)
		}
		return v, machineError(e)
	})
	handle(d, "machines.access", func(ctx context.Context, p machines.Access) (machines.Machine, error) {
		v, e := m.SetAccess(ctx, "user", p)
		if e == nil {
			d.IPC.Publish("machines.changed", v.ID)
		}
		return v, machineError(e)
	})
	handle(d, "machines.rename", func(ctx context.Context, p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}) (machines.Machine, error) {
		v, e := m.Rename(ctx, "user", p.ID, p.Name)
		if e == nil {
			d.IPC.Publish("machines.changed", v.ID)
		}
		return v, machineError(e)
	})
	handle(d, "machines.remove", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (none, error) {
		e := m.Remove(ctx, "user", p.ID)
		if e == nil {
			d.IPC.Publish("machines.changed", p.ID)
		}
		return none{}, machineError(e)
	})
	handle(d, "machines.health", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (machines.MachineHealth, error) {
		v, e := m.Health(ctx, "user", p.ID)
		return v, machineError(e)
	})
	handle(d, "machines.execute", func(ctx context.Context, p machines.Command) (machines.CommandResult, error) {
		v, e := m.Execute(ctx, "user", p)
		return v, machineError(e)
	})
	handle(d, "machines.cancel", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (none, error) {
		return none{}, machineError(m.Cancel(p.ID))
	})
	handle(d, "machines.files.list", func(ctx context.Context, p machines.FileRequest) ([]files.Entry, error) {
		v, e := m.ListFiles(ctx, "user", p)
		return v, machineFileError(e)
	})
	handle(d, "machines.files.stat", func(ctx context.Context, p machines.FileRequest) (files.Info, error) {
		v, e := m.StatFile(ctx, "user", p)
		return v, machineFileError(e)
	})
	handle(d, "machines.files.read", func(ctx context.Context, p machines.FileReadRequest) (files.ReadResult, error) {
		v, e := m.ReadFile(ctx, "user", p)
		return v, machineFileError(e)
	})
	handle(d, "machines.files.write", func(ctx context.Context, p machines.FileWriteRequest) (files.Info, error) {
		v, e := m.WriteFile(ctx, "user", p)
		return v, machineFileError(e)
	})
}

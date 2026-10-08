package app

import (
	"context"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/machines"
)

type Machines struct{ L *Link }

func (s Machines) List(ctx context.Context) ([]machines.Machine, error) {
	var out []machines.Machine
	err := s.L.call(ctx, "machines.list", nil, &out)
	return out, err
}
func (s Machines) Probe(ctx context.Context, address string) (machines.Identity, error) {
	var out machines.Identity
	err := s.L.call(ctx, "machines.probe", machines.ProbeRequest{Address: address}, &out)
	return out, err
}
func (s Machines) Pair(ctx context.Context, p machines.PairRequest) (machines.Machine, error) {
	var out machines.Machine
	err := s.L.call(ctx, "machines.pair", p, &out)
	return out, err
}
func (s Machines) SetAccess(ctx context.Context, p machines.Access) (machines.Machine, error) {
	var out machines.Machine
	err := s.L.call(ctx, "machines.access", p, &out)
	return out, err
}
func (s Machines) Rename(ctx context.Context, id, name string) (machines.Machine, error) {
	var out machines.Machine
	err := s.L.call(ctx, "machines.rename", map[string]string{"id": id, "name": name}, &out)
	return out, err
}
func (s Machines) Remove(ctx context.Context, id string) error {
	return s.L.call(ctx, "machines.remove", map[string]string{"id": id}, nil)
}
func (s Machines) Health(ctx context.Context, id string) (machines.MachineHealth, error) {
	var out machines.MachineHealth
	err := s.L.call(ctx, "machines.health", map[string]string{"id": id}, &out)
	return out, err
}
func (s Machines) Execute(ctx context.Context, p machines.Command) (machines.CommandResult, error) {
	var out machines.CommandResult
	err := s.L.call(ctx, "machines.execute", p, &out)
	return out, err
}
func (s Machines) Cancel(ctx context.Context, operationID string) error {
	return s.L.call(ctx, "machines.cancel", map[string]string{"id": operationID}, nil)
}
func (s Machines) ListFiles(ctx context.Context, p machines.FileRequest) ([]files.Entry, error) {
	var out []files.Entry
	err := s.L.call(ctx, "machines.files.list", p, &out)
	return out, err
}
func (s Machines) StatFile(ctx context.Context, p machines.FileRequest) (files.Info, error) {
	var out files.Info
	err := s.L.call(ctx, "machines.files.stat", p, &out)
	return out, err
}
func (s Machines) ReadFile(ctx context.Context, p machines.FileReadRequest) (files.ReadResult, error) {
	var out files.ReadResult
	err := s.L.call(ctx, "machines.files.read", p, &out)
	return out, err
}
func (s Machines) WriteFile(ctx context.Context, p machines.FileWriteRequest) (files.Info, error) {
	var out files.Info
	err := s.L.call(ctx, "machines.files.write", p, &out)
	return out, err
}

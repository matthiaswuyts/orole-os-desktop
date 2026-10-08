package gateway

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/skills"
)

// IPCBackend implements Backend over the authenticated daemon connection.
type IPCBackend struct {
	c       *ipc.Client
	session string
}

// NewIPCBackend returns a backend that calls the daemon through c as the given agent session
// ("" = a harness Orole did not launch; calls run as actor agent:external).
func NewIPCBackend(c *ipc.Client, session string) *IPCBackend {
	return &IPCBackend{c: c, session: session}
}

func (b *IPCBackend) call(ctx context.Context, timeout time.Duration, method string, params, out any) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return b.c.Call(ctx, method, params, out)
}

// Tools implements Backend.
func (b *IPCBackend) Tools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	err := b.call(ctx, 20*time.Second, MethodTools, ToolsParams{SessionID: b.session}, &out)
	return out, err
}

// Call implements Backend. It has no client-side timeout: the call may wait for the user to
// approve it. Cancellation (the harness aborting) propagates to the daemon.
func (b *IPCBackend) Call(ctx context.Context, name string, args json.RawMessage) (*mcp.CallResult, error) {
	var out mcp.CallResult
	if err := b.call(ctx, 0, MethodCall, CallParams{SessionID: b.session, Name: name, Arguments: args}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Skills implements Backend.
func (b *IPCBackend) Skills(ctx context.Context) ([]skills.Skill, error) {
	var out []skills.Skill
	err := b.call(ctx, 20*time.Second, MethodSkills, struct{}{}, &out)
	return out, err
}

// Skill implements Backend.
func (b *IPCBackend) Skill(ctx context.Context, name, file string) (SkillContent, error) {
	var out SkillContent
	err := b.call(ctx, 20*time.Second, MethodSkill, SkillParams{Name: name, File: file}, &out)
	return out, err
}

var _ Backend = (*IPCBackend)(nil)

// WatchEvents subscribes c to the daemon events that can change the tool list and returns a
// channel that receives a signal for each (coalesced). Until the daemon is reachable it retries
// in the background; the IPC client re-applies the subscription after later reconnects.
func WatchEvents(ctx context.Context, c *ipc.Client) <-chan struct{} {
	refresh := make(chan struct{}, 1)
	signal := func() {
		select {
		case refresh <- struct{}{}:
		default:
		}
	}
	c.OnEvent(func(string, json.RawMessage) { signal() })
	go func() {
		for ctx.Err() == nil {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Connect(cctx)
			if err == nil {
				err = c.Subscribe(cctx, RefreshEvents...)
			}
			cancel()
			if err == nil {
				signal() // events between the first listing and the subscription would be missed
				return
			}
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return refresh
}

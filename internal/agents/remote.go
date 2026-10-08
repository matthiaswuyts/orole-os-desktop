package agents

import (
	"context"
	"errors"
	"sync"

	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/machines"
)

var errRemoteOperation = errors.New("this operation is not supported for remote agent sessions; it will not access a local path")

type remoteProcess struct {
	process *machines.AgentProcess
	events  chan Event
}

func (*remoteProcess) Pid() int           { return 0 }
func (*remoteProcess) TerminalID() string { return "" }
func (*remoteProcess) Send(string) error {
	return errors.New("remote process mode accepts one task per launch; wait for exit, then restart with a new task")
}
func (*remoteProcess) Respond(string, bool, string) error { return ErrNoPermissions }
func (p *remoteProcess) Stop() error                      { return p.process.Stop() }
func (p *remoteProcess) Events() <-chan Event             { return p.events }

// remoteOutput bounds both event volume and retained output, continuing to drain
// the SSH channel after truncation so a noisy child cannot grow daemon memory.
type remoteOutput struct {
	mu        sync.Mutex
	events    chan Event
	bytes     int
	truncated bool
	tail      tailBuf
}

func (w *remoteOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(data)
	_, _ = w.tail.Write(data)
	for len(data) > 0 && w.bytes < 512<<10 {
		take := min(len(data), 4096, (512<<10)-w.bytes)
		w.events <- Event{Kind: EvMessage, Text: string(data[:take])}
		w.bytes += take
		data = data[take:]
	}
	if len(data) > 0 && !w.truncated {
		w.truncated = true
		w.events <- Event{Kind: EvSystem, Text: "Remote output exceeded 512 KiB; remaining output is drained and only its final 16 KiB retained."}
	}
	return n, nil
}

func (m *Manager) startRemote(ctx context.Context, spec LaunchSpec) (Process, error) {
	if m.cfg.Machines == nil {
		return nil, errors.New("machine transport is unavailable")
	}
	if spec.Mode != ModeProcess || spec.Resume != "" {
		return nil, errRemoteOperation
	}
	p := &remoteProcess{events: make(chan Event, 32)}
	output := &remoteOutput{events: p.events, tail: tailBuf{max: 16 << 10}}
	actor := "user"
	if chain := events.ChainFrom(ctx); len(chain) > 0 {
		actor = "automation:" + chain[len(chain)-1]
	}
	remote, err := m.cfg.Machines.StartAgentProcess(ctx, m.bg, actor, machines.AgentProcessRequest{
		Machine: spec.Machine, Kind: spec.Kind, Dir: spec.Dir,
		Model: spec.Model, Reasoning: spec.Reasoning, PermissionMode: spec.PermissionMode, Prompt: spec.Prompt,
	}, output, output)
	if err != nil {
		return nil, err
	}
	p.process = remote
	go func() {
		defer close(p.events)
		p.events <- Event{Kind: EvSystem, Text: "Running on the selected SSH box in one-shot text/process mode. No local terminal, structured approvals, or worktree review is available. Disconnecting cannot guarantee that remote descendants stop."}
		result := <-remote.Done()
		if text := output.tail.String(); text != "" {
			p.events <- Event{Kind: EvMessage, Text: text}
		}
		p.events <- Event{Kind: EvExit, Data: jraw(map[string]any{"code": result.Code, "stderr": result.Error, "uncertain": result.Uncertain})}
	}()
	return p, nil
}

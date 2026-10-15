package daemon

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/sandbox"
)

// sandboxRef names one bot's computer.
type sandboxRef struct {
	BotID string `json:"botId"`
}

// sandboxReplyWait is how long sandbox.setup.enable waits for the setup to finish before it answers with
// "running" (the IPC call deadline is 30 s; a UAC prompt can stay open longer). The result follows as events.
const sandboxReplyWait = 25 * time.Second

// sandboxSetupRun is one Windows Hypervisor Platform setup in flight; callers that arrive meanwhile share it.
type sandboxSetupRun struct {
	done  chan struct{}
	state sandbox.SetupState
	err   error
}

// registerSandbox owns the bot computers: one QEMU/WHPX VM per bot (internal/sandbox), the pinned runtime
// download and the one-time Windows Hypervisor Platform setup. VMs are children of this daemon (job object)
// and stop with it; disks under <data>\sandbox survive.
//
// IPC: sandbox.availability, sandbox.status{botId}, sandbox.setup.enable (one UAC prompt, audited),
// sandbox.runtime.install (starts the background download, returns its state; calling it again while it runs
// returns progress). Events: sandbox.status, sandbox.availability, sandbox.runtime, sandbox.setup.
//
// Setup and install are user actions: they run with the daemon's lifetime, not the request's, so closing the
// window or a call timeout does not abort them, and report progress as events.
func (d *Daemon) registerSandbox() {
	paths := sandbox.Paths{Root: filepath.Join(d.DataDir, "sandbox")}
	life, cancel := context.WithCancel(context.Background())
	d.sandboxStop = cancel
	d.sandboxStore = sandbox.NewStore(d.DB.DB)

	var m *sandbox.Manager
	rt := sandbox.NewRuntime(sandbox.RuntimeConfig{Paths: paths, Pins: sandbox.DefaultPins,
		OnChange: func(s sandbox.RuntimeState) {
			d.IPC.Publish("sandbox.runtime", s)
			if !s.Installing && m != nil {
				d.IPC.Publish("sandbox.availability", m.Availability(life))
			}
		}})
	m = sandbox.NewManager(sandbox.Config{
		Paths: paths, MaxRunning: 4, IdleTimeout: 15 * time.Minute, BootTimeout: 60 * time.Second, BusyWait: 30 * time.Second,
		FreeMemory: d.freeMemory, Now: time.Now, Image: rt.Image,
	}, sandbox.NewHostBackend(rt, paths), d.sandboxStore)
	d.Sandbox = m
	m.Subscribe(func(s sandbox.Status) { d.IPC.Publish("sandbox.status", s) })

	// The helper is always the file next to this executable, never a path from IPC or settings: it runs elevated.
	helper := ""
	if exe, err := os.Executable(); err == nil {
		helper = sandbox.SetupHelperPath(exe)
	}

	handle(d, "sandbox.availability", func(ctx context.Context, _ none) (sandbox.Availability, error) {
		return m.Availability(ctx), nil
	})
	handle(d, "sandbox.status", func(ctx context.Context, p sandboxRef) (sandbox.Status, error) {
		if p.BotID == "" {
			return sandbox.Status{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: "botId is required"}
		}
		return m.Status(p.BotID), nil
	})

	var setupMu sync.Mutex
	var setup *sandboxSetupRun
	handle(d, "sandbox.setup.enable", func(ctx context.Context, _ none) (sandbox.SetupState, error) {
		if err := requireDesktopUser(ctx, "turn on Windows Hypervisor Platform"); err != nil {
			return sandbox.SetupState{}, err
		}
		setupMu.Lock()
		run := setup
		if run == nil {
			run = &sandboxSetupRun{done: make(chan struct{})}
			setup = run
			d.sandboxWG.Add(1)
			go func() {
				defer d.sandboxWG.Done()
				d.IPC.Publish("sandbox.setup", sandbox.SetupState{Running: true})
				res, err := sandbox.EnableWHP(life, helper, paths)
				st := sandbox.SetupState{RebootRequired: res.RebootRequired}
				detail := "enabled"
				if res.RebootRequired {
					detail = "enabled, restart required"
				}
				if err != nil {
					st.Error, detail = err.Error(), err.Error()
				}
				d.audit("sandbox.setup", "HypervisorPlatform", outcomeOf(err), detail)
				run.state, run.err = st, err
				setupMu.Lock()
				setup = nil
				setupMu.Unlock()
				close(run.done)
				d.IPC.Publish("sandbox.setup", st)
				d.IPC.Publish("sandbox.availability", m.Availability(life))
			}()
		}
		setupMu.Unlock()
		wait := time.NewTimer(sandboxReplyWait)
		defer wait.Stop()
		select {
		case <-run.done:
			return run.state, run.err
		case <-wait.C:
		case <-ctx.Done():
		}
		return sandbox.SetupState{Running: true}, nil
	})

	handle(d, "sandbox.runtime.install", func(ctx context.Context, _ none) (sandbox.RuntimeState, error) {
		if err := requireDesktopUser(ctx, "download the computer runtime"); err != nil {
			return sandbox.RuntimeState{}, err
		}
		if !sandbox.DefaultPins.Complete() {
			return rt.State(), sandbox.ErrNoPins
		}
		before := rt.State()
		st := rt.Start(life) // the daemon's lifetime: the download outlives this request
		if !before.Installing && !before.Ready {
			d.audit("sandbox.runtime.install", sandbox.DefaultPins.ImageVersion, "allowed", "download started")
		}
		return st, nil
	})
}

// requireDesktopUser refuses a call made for a bot or an automation. IPC calls come from the desktop app or the
// CLI (the token holder, the user), whose context carries neither; this keeps it that way if a bot tool or an
// automation step is ever routed to these handlers.
func requireDesktopUser(ctx context.Context, what string) error {
	if agents.ActorFrom(ctx) != "" || len(events.ChainFrom(ctx)) > 0 {
		return &ipc.Error{Code: ipc.CodeDenied, Message: "only you can " + what + ", from the Orole app"}
	}
	return nil
}

// freeMemory is the Manager's free-RAM check (refuse a boot below VM RAM + 1 GB).
func (d *Daemon) freeMemory() (uint64, error) {
	s, err := d.Metrics.Snapshot(context.Background())
	return s.MemoryAvailableBytes, err
}

// deleteBotComputer stops an archived bot's VM and removes its disk and row. Archiving is how bots are deleted.
// A bot that never had a computer is a no-op.
func (d *Daemon) deleteBotComputer(botID string) {
	if d.Sandbox == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, had, err := d.sandboxStore.Get(ctx, botID)
	if err != nil {
		d.Log.Warn("delete bot computer", "bot", botID, "err", err)
	}
	running := d.Sandbox.Status(botID).State != sandbox.StateOff
	if err := d.Sandbox.Delete(ctx, botID); err != nil {
		d.Log.Warn("delete bot computer", "bot", botID, "err", err)
		d.audit("sandbox.delete", botID, "failed", err.Error())
		return
	}
	if had || running {
		d.audit("sandbox.delete", botID, "allowed", "Bot archived")
	}
}

// shutdownSandbox ends the background setup and downloads, then stops every VM cleanly on daemon exit (the job
// object kills any that do not stop).
func (d *Daemon) shutdownSandbox() {
	if d.sandboxStop != nil {
		d.sandboxStop()
	}
	if d.Sandbox != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err := d.Sandbox.Shutdown(ctx); err != nil {
			d.Log.Warn("sandbox shutdown", "err", err)
		}
		cancel()
	}
	// The setup goroutine audits to the database, which closes next. A UAC prompt still open keeps it in
	// ShellExecuteEx, which no context interrupts: do not hang the exit on it.
	done := make(chan struct{})
	go func() { d.sandboxWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		d.Log.Warn("sandbox shutdown: the Windows Hypervisor Platform setup is still waiting for its UAC prompt")
	}
}

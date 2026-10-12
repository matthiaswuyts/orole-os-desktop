package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/agents/orchestrate"
	"github.com/rayorole/orole-os-desktop/internal/cliruntime"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/pricing"
	"github.com/rayorole/orole-os-desktop/internal/projects"
)

// Agent runtime. Local processes are daemon children; SSH processes retain pinned
// machine access snapshots. A lost remote transport never implies process termination.
// Sessions, events and the orchestration journal persist in SQLite.
//
// IPC methods: agents.detect/list/get/start/stop/restart/prompt/setPermissionMode/approve/events/transcript/
// remove/diff/review/check/hook/usage, agents.handoff/reviewWith/loopUntil/tryN/runs/cancelRun.
// agents.list takes {limit, offset} (default page 200, at most 500) and returns sessions
// newest first; callers page until a page comes back shorter than the limit.
// agents.usage takes agents.UsageQuery and returns agents.UsageStats: token usage and spend from
// the usage ledger, priced with the models.dev catalogue (refreshed at most daily, see pricing).
// Events: agents.updated (Session: status and other durable changes), agents.delta
// (SessionDelta: display fields changed on a hot path), agents.event (Event),
// agents.removed ({id}), agents.run (Run).

// agentWorktrees creates/removes worktrees through the projects service and reads diffs with git.
type agentWorktrees struct {
	d   *Daemon
	git agents.GitWorktrees
}

func (w agentWorktrees) project(ctx context.Context, repo string) (string, error) {
	p, err := w.d.Projects.Add(ctx, repo)
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

func (w agentWorktrees) Create(ctx context.Context, repo, name, base string) (string, string, error) {
	pid, err := w.project(ctx, repo)
	if err != nil {
		return "", "", err
	}
	wt, err := w.d.Projects.Worktrees.Create(ctx, pid, name, base)
	if errors.Is(err, projects.ErrConflict) {
		return "", "", fmt.Errorf("%w: %v", agents.ErrWorktreeExists, err)
	}
	if err != nil {
		return "", "", err
	}
	return wt.Path, wt.Branch, nil
}

func (w agentWorktrees) Remove(ctx context.Context, repo, path string) error {
	pid, err := w.project(ctx, repo)
	if err != nil {
		return err
	}
	list, err := w.d.Projects.Worktrees.List(ctx, pid)
	if err != nil {
		return err
	}
	for _, wt := range list {
		if !wt.IsMain && strings.EqualFold(filepath.Clean(wt.Path), filepath.Clean(path)) {
			return w.d.Projects.Worktrees.Remove(ctx, wt.ID, true)
		}
	}
	return errors.New("worktree is not known to the project registry")
}

func (w agentWorktrees) Head(ctx context.Context, dir string) (string, error) {
	return w.git.Head(ctx, dir)
}

func (w agentWorktrees) Diff(ctx context.Context, dir, base string) (agents.Diff, error) {
	return w.git.Diff(ctx, dir, base)
}

func (w agentWorktrees) DiffStat(ctx context.Context, dir, base string) (files, additions, deletions int, err error) {
	return w.git.DiffStat(ctx, dir, base)
}

// Page sizes of the agents.list method.
const (
	agentListDefault = 200
	agentListMax     = 500
)

func (d *Daemon) projectTaskDirectory(ctx context.Context, projectID, machine, dir string) (string, error) {
	if d.Projects == nil {
		return "", errors.New("project workspaces are unavailable")
	}
	return d.Projects.TaskDirectory(ctx, projectID, machine, dir, filepath.Join(d.DataDir, "project-workspaces"))
}

// agentSettings is the "agents" daemon setting: {"yoloDisabled": true} turns YOLO sessions off
// entirely and {"aiTitlesDisabled": true} stops Orole from spending one short request on the
// task's own Claude or ChatGPT account to name a new task (names chosen by the harness itself
// still apply). Absent means every option stays on. ok is false when the stored value cannot be
// read or parsed; callers fail closed.
type agentSettings struct {
	YoloDisabled     bool `json:"yoloDisabled"`
	AITitlesDisabled bool `json:"aiTitlesDisabled"`
}

func (d *Daemon) agentSettings() (v agentSettings, ok bool) {
	var raw string
	err := d.DB.QueryRow(`SELECT value FROM settings WHERE key='agents'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, true
	}
	if err != nil {
		return v, false
	}
	return v, json.Unmarshal([]byte(raw), &v) == nil
}

// yoloEnabled reports whether YOLO sessions are allowed; unreadable settings fail closed.
func (d *Daemon) yoloEnabled() bool {
	v, ok := d.agentSettings()
	return ok && !v.YoloDisabled
}

// aiTitlesEnabled reports whether a task may be named through its harness's own account;
// unreadable settings fail closed because that request sends the first prompt to the vendor.
func (d *Daemon) aiTitlesEnabled() bool {
	v, ok := d.agentSettings()
	return ok && !v.AITitlesDisabled
}

// agentTitleAccount resolves the account a task's own harness signs in with, for naming it:
// Claude Code's task uses the Claude account, Codex's the ChatGPT account, and nothing else is
// ever substituted (no OpenRouter, OpenAI key or default model). It fails unless that CLI is
// installed and signed in.
func (d *Daemon) agentTitleAccount(ctx context.Context, kind string) (agents.TitleRoute, error) {
	var id string
	switch kind {
	case "claude":
		id = cliruntime.Claude
	case "codex":
		id = cliruntime.Codex
	default:
		return agents.TitleRoute{}, fmt.Errorf("%s has no account to name tasks with", kind)
	}
	if d.CLI == nil || d.LLM == nil {
		return agents.TitleRoute{}, errors.New("account providers are unavailable")
	}
	if ok, why := d.CLI.Ready(id); !ok {
		return agents.TitleRoute{}, errors.New(why)
	}
	p, err := d.LLM.Provider(ctx, id)
	if err != nil {
		return agents.TitleRoute{}, err
	}
	models, err := d.LLM.Models(ctx, id)
	if err != nil {
		return agents.TitleRoute{}, err
	}
	return agents.TitleRoute{Provider: p, Models: models}, nil
}

func (d *Daemon) registerAgents() {
	exe, _ := os.Executable()
	var wt agents.Worktrees
	if d.Projects != nil {
		wt = agentWorktrees{d: d}
	}
	tasks := newTaskEvents(d.Events, d.reportToOwner)
	prices := pricing.Embedded()
	refresher := &pricing.Refresher{Catalog: prices, Dir: filepath.Join(d.DataDir, "pricing"), Delay: time.Minute, Log: d.Log}
	refresher.LoadCache()
	d.Agents = agents.NewManager(agents.Config{
		Store:            agents.NewStore(d.DB),
		Host:             &agents.Host{Terminals: d.Terminals, DataDir: d.DataDir, HookExe: exe, GatewayExe: d.gatewayExe},
		Log:              d.Log,
		Worktrees:        wt,
		Machines:         d.Machines,
		SkillsPrompt:     d.skillsPrompt,
		GitHub:           d.publishClient,
		ProjectDirectory: d.projectTaskDirectory,
		ProjectOptions: func(ctx context.Context) ([]agents.ProjectOption, error) {
			out := []agents.ProjectOption{}
			if d.Projects == nil {
				return out, nil
			}
			projects, err := d.Projects.List(ctx)
			if err != nil {
				return nil, err
			}
			for _, p := range projects {
				if p.MachineID == "local" && !p.Missing {
					out = append(out, agents.ProjectOption{ID: p.ID, Name: p.Name, Path: p.Path, Machine: p.MachineID, Git: p.IsGit})
				}
			}
			return out, nil
		},
		Publish: func(event string, data any) {
			d.IPC.Publish(event, data)
			tasks.observe(event, data)
		},
		Created:       tasks.created,
		Audit:         d.audit,
		YoloEnabled:   d.yoloEnabled,
		TitleAccount:  d.agentTitleAccount,
		TitlesEnabled: d.aiTitlesEnabled,
		Authorize: func(ctx context.Context, action, resource, detail string) error {
			if d.Policy == nil {
				return errors.New("permission policy is unavailable")
			}
			actor := "user"
			if chain := events.ChainFrom(ctx); len(chain) > 0 {
				actor = "automation:" + chain[len(chain)-1]
			}
			if a := agents.ActorFrom(ctx); a != "" {
				actor = a
			}
			return d.Policy.Require(ctx, policy.Request{
				Actor: actor, Action: action, Resource: resource, Detail: detail,
				Effect: policy.EffectDestructive,
			})
		},
		PermissionDecision: func(ctx context.Context, id string, req agents.PermissionRequest) error {
			if d.Policy == nil {
				return errors.New("permission policy is unavailable")
			}
			effect := policy.EffectDestructive
			switch req.Tool {
			case "Read", "Glob", "Grep":
				effect = policy.EffectRead
			}
			ev, err := d.Policy.Decide(ctx, policy.Request{
				Actor: "agent:" + id, Action: "agent.tool.use", Resource: req.Tool,
				Detail: req.Summary, Effect: effect,
			})
			if err != nil {
				return err
			}
			if ev.Decision == policy.Deny {
				return &ipc.Error{Code: ipc.CodeDenied, Message: ev.Reason}
			}
			return nil
		},
		Finished: tasks.finished,
		Prices:   prices,
	})
	orch := orchestrate.New(d.Agents, d.IPC.Publish)
	pricesCtx, stopPrices := context.WithCancel(context.Background())
	var pricesRun sync.WaitGroup
	d.agentsShutdown = func() {
		stopPrices()
		pricesRun.Wait()
		orch.Shutdown()
		d.Agents.Shutdown()
	}
	d.agentsStart = func() {
		// Only the daemon that owns the endpoint downloads prices; a refused second one never starts.
		pricesRun.Go(func() { refresher.Run(pricesCtx) })
		if lost, err := d.Agents.Recover(); err != nil {
			d.Log.Warn("agent recovery failed", "err", err)
		} else if lost > 0 {
			d.Log.Warn("agent sessions from the previous daemon run marked lost", "count", lost)
		}
	}
	m := d.Agents

	type idArg struct {
		ID string `json:"id"`
	}
	bad := func(msg string) error { return &ipc.Error{Code: ipc.CodeBadRequest, Message: msg} }
	mapErr := func(err error) error {
		switch {
		case err == nil:
			return nil
		case errors.Is(err, agents.ErrNotFound):
			return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
		}
		return err
	}
	d.registerAgentsChat(mapErr)

	handle(d, "agents.detect", func(ctx context.Context, p struct {
		Refresh bool `json:"refresh"`
	}) ([]agents.Detection, error) {
		return m.Detect(ctx, p.Refresh), nil
	})
	handle(d, "agents.options", func(ctx context.Context, p struct {
		Machine string `json:"machine"`
	}) (agents.LaunchOptions, error) {
		return m.Options(ctx, p.Machine)
	})
	handle(d, "agents.list", func(ctx context.Context, p struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}) ([]agents.Session, error) {
		limit := p.Limit
		if limit <= 0 {
			limit = agentListDefault
		}
		return m.ListPage(min(limit, agentListMax), p.Offset)
	})
	handle(d, "agents.get", func(ctx context.Context, p idArg) (agents.Session, error) {
		s, err := m.Get(p.ID)
		return s, mapErr(err)
	})
	handle(d, "agents.start", func(ctx context.Context, p agents.StartSpec) (agents.Session, error) {
		return m.Start(ctx, p)
	})
	handle(d, "agents.uploadAttachment", func(ctx context.Context, p agents.UploadAttachmentSpec) (agents.Attachment, error) {
		return m.UploadAttachment(ctx, p)
	})
	handle(d, "agents.discardAttachment", func(ctx context.Context, p idArg) (none, error) {
		return none{}, m.DiscardAttachment(ctx, p.ID)
	})
	handle(d, "agents.stop", func(ctx context.Context, p idArg) (agents.Session, error) { return m.Stop(ctx, p.ID) })
	handle(d, "agents.restart", func(ctx context.Context, p struct {
		ID     string `json:"id"`
		Fresh  bool   `json:"fresh"`
		Prompt string `json:"prompt"`
	}) (agents.Session, error) {
		return m.Restart(ctx, p.ID, p.Fresh, p.Prompt)
	})
	handle(d, "agents.prompt", func(ctx context.Context, p struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		Key  string `json:"key"`
	}) (agents.Session, error) {
		return orch.SendPrompt(ctx, p.Key, p.ID, p.Text)
	})
	handle(d, "agents.setPermissionMode", func(ctx context.Context, p struct {
		ID          string `json:"id"`
		Mode        string `json:"mode"`
		ConfirmYolo bool   `json:"confirmYolo"`
	}) (agents.Session, error) {
		s, err := m.SetPermissionMode(ctx, p.ID, p.Mode, p.ConfirmYolo)
		return s, mapErr(err)
	})
	handle(d, "agents.rename", func(ctx context.Context, p struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}) (agents.Session, error) {
		s, err := m.Rename(p.ID, p.Title)
		return s, mapErr(err)
	})
	handle(d, "agents.setPinned", func(ctx context.Context, p struct {
		ID     string `json:"id"`
		Pinned bool   `json:"pinned"`
	}) (agents.Session, error) {
		s, err := m.SetPinned(p.ID, p.Pinned)
		return s, mapErr(err)
	})
	handle(d, "agents.approve", func(ctx context.Context, p struct {
		ID        string `json:"id"`
		RequestID string `json:"requestId"`
		Allow     bool   `json:"allow"`
		Reason    string `json:"reason"`
	}) (none, error) {
		return none{}, m.Approve(ctx, p.ID, p.RequestID, p.Allow, p.Reason)
	})
	handle(d, "agents.events", func(ctx context.Context, p struct {
		ID    string `json:"id"`
		After int64  `json:"after"`
		Limit int    `json:"limit"`
	}) ([]agents.Event, error) {
		evs, err := m.Events(p.ID, p.After, p.Limit)
		return evs, mapErr(err)
	})
	handle(d, "agents.transcript", func(ctx context.Context, p struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}) ([]agents.Event, error) {
		evs, err := m.Transcript(p.ID, p.Limit)
		return evs, mapErr(err)
	})
	handle(d, "agents.remove", func(ctx context.Context, p idArg) (none, error) {
		return none{}, mapErr(m.Remove(ctx, p.ID))
	})
	handle(d, "agents.usage", func(ctx context.Context, p agents.UsageQuery) (agents.UsageStats, error) {
		s, err := m.Usage(ctx, p)
		if errors.Is(err, agents.ErrBadUsageQuery) {
			return s, bad(err.Error())
		}
		return s, err
	})
	handle(d, "agents.diff", func(ctx context.Context, p idArg) (agents.ReviewDiff, error) {
		r, err := m.Diff(ctx, p.ID)
		return r, mapErr(err)
	})
	handle(d, "agents.review", func(ctx context.Context, p struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}) (agents.Session, error) {
		return m.Review(ctx, p.ID, p.Decision, p.Note)
	})
	handle(d, "agents.check", func(ctx context.Context, p struct {
		ID      string `json:"id"`
		Command string `json:"command"`
	}) (agents.Session, error) {
		return m.Check(ctx, p.ID, p.Command)
	})
	handle(d, "agents.hook", func(ctx context.Context, p struct {
		SessionID string          `json:"sessionId"`
		Event     string          `json:"event"`
		Payload   json.RawMessage `json:"payload"`
	}) (none, error) {
		return none{}, m.Hook(p.SessionID, p.Event, p.Payload)
	})

	// Orchestration.
	handle(d, "agents.handoff", func(ctx context.Context, p struct {
		orchestrate.HandOffParams
		Key string `json:"key"`
	}) (agents.Session, error) {
		return orch.HandOff(ctx, p.Key, p.HandOffParams)
	})
	handle(d, "agents.reviewWith", func(ctx context.Context, p struct {
		orchestrate.ReviewParams
		Key string `json:"key"`
	}) (agents.Session, error) {
		return orch.ReviewWith(ctx, p.Key, p.ReviewParams)
	})
	handle(d, "agents.loopUntil", func(ctx context.Context, p struct {
		orchestrate.LoopParams
		Key string `json:"key"`
	}) (agents.Run, error) {
		if p.Session == "" {
			return agents.Run{}, bad("session is required")
		}
		return orch.LoopUntil(ctx, p.Key, p.LoopParams)
	})
	handle(d, "agents.tryN", func(ctx context.Context, p struct {
		orchestrate.TryParams
		Key string `json:"key"`
	}) (orchestrate.TryResult, error) {
		return orch.TryN(ctx, p.Key, p.TryParams)
	})
	handle(d, "agents.runs", func(ctx context.Context, _ none) ([]agents.Run, error) { return orch.Runs() })
	handle(d, "agents.cancelRun", func(ctx context.Context, p idArg) (none, error) {
		return none{}, orch.Cancel(p.ID)
	})
	if d.Automations != nil {
		d.Automations.SetAgentControl(agentAutomation{d: d, orch: orch})
	}
}

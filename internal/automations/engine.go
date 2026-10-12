package automations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/automations/expr"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	DB          *sql.DB
	Bus         *events.Bus
	Credentials credentials.Store
	Clock       Clock
	Log         *slog.Logger
	Audit       func(string, string, string, string)
	Changed     func(Run)
	Commands    CommandRunner
	Notifier    Notifier
	// Authorize must use the daemon's shared policy. Nil denies native effects.
	Authorize func(context.Context, policy.Request) error
}
type Engine struct {
	db          *sql.DB
	bus         *events.Bus
	credentials credentials.Store
	clock       Clock
	log         *slog.Logger
	redact      Redactor
	auditFn     func(string, string, string, string)
	changedFn   func(Run)
	mu          sync.Mutex
	deps        sync.RWMutex
	config      Config
	tools       ToolInvoker
	agents      AgentControl
	github      GitHubQuery
	commands    CommandRunner
	notifier    Notifier
	authorize   func(context.Context, policy.Request) error
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	queue       []string
	live        map[string]Run // queued, running and waiting-approval runs, mirrored from SQLite
	liveMu      sync.Mutex     // never held while taking mu
	defMu       sync.Mutex
	defSnap     *defSnapshot
	secretMu    sync.RWMutex
	secret      []byte
	nextPrune   time.Time // scheduler goroutine only
	pruning     atomic.Bool
	active      map[string]context.CancelFunc
	counts      map[string]int
	wakeC       chan struct{}
	started     bool
	http        SafeHTTP
	webhook     *webhookServer
}

func New(o Options) *Engine {
	if o.Clock == nil {
		o.Clock = realClock{}
	}
	if o.Bus == nil {
		o.Bus = events.NewBus()
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Commands == nil {
		o.Commands = ExecRunner{}
	}
	e := &Engine{db: o.DB, bus: o.Bus, credentials: o.Credentials, clock: o.Clock, log: o.Log, auditFn: o.Audit, changedFn: o.Changed, commands: o.Commands, notifier: o.Notifier, authorize: o.Authorize, config: DefaultConfig(), active: map[string]context.CancelFunc{}, live: map[string]Run{}, counts: map[string]int{}, wakeC: make(chan struct{}, 1)}
	var raw string
	if err := e.db.QueryRow(`SELECT value FROM settings WHERE key='automations.config'`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &e.config)
	}
	return e
}
func (e *Engine) SetToolInvoker(v ToolInvoker)   { e.deps.Lock(); e.tools = v; e.deps.Unlock() }
func (e *Engine) SetAgentControl(v AgentControl) { e.deps.Lock(); e.agents = v; e.deps.Unlock() }
func (e *Engine) SetGitHubQuery(v GitHubQuery)   { e.deps.Lock(); e.github = v; e.deps.Unlock() }
func (e *Engine) RegisterSecret(v string)        { e.redact.Register(v) }
func (e *Engine) StepTypes() []StepType {
	e.deps.RLock()
	defer e.deps.RUnlock()
	list := catalog()
	for i := range list {
		s := &list[i]
		switch s.Type {
		case "mcp.tool":
			s.Available = e.tools != nil
		case "agent.start", "agent.prompt", "agent.wait":
			s.Available = e.agents != nil
		case "github.query":
			s.Available = e.github != nil
		case "notify":
			s.Available = e.notifier != nil
		}
		if !s.Available {
			s.Reason = "Backing integration is not available in this daemon"
		}
	}
	return list
}
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return nil
	}
	e.ctx, e.cancel = context.WithCancel(ctx)
	e.started = true
	rows, err := e.db.Query(`SELECT ` + runColumns + ` FROM automation_runs WHERE status IN ('queued','running') ORDER BY created_at`)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	for rows.Next() {
		var r Run
		if r, err = scanRun(rows); err != nil {
			break
		}
		e.rememberRun(r)
		e.queue = append(e.queue, r.ID)
	}
	rows.Close()
	if err == nil {
		err = rows.Err()
	}
	if err != nil {
		e.mu.Unlock()
		return err
	}
	e.pumpLocked()
	e.wg.Add(1)
	e.mu.Unlock()
	go e.loop()
	if e.config.WebhookEnabled {
		if err := e.startWebhook(e.config.WebhookPort); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) Close() {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()
	e.stopWebhook()
	e.wg.Wait()
}
func (e *Engine) wake() {
	select {
	case e.wakeC <- struct{}{}:
	default:
	}
}
func (e *Engine) audit(action, id, outcome, detail string) {
	if e.auditFn != nil {
		e.auditFn(action, id, outcome, e.redact.Text(detail))
	}
}

// changed publishes a run summary from memory; only a run that is no longer
// tracked (a late journal write after it finished) costs a SQLite read.
func (e *Engine) changed(id string) {
	if e.changedFn == nil {
		return
	}
	r, err := e.runState(context.Background(), id)
	if err != nil {
		return
	}
	e.changedFn(r)
}
func (e *Engine) finished(r Run) {
	if terminal(r.Status) {
		e.bus.Publish(events.Event{Topic: events.TopicAutomationRun, Chain: r.Chain, Data: map[string]any{"automationId": r.AutomationID, "runId": r.ID, "status": r.Status, "name": r.Name}})
	}
}
func (e *Engine) Run(ctx context.Context, id string) (Run, error) {
	return e.trigger(ctx, id, "manual", nil, nil, "")
}
func (e *Engine) trigger(ctx context.Context, id, kind string, data any, chain []string, key string) (Run, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started || e.ctx.Err() != nil {
		return Run{}, fmt.Errorf("automation engine is not running")
	}
	if len(chain) >= MaxDepth {
		return Run{}, fmt.Errorf("trigger chain exceeds maximum depth %d", MaxDepth)
	}
	for _, p := range chain {
		if p == id {
			return Run{}, fmt.Errorf("automation cannot trigger itself through its provenance chain")
		}
	}
	entry, err := e.definitionEntry(ctx, id)
	if err != nil {
		return Run{}, err
	}
	d := entry.def
	if kind != "manual" && !d.Enabled {
		return Run{}, fmt.Errorf("automation is disabled")
	}
	if issues := entry.issues; len(issues) > 0 {
		return Run{}, fmt.Errorf("invalid definition: %s: %s", issues[0].Path, issues[0].Message)
	}
	var pending int
	if err = e.db.QueryRowContext(ctx, `SELECT count(*) FROM automation_runs WHERE automation_id=? AND status IN ('queued','running','waiting-approval')`, id).Scan(&pending); err != nil {
		return Run{}, err
	}
	if pending >= 100 {
		return Run{}, fmt.Errorf("automation queue is full")
	}
	chain = append(append([]string{}, chain...), id)
	b, err := json.Marshal(data)
	if err != nil {
		return Run{}, err
	}
	if len(b) > 128<<10 {
		return Run{}, fmt.Errorf("trigger data exceeds limit")
	}
	b = e.redact.JSON(b)
	cb, _ := json.Marshal(chain)
	r := Run{ID: uuid.NewString(), AutomationID: id, Name: d.Name, Status: "queued", TriggerType: kind, TriggerData: b, Chain: chain, Definition: d, CreatedAt: e.clock.Now().UnixMilli()}
	var triggerKey any
	if key != "" {
		triggerKey = key
	}
	_, err = e.db.ExecContext(ctx, `INSERT INTO automation_runs(id,automation_id,status,trigger_type,trigger_key,trigger_data,chain,definition,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, r.ID, id, r.Status, kind, triggerKey, string(b), string(cb), entry.raw, r.CreatedAt)
	if err != nil {
		if key != "" {
			existing, x := scanRun(e.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM automation_runs WHERE trigger_key=?`, key))
			if x == nil {
				return existing, nil
			}
		}
		return Run{}, err
	}
	e.audit("automation.run.start", r.ID, "allowed", kind)
	e.rememberRun(r)
	e.queue = append(e.queue, r.ID)
	e.pumpLocked()
	return r, nil
}
func (e *Engine) pumpLocked() {
	if !e.started || e.ctx.Err() != nil {
		return
	}
	for i := 0; i < len(e.queue) && len(e.active) < e.config.GlobalConcurrency; {
		id := e.queue[i]
		r, err := e.runState(e.ctx, id)
		if err != nil || terminal(r.Status) {
			e.queue = append(e.queue[:i], e.queue[i+1:]...)
			continue
		}
		limit := r.Definition.Concurrency
		if limit == 0 {
			limit = e.config.PerAutomation
		}
		if e.counts[r.AutomationID] >= limit {
			i++
			continue
		}
		if _, ok := e.active[id]; ok {
			e.queue = append(e.queue[:i], e.queue[i+1:]...)
			continue
		}
		e.queue = append(e.queue[:i], e.queue[i+1:]...)
		remaining := time.Duration(r.Definition.TimeoutSec) * time.Second
		if r.StartedAt > 0 {
			remaining -= e.clock.Now().Sub(time.UnixMilli(r.StartedAt))
		}
		ctx, cancel := context.WithTimeout(e.ctx, remaining)
		e.active[id] = cancel
		e.counts[r.AutomationID]++
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			defer cancel()
			err := e.execute(events.WithChain(ctx, r.Chain), r)
			if err != nil {
				e.log.Error("automation runner stopped", "run", id, "error", e.redact.Text(err.Error()))
			}
			e.mu.Lock()
			delete(e.active, id)
			e.counts[r.AutomationID]--
			e.pumpLocked()
			e.mu.Unlock()
		}()
	}
}
func (e *Engine) Cancel(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.runState(ctx, id)
	if err != nil {
		return err
	}
	if terminal(r.Status) {
		return nil
	}
	if err = e.status(id, "canceled", "Canceled by user"); err != nil {
		return err
	}
	if cancel := e.active[id]; cancel != nil {
		cancel()
	}
	return nil
}
func keyFor(run, step string) string {
	x := sha256.Sum256([]byte(run + "/" + step))
	return hex.EncodeToString(x[:])
}
func (e *Engine) execute(ctx context.Context, r Run) error {
	current, err := e.runState(ctx, r.ID)
	if err != nil {
		return err
	}
	if terminal(current.Status) {
		return nil
	}
	steps, err := e.loadJournals(ctx, r.ID)
	if err != nil {
		return err
	}
	if err = e.status(r.ID, "running", ""); err != nil {
		return err
	}
	journals := map[string]Journal{}
	for _, j := range steps {
		journals[j.StepID] = j
	}
	var trigger any
	_ = json.Unmarshal(r.TriggerData, &trigger)
	outputs := map[string]any{}
	env := map[string]any{"trigger": map[string]any{"type": r.TriggerType, "data": trigger}, "event": trigger, "steps": outputs, "run": map[string]any{"id": r.ID, "automationId": r.AutomationID}}
	for i := 0; i < len(r.Definition.Steps); {
		s := r.Definition.Steps[i]
		j, exists := journals[s.ID]
		sp, _ := spec(s.Type)
		if exists && j.Status == "running" && sp.SideEffect && s.Strategy == "at-most-once" {
			j.Status = "uncertain"
			j.Error = "Daemon stopped during a side-effecting step; it was not repeated"
			j.FinishedAt = e.clock.Now().UnixMilli()
			if err = e.journal(j); err != nil {
				return err
			}
			return e.status(r.ID, "uncertain", j.Error)
		}
		if !exists || j.Status == "running" || j.Status == "authorizing" {
			j, err = e.runStep(ctx, r, s, i, env, j)
			if err != nil {
				return err
			}
		}
		if j.Status == "waiting-approval" {
			return e.status(r.ID, "waiting-approval", "")
		}
		var output any
		_ = json.Unmarshal(j.Output, &output)
		outputs[s.ID] = map[string]any{"output": output, "status": j.Status, "error": j.Error}
		if j.Status == "uncertain" {
			return e.status(r.ID, "uncertain", j.Error)
		}
		if j.Status == "canceled" {
			return e.status(r.ID, "canceled", j.Error)
		}
		if j.Status == "failed" && s.OnError != "continue" {
			return e.status(r.ID, "failed", j.Error)
		}
		if s.Type == "branch" && j.Status == "succeeded" {
			m, _ := output.(map[string]any)
			target, _ := m["next"].(string)
			if target == "end" {
				break
			}
			found := false
			for next := i + 1; next < len(r.Definition.Steps); next++ {
				if r.Definition.Steps[next].ID == target {
					i = next
					found = true
					break
				}
			}
			if !found {
				return e.status(r.ID, "failed", "Invalid branch target")
			}
			continue
		}
		i++
	}
	if ctx.Err() != nil {
		return e.interrupted(r.ID, ctx.Err())
	}
	return e.status(r.ID, "succeeded", "")
}
func (e *Engine) interrupted(id string, err error) error {
	if e.ctx.Err() != nil {
		return nil
	}
	r, x := e.runState(context.Background(), id)
	if x != nil {
		return x
	}
	if r.Status == "canceled" {
		return nil
	}
	return e.status(id, "failed", err.Error())
}
func (e *Engine) runStep(ctx context.Context, r Run, s Step, seq int, env map[string]any, previous Journal) (Journal, error) {
	j := Journal{RunID: r.ID, StepID: s.ID, Seq: seq, Type: s.Type, Status: "running", Key: keyFor(r.ID, s.ID), StartedAt: e.clock.Now().UnixMilli(), Attempt: previous.Attempt}
	fail := func(err error) (Journal, error) {
		j.Status = "failed"
		j.Error = err.Error()
		j.FinishedAt = e.clock.Now().UnixMilli()
		return j, e.journal(j)
	}
	if ctx.Err() != nil {
		return j, e.interrupted(r.ID, ctx.Err())
	}
	if s.When != "" {
		ex, err := expr.Parse(s.When)
		if err != nil {
			return fail(err)
		}
		yes, err := ex.EvalBool(env)
		if err != nil {
			return fail(err)
		}
		if !yes {
			j.Status = "skipped"
			j.FinishedAt = e.clock.Now().UnixMilli()
			return j, e.journal(j)
		}
	}
	var raw map[string]any
	if err := json.Unmarshal(s.Params, &raw); err != nil {
		return fail(err)
	}
	rendered, err := renderValue(raw, env, false, 0)
	if err != nil {
		return fail(err)
	}
	params := rendered.(map[string]any)
	sp, _ := spec(s.Type)
	if issues := validateParams(sp, params, true); len(issues) > 0 {
		return fail(fmt.Errorf("%s: %s", issues[0].Path, issues[0].Message))
	}
	j.Input, _ = json.Marshal(params)
	if s.Type == "approval" {
		hours, _ := params["expiresHours"].(float64)
		if hours > 0 {
			params["expiresAt"] = e.clock.Now().Add(time.Duration(hours * float64(time.Hour))).UnixMilli()
		}
		j.Input, _ = json.Marshal(params)
		j.Status = "waiting-approval"
		return j, e.journal(j)
	}
	for attempt := 0; attempt <= s.Retries; attempt++ {
		if ctx.Err() != nil {
			return j, e.interrupted(r.ID, ctx.Err())
		}
		j.Attempt++
		j.Status = "running"
		if nativeEffect(s.Type) {
			j.Status = "authorizing"
		}
		j.FinishedAt = 0
		if err = e.journal(j); err != nil {
			return j, err
		}
		stepCtx, cancel := context.WithTimeout(ctx, time.Duration(s.TimeoutSec)*time.Second)
		out, runErr := e.invoke(stepCtx, r, s, params, j.Key, env, func() error {
			if err := stepCtx.Err(); err != nil {
				return err
			}
			// Persist the dispatch boundary only once, including across redirects.
			// A restart before this boundary may safely request approval again.
			if j.Status != "running" {
				j.Status = "running"
				if err := e.journal(j); err != nil {
					return err
				}
			}
			return stepCtx.Err()
		})
		cancel()
		if e.ctx.Err() != nil {
			return j, errors.New("daemon stopped; unfinished journal retained")
		}
		b, marshalErr := json.Marshal(out)
		if marshalErr != nil {
			runErr = marshalErr
		}
		j.Output = e.redact.JSON(b)
		if ctx.Err() != nil {
			j.Status = "canceled"
			j.Error = ctx.Err().Error()
			j.FinishedAt = e.clock.Now().UnixMilli()
			if err = e.journal(j); err != nil {
				return j, err
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				j.Status = "failed"
				_ = e.journal(j)
			}
			return j, nil
		}
		if runErr == nil {
			j.Status = "succeeded"
			j.Error = ""
			j.FinishedAt = e.clock.Now().UnixMilli()
			return j, e.journal(j)
		}
		j.Error = runErr.Error()
		var denied *authorizationError
		if errors.As(runErr, &denied) {
			break
		}
		if attempt < s.Retries {
			delay := time.Duration(s.BackoffMs) * time.Millisecond * (1 << attempt)
			if delay > time.Minute {
				delay = time.Minute
			}
			select {
			case <-ctx.Done():
				return j, e.interrupted(r.ID, ctx.Err())
			case <-e.clock.After(delay):
			}
		}
	}
	j.Status = "failed"
	j.FinishedAt = e.clock.Now().UnixMilli()
	return j, e.journal(j)
}

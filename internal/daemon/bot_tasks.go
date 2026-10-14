package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/tools"
	"github.com/rayorole/orole-os-desktop/internal/events"
)

// botTasks lets bots start, watch and steer code tasks through the same agent runtime the
// user drives. The actor ("bot:<id>") goes into the context: the agent runtime records it
// as the task's owner, asks the shared policy as that actor, and gates YOLO. The harness,
// model, reasoning and permission mode come from the user's settings (botTaskDefaults); a bot
// chooses only what those leave to it.
type botTasks struct {
	d       *Daemon
	options *harnessCache
}

func newBotTasks(d *Daemon) botTasks { return botTasks{d: d, options: &harnessCache{}} }

func (b botTasks) Projects(ctx context.Context) ([]tools.TaskProject, error) {
	if b.d.Projects == nil {
		return nil, errors.New("projects are unavailable")
	}
	list, err := b.d.Projects.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []tools.TaskProject{}
	for _, p := range list {
		if p.MachineID == "local" && !p.Missing && p.Path != "" {
			out = append(out, tools.TaskProject{ID: p.ID, Name: p.Name, Git: p.IsGit})
		}
	}
	return out, nil
}

func (b botTasks) Choices(ctx context.Context, bot chat.Bot) (tools.TaskChoices, error) {
	return taskChoices(b.d.botTaskDefaults(bot.Config.Tasks), b.harnesses(ctx, false)), nil
}

func (b botTasks) Start(ctx context.Context, actor string, bot chat.Bot, spec tools.TaskStart) (tools.TaskInfo, error) {
	p, err := b.d.Projects.Get(ctx, spec.ProjectID)
	if err != nil {
		return tools.TaskInfo{}, fmt.Errorf("unknown project %q", spec.ProjectID)
	}
	installed := b.harnesses(ctx, true)
	plan, err := planBotTask(b.d.botTaskDefaults(bot.Config.Tasks), installed, spec)
	if err != nil {
		return tools.TaskInfo{}, err
	}
	s, err := b.d.Agents.Start(agents.WithCall(agents.WithActor(ctx, actor), spec.CallID), agents.StartSpec{
		Kind: plan.Harness, ProjectID: p.ID, Machine: p.MachineID, Prompt: spec.Prompt, Title: spec.Title,
		Model: plan.Model, Reasoning: plan.Reasoning, NewWorktree: spec.NewWorktree,
		// The mode is always the user's setting, named explicitly so the runtime never swaps it for
		// another; a YOLO setting is the user's confirmation, and the runtime still refuses it unless
		// "Bots may use Yolo" is on.
		PermissionMode: plan.PermissionMode, ConfirmYolo: plan.PermissionMode == agents.PermYolo,
	})
	if err != nil {
		return tools.TaskInfo{}, err
	}
	info := taskInfo(s, false)
	if h := slices.IndexFunc(installed, func(h agents.HarnessOption) bool { return h.ID == plan.Harness }); h >= 0 {
		if m := slices.IndexFunc(installed[h].Models, func(m agents.ModelOption) bool { return m.ID == s.Model }); m >= 0 && installed[h].Models[m].Name != s.Model {
			info.ModelName = installed[h].Models[m].Name
		}
	}
	return info, nil
}

// taskChoices is what d leaves a bot to choose, over the installed harnesses.
func taskChoices(d chat.BotTaskDefaults, installed []agents.HarnessOption) tools.TaskChoices {
	out := tools.TaskChoices{Harness: d.Harness, Model: d.Model, Reasoning: d.Reasoning, PermissionMode: d.PermissionMode, Harnesses: []tools.TaskHarness{}}
	for _, h := range installed {
		if d.Harness != chat.TaskChoiceAuto && h.ID != d.Harness {
			continue
		}
		models := make([]string, 0, len(h.Models))
		names := map[string]string{}
		for _, m := range h.Models {
			models = append(models, m.ID)
			if m.Name != "" && m.Name != m.ID {
				names[m.ID] = m.Name
			}
		}
		out.Harnesses = append(out.Harnesses, tools.TaskHarness{ID: h.ID, Models: models, ModelNames: names, Reasoning: h.Reasoning, CustomModel: h.CustomModel})
	}
	return out
}

// planBotTask resolves the harness, model and reasoning of a bot's task: a fixed value in d wins
// over spec; an auto one takes the bot's choice, which must be one the harness offers. The
// permission mode is d's.
func planBotTask(d chat.BotTaskDefaults, installed []agents.HarnessOption, spec tools.TaskStart) (chat.BotTaskDefaults, error) {
	ids := make([]string, 0, len(installed))
	for _, h := range installed {
		ids = append(ids, h.ID)
	}
	choose := "no coding agent is installed; ask the user to install Claude Code or Codex"
	if len(ids) > 0 {
		choose = "choose one of: " + strings.Join(ids, ", ")
	}
	out := chat.BotTaskDefaults{Harness: d.Harness, Model: d.Model, Reasoning: d.Reasoning, PermissionMode: d.PermissionMode}
	if d.Harness == chat.TaskChoiceAuto {
		if out.Harness = strings.TrimSpace(spec.Harness); out.Harness == "" {
			return out, errors.New("name the harness to run the task with; " + choose)
		}
	}
	i := slices.IndexFunc(installed, func(h agents.HarnessOption) bool { return h.ID == out.Harness })
	if i < 0 {
		if d.Harness != chat.TaskChoiceAuto {
			return out, fmt.Errorf("the harness %q set for your tasks in Orole's settings is not installed; ask the user to install it or change the setting", d.Harness)
		}
		return out, fmt.Errorf("harness %q is not installed; %s", out.Harness, choose)
	}
	h := installed[i]
	var model *agents.ModelOption
	if d.Model == chat.TaskChoiceAuto {
		out.Model = strings.TrimSpace(spec.Model)
		if out.Model != "" {
			if j := slices.IndexFunc(h.Models, func(m agents.ModelOption) bool { return m.ID == out.Model }); j >= 0 {
				model = &h.Models[j]
			} else if len(h.Models) > 0 && !h.CustomModel {
				return out, fmt.Errorf("%s does not offer model %q; use one of: %s", h.ID, out.Model, strings.Join(modelIDs(h.Models), ", "))
			}
		}
	}
	if d.Reasoning == chat.TaskChoiceAuto {
		out.Reasoning = strings.TrimSpace(spec.Reasoning)
		levels := h.Reasoning
		if model != nil && len(model.Reasoning) > 0 {
			levels = model.Reasoning
		}
		if out.Reasoning != "" && len(levels) > 0 && !slices.Contains(levels, out.Reasoning) {
			return out, fmt.Errorf("%s does not offer reasoning effort %q here; use one of: %s", h.ID, out.Reasoning, strings.Join(levels, ", "))
		}
	}
	return out, nil
}

func modelIDs(list []agents.ModelOption) []string {
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, m.ID)
	}
	return out
}

// harnessOptionsTTL is how long the installed harnesses' models and reasoning levels are reused:
// listing them runs the CLIs.
const harnessOptionsTTL = 5 * time.Minute

// harnessCache holds the local harnesses bots may use. One refresh runs at a time, in the
// background, so a caller with a short deadline (a tool list) gets the last list instead of
// waiting on the CLIs.
type harnessCache struct {
	mu      sync.Mutex
	at      time.Time
	list    []agents.HarnessOption // nil until the first refresh ends
	pending chan struct{}          // closed when the running refresh ends; nil when none runs
}

// harnesses returns the installed local harnesses other than "custom", with the models and
// reasoning levels each advertises. A stale list starts a refresh; wait waits for it (until ctx
// ends), otherwise the stale list is returned at once. With nothing listed yet it waits until ctx
// ends and then falls back to detection alone (no models or levels).
func (b botTasks) harnesses(ctx context.Context, wait bool) []agents.HarnessOption {
	c := b.options
	c.mu.Lock()
	if c.list != nil && time.Since(c.at) < harnessOptionsTTL {
		defer c.mu.Unlock()
		return c.list
	}
	if c.pending == nil {
		c.pending = make(chan struct{})
		go b.refreshHarnesses(c.pending)
	}
	pending, list := c.pending, c.list
	c.mu.Unlock()
	if list != nil && !wait {
		return list
	}
	select {
	case <-pending:
	case <-ctx.Done():
	}
	c.mu.Lock()
	list = c.list
	c.mu.Unlock()
	if list != nil {
		return list
	}
	out := []agents.HarnessOption{}
	for _, det := range b.d.Agents.Detect(ctx, false) {
		if det.Installed && det.Kind != "custom" {
			out = append(out, agents.HarnessOption{ID: det.Kind, Name: det.Name, Available: true, CustomModel: true})
		}
	}
	return out
}

// refreshHarnesses lists the harnesses for the cache and closes done. A listing cut short by its
// deadline only replaces an empty cache, and stays stale so the next caller refreshes again.
func (b botTasks) refreshHarnesses(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opts, _ := b.d.Agents.Options(ctx, "local") // the harness list survives a failed project or machine lookup
	list := []agents.HarnessOption{}
	for _, h := range opts.Harnesses {
		if h.Available && h.ID != "custom" {
			list = append(list, h)
		}
	}
	c := b.options
	c.mu.Lock()
	switch {
	case ctx.Err() == nil:
		c.list, c.at = list, time.Now()
	case c.list == nil:
		c.list, c.at = list, time.Time{}
	}
	c.pending = nil
	c.mu.Unlock()
	close(done)
}

func (b botTasks) List(ctx context.Context, limit int) ([]tools.TaskInfo, error) {
	list, err := b.d.Agents.ListPage(limit, 0)
	if err != nil {
		return nil, err
	}
	out := make([]tools.TaskInfo, 0, len(list))
	for _, s := range list {
		out = append(out, taskInfo(s, false))
	}
	return out, nil
}

func (b botTasks) Get(ctx context.Context, id string, transcript int) (tools.TaskInfo, []tools.TaskLine, error) {
	s, err := b.d.Agents.Get(id)
	if err != nil {
		return tools.TaskInfo{}, nil, fmt.Errorf("no task %q", id)
	}
	var lines []tools.TaskLine
	if transcript > 0 {
		events, err := b.d.Agents.Transcript(id, transcript)
		if err != nil {
			return tools.TaskInfo{}, nil, err
		}
		for _, e := range events {
			if strings.TrimSpace(e.Text) != "" {
				lines = append(lines, tools.TaskLine{Kind: e.Kind, Text: clipTo(e.Text, botTextMax)})
			}
		}
	}
	return taskInfo(s, true), lines, nil
}

func (b botTasks) Send(ctx context.Context, actor, id, text string) (tools.TaskInfo, error) {
	s, err := b.d.Agents.SendMessage(agents.WithActor(ctx, actor), id, text, nil, "", "")
	if err != nil {
		return tools.TaskInfo{}, err
	}
	return taskInfo(s, false), nil
}

func (b botTasks) Stop(ctx context.Context, actor, id string) (tools.TaskInfo, error) {
	s, err := b.d.Agents.Stop(agents.WithActor(ctx, actor), id)
	if err != nil {
		return tools.TaskInfo{}, err
	}
	return taskInfo(s, false), nil
}

var _ tools.TaskRuntime = botTasks{}

// botTextMax bounds one text a bot reads from a task (its result, last message, a transcript
// line): the runtime keeps 32 KiB of each, and a report is the task's deliverable, so a bot sees
// all of it. Lists stay at the event clip.
const botTextMax = 32 << 10

// taskInfo maps a session for a bot. full keeps the result and last message whole (task_status
// and reports); otherwise they are clipped for lists.
func taskInfo(s agents.Session, full bool) tools.TaskInfo {
	bound := clip
	if full {
		bound = func(text string) string { return clipTo(text, botTextMax) }
	}
	return tools.TaskInfo{
		ID: s.ID, Title: s.Title, Harness: s.Kind, Project: s.Project, ProjectID: s.ProjectID, Status: string(s.Status), Live: s.Live,
		Activity: clip(s.Activity), LastMessage: bound(s.LastMessage), Result: bound(s.Result), Error: clip(s.Error),
		Branch: s.Branch, PRURL: s.PRURL, Files: s.Files, Additions: s.Additions, Deletions: s.Deletions,
		Turns: s.Turns, Subagents: s.Subagents, Model: s.Model, Reasoning: s.Reasoning, PermissionMode: s.PermissionMode,
		Waiting: waitingFor(s), Owner: s.Owner, UpdatedAt: s.UpdatedAt.UnixMilli(),
	}
}

// waitingFor says what a task asks the user, "" when it asks nothing.
func waitingFor(s agents.Session) string {
	var parts []string
	for _, p := range s.Pending {
		parts = append(parts, "permission for "+p.Tool+": "+p.Summary)
	}
	for _, q := range s.Questions {
		for _, x := range q.Questions {
			parts = append(parts, "question: "+x.Prompt)
		}
	}
	return clip(strings.Join(parts, "\n"))
}

// reportToOwner tells the bot that started s what happened. It runs off the agent runtime's
// publish path: storing the report takes the bot's lock and writes SQLite. A task started on a
// phone call reports into that call's thread (Session.CallID).
func (d *Daemon) reportToOwner(topic string, s agents.Session) {
	botID, ok := strings.CutPrefix(s.Owner, chat.BotPrefix)
	if !ok || d.Chat == nil {
		return
	}
	r := chat.TaskReport{BotID: botID, TaskID: s.ID, Title: s.Title, Status: taskReportStatus(topic), Text: taskReportText(topic, s), CallID: s.CallID}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := d.Chat.ReportTask(ctx, r); err != nil {
			d.Log.Warn("task report not delivered", "task", s.ID, "bot", botID, "err", err)
		}
	}()
}

// taskReportStatus is chat.FollowUp.Status for a task topic: "finished", "waiting" or "failed".
func taskReportStatus(topic string) string {
	switch topic {
	case events.TopicAgentFinished:
		return "finished"
	case events.TopicAgentWaiting:
		return "waiting"
	}
	return "failed"
}

// taskReportText is what the owning bot reads when s changed state. A finished task's report
// carries its whole result (the deliverable), not a clipped last message; waiting and failed
// reports carry what the task asks or the error plus its last message.
func taskReportText(topic string, s agents.Session) string {
	var b strings.Builder
	what := map[string]string{"agent.finished": "finished", "agent.waiting": "needs the user", "agent.failed": "failed"}[topic]
	fmt.Fprintf(&b, "Task %q (%s, id %s) %s.\nStatus: %s", s.Title, s.Kind, s.ID, what, s.Status)
	if topic == "agent.finished" {
		fmt.Fprintf(&b, " · %d turn(s) completed, no background agents running", s.Turns)
	}
	if s.Branch != "" {
		fmt.Fprintf(&b, " · branch %s", s.Branch)
	}
	if s.Files > 0 {
		fmt.Fprintf(&b, " · %d files (+%d −%d)", s.Files, s.Additions, s.Deletions)
	}
	if s.PRURL != "" {
		fmt.Fprintf(&b, " · %s", s.PRURL)
	}
	if w := waitingFor(s); w != "" {
		fmt.Fprintf(&b, "\nWaiting for:\n%s", w)
	}
	if s.Error != "" {
		fmt.Fprintf(&b, "\nError: %s", clip(s.Error))
	}
	result := s.Result
	if topic != "agent.finished" || result == "" {
		result = s.LastMessage
	}
	if result != "" {
		label := "Last message"
		if topic == "agent.finished" {
			label = "Result (complete)"
		}
		fmt.Fprintf(&b, "\n%s:\n%s", label, clipTo(result, botTextMax))
	}
	return b.String()
}

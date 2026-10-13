package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/tools"
)

// botTasks lets bots start, watch and steer code tasks through the same agent runtime the
// user drives. The actor ("bot:<id>") goes into the context: the agent runtime records it
// as the task's owner, asks the shared policy as that actor, and refuses YOLO.
type botTasks struct{ d *Daemon }

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

func (b botTasks) Harnesses(ctx context.Context) ([]string, error) {
	out := []string{}
	for _, det := range b.d.Agents.Detect(ctx, false) {
		if det.Installed && det.Kind != "custom" {
			out = append(out, det.Kind)
		}
	}
	return out, nil
}

func (b botTasks) Start(ctx context.Context, actor string, spec tools.TaskStart) (tools.TaskInfo, error) {
	p, err := b.d.Projects.Get(ctx, spec.ProjectID)
	if err != nil {
		return tools.TaskInfo{}, fmt.Errorf("unknown project %q", spec.ProjectID)
	}
	s, err := b.d.Agents.Start(agents.WithActor(ctx, actor), agents.StartSpec{
		Kind: spec.Harness, ProjectID: p.ID, Machine: p.MachineID, Prompt: spec.Prompt, Title: spec.Title,
		// A bot's YOLO request is the user's choice: the runtime only honours it with "Bots may use Yolo" on.
		PermissionMode: spec.PermissionMode, ConfirmYolo: spec.PermissionMode == agents.PermYolo, NewWorktree: spec.NewWorktree,
	})
	if err != nil {
		return tools.TaskInfo{}, err
	}
	return taskInfo(s, false), nil
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
		Turns: s.Turns, Subagents: s.Subagents, PermissionMode: s.PermissionMode,
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
// publish path: storing the report takes the bot's lock and writes SQLite.
func (d *Daemon) reportToOwner(topic string, s agents.Session) {
	botID, ok := strings.CutPrefix(s.Owner, chat.BotPrefix)
	if !ok || d.Chat == nil {
		return
	}
	text := taskReportText(topic, s)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := d.Chat.ReportTask(ctx, botID, s.ID, text); err != nil {
			d.Log.Warn("task report not delivered", "task", s.ID, "bot", botID, "err", err)
		}
	}()
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

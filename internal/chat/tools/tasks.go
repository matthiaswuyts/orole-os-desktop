package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Code-task tool names.
const (
	toolStartTask   = "start_task"
	toolListTasks   = "list_tasks"
	toolTaskStatus  = "task_status"
	toolMessageTask = "message_task"
	toolStopTask    = "stop_task"
)

// TaskInfo is what a bot sees of a code task. Activity, LastMessage, Result, Error and the
// transcript are the agent's own words: untrusted data.
type TaskInfo struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Harness        string `json:"harness"`
	Project        string `json:"project"`
	ProjectID      string `json:"projectId,omitempty"`
	Status         string `json:"status"`
	Live           bool   `json:"live"`
	Activity       string `json:"activity,omitempty"`
	LastMessage    string `json:"lastMessage,omitempty"`
	Result         string `json:"result,omitempty"`
	Error          string `json:"error,omitempty"`
	Branch         string `json:"branch,omitempty"`
	PRURL          string `json:"prUrl,omitempty"`
	Files          int    `json:"files"`
	Additions      int    `json:"additions"`
	Deletions      int    `json:"deletions"`
	Turns          int    `json:"turns"`               // completed turns; a task can take several before it is finished
	Subagents      int    `json:"subagents,omitempty"` // background agents still running: the task is not finished while > 0
	PermissionMode string `json:"permissionMode,omitempty"`
	Waiting        string `json:"waitingFor,omitempty"` // what the task asks the user, when it needs them
	Owner          string `json:"owner,omitempty"`
	UpdatedAt      int64  `json:"updatedAt"`
}

// TaskLine is one transcript entry of a task.
type TaskLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// TaskProject is a project a task can start in.
type TaskProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Git  bool   `json:"git"`
}

// TaskStart is a bot's request for a new task.
type TaskStart struct {
	ProjectID      string `json:"projectId"`
	Harness        string `json:"harness"`
	Prompt         string `json:"prompt"`
	Title          string `json:"title"`
	PermissionMode string `json:"permissionMode"`
	NewWorktree    bool   `json:"newWorktree"`
}

// TaskRuntime is the daemon's agent runtime as bots may use it. actor is "bot:<id>": the
// runtime records it as the task's owner and as the policy actor; YOLO is honoured only when
// the user's settings let bots use it.
type TaskRuntime interface {
	Projects(ctx context.Context) ([]TaskProject, error)
	Harnesses(ctx context.Context) ([]string, error)
	Start(ctx context.Context, actor string, spec TaskStart) (TaskInfo, error)
	List(ctx context.Context, limit int) ([]TaskInfo, error)
	Get(ctx context.Context, id string, transcript int) (TaskInfo, []TaskLine, error)
	Send(ctx context.Context, actor, id, text string) (TaskInfo, error)
	Stop(ctx context.Context, actor, id string) (TaskInfo, error)
}

// Tasks offers the code-task tools to bots whose configuration allows code tasks.
type Tasks struct{ Runtime TaskRuntime }

func (Tasks) ID() string { return "code_tasks" }

func (t Tasks) Specs(bot chat.Bot, caps llm.Caps) []llm.ToolSpec {
	if !caps.Tools || !bot.Config.CodeTasks || t.Runtime == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	projects, _ := t.Runtime.Projects(ctx)
	harnesses, _ := t.Runtime.Harnesses(ctx)
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, fmt.Sprintf("%s (%s)", p.ID, p.Name))
	}
	projectHelp := "Project id. Known projects: none registered yet; ask the user to add one in Orole."
	if len(names) > 0 {
		projectHelp = "Project id, one of: " + strings.Join(names, "; ")
	}
	harnessJSON, _ := json.Marshal(harnesses)
	if len(harnesses) == 0 {
		harnessJSON = []byte(`["claude"]`)
	}
	return []llm.ToolSpec{
		{Name: toolStartTask, Description: "Start a code task: a coding agent (Claude Code, Codex, ...) that reads and edits files and runs commands in a project on this PC. You cannot do that yourself. The user may be asked to approve the start (in chat) unless they allowed you always. Give a complete, self-contained task. You hear back with the task's full result once it is finished: its turn ended and no background agents of it are still running. Until then its status is working; a report never comes early.",
			Schema: json.RawMessage(`{"type":"object","properties":{"projectId":{"type":"string","description":` + quote(projectHelp) + `},"harness":{"type":"string","enum":` + string(harnessJSON) + `},"prompt":{"type":"string","description":"The task for the agent."},"title":{"type":"string","description":"Short title (optional)."},"permissionMode":{"type":"string","enum":["manual","auto","yolo"],"description":"Omit to use the user's default mode. manual asks the user before risky actions; auto lets the agent's own safety checks decide; yolo asks nothing (only when the user allowed bots to use it)."},"newWorktree":{"type":"boolean","description":"Work on a new branch in a separate worktree (recommended for changes)."}},"required":["projectId","harness","prompt"],"additionalProperties":false}`)},
		{Name: toolListTasks, Description: "List recent code tasks (yours and the user's) with status and what each is doing.",
			Schema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":30}},"additionalProperties":false}`)},
		{Name: toolTaskStatus, Description: "Look at one code task: status, current activity, last message, changes, what it waits for, and optionally the latest transcript lines.",
			Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"transcript":{"type":"integer","minimum":0,"maximum":40,"description":"How many recent transcript lines to include (default 0)."}},"required":["id"],"additionalProperties":false}`)},
		{Name: toolMessageTask, Description: "Send a follow-up instruction to a code task you started. The user may be asked to approve it.",
			Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"},"message":{"type":"string"}},"required":["id","message"],"additionalProperties":false}`)},
		{Name: toolStopTask, Description: "Stop a code task you started.",
			Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`)},
	}
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

func (t Tasks) Call(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if t.Runtime == nil || !rc.Bot.Config.CodeTasks || rc.BotID == "" {
		return errResult("code tasks are not enabled for this bot")
	}
	actor := chat.BotPrefix + rc.BotID
	switch name {
	case toolStartTask:
		var in TaskStart
		if err := decode(args, &in); err != nil {
			return errResult("invalid arguments: %s", err)
		}
		if err := text("prompt", in.Prompt, 100_000, true); err != nil {
			return errResult("%s", err)
		}
		if err := text("title", in.Title, 120, false); err != nil {
			return errResult("%s", err)
		}
		switch in.PermissionMode {
		case "", "manual", "auto", "yolo": // "" = the user's default mode, applied by the runtime
		default:
			return errResult("permissionMode must be manual, auto or yolo")
		}
		info, err := t.Runtime.Start(ctx, actor, in)
		if err != nil {
			return errResult("the task did not start: %s", err)
		}
		res, err := taskResult("Started. You will get a report with the full result when it is finished (turn ended, no background agents running), and sooner only if it fails or needs the user. Do not call it done before that report; task_status shows status, turns and subagents meanwhile.", info, nil)
		res.Parts = []chat.MsgPart{{Type: chat.PartTask, ID: info.ID, Title: oneLine(info.Title, 120), Name: info.Harness}}
		return res, err
	case toolListTasks:
		var in struct {
			Limit int `json:"limit"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("invalid arguments: %s", err)
		}
		if in.Limit <= 0 || in.Limit > 30 {
			in.Limit = 10
		}
		list, err := t.Runtime.List(ctx, in.Limit)
		if err != nil {
			return errResult("cannot list tasks: %s", err)
		}
		b, _ := json.Marshal(list)
		return chat.ToolResult{Text: string(b), Untrusted: true}, nil
	case toolTaskStatus:
		var in struct {
			ID         string `json:"id"`
			Transcript int    `json:"transcript"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("invalid arguments: %s", err)
		}
		info, lines, err := t.Runtime.Get(ctx, in.ID, min(max(in.Transcript, 0), 40))
		if err != nil {
			return errResult("%s", err)
		}
		return taskResult("", info, lines)
	case toolMessageTask, toolStopTask:
		var in struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		if err := decode(args, &in); err != nil {
			return errResult("invalid arguments: %s", err)
		}
		info, _, err := t.Runtime.Get(ctx, in.ID, 0)
		if err != nil {
			return errResult("%s", err)
		}
		if info.Owner != actor {
			return errResult("only tasks you started can be steered; ask the user to handle %q in Orole", info.Title)
		}
		if name == toolStopTask {
			info, err = t.Runtime.Stop(ctx, actor, in.ID)
		} else {
			if err := text("message", in.Message, 100_000, true); err != nil {
				return errResult("%s", err)
			}
			info, err = t.Runtime.Send(ctx, actor, in.ID, in.Message)
		}
		if err != nil {
			return errResult("%s", err)
		}
		return taskResult("Done.", info, nil)
	}
	return errResult("unknown tool")
}

// taskResult returns a task's state to the model as fenced data.
func taskResult(note string, info TaskInfo, lines []TaskLine) (chat.ToolResult, error) {
	b, _ := json.Marshal(struct {
		Note       string     `json:"note,omitempty"`
		Task       TaskInfo   `json:"task"`
		Transcript []TaskLine `json:"transcript,omitempty"`
	}{note, info, lines})
	return chat.ToolResult{Text: string(b), Untrusted: true}, nil
}

// TasksPrompt tells a bot with code tasks what it can do and lists the tasks it started that
// have not ended, so it can answer "how is it going" without a tool call.
func TasksPrompt(rt TaskRuntime, bot chat.Bot) string {
	if rt == nil || !bot.Config.CodeTasks {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Code tasks\nYou can start code tasks (start_task): coding agents that edit files and run commands on the user's PC. " +
		"Use them for real changes or deep code research in a project, not for questions you can answer yourself. The user may approve each start in chat. " +
		"A task is finished only when its status is done with no background agents running; a working task that printed a progress note is not done. " +
		"You get a report with the task's complete result when it finishes, and earlier only if it fails or needs the user; only the user can answer a task's questions. " +
		"When a report arrives, relay its findings to the user in full (its structure, numbers, file paths, findings), not a one-line summary.")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	list, err := rt.List(ctx, 50)
	if err != nil {
		return b.String()
	}
	var running strings.Builder
	for _, t := range list {
		if t.Owner != chat.BotPrefix+bot.ID || !t.Live {
			continue
		}
		fmt.Fprintf(&running, "- %s %q: %s", t.ID, oneLine(t.Title, 80), t.Status)
		if t.Activity != "" {
			fmt.Fprintf(&running, " (%s)", oneLine(t.Activity, 80))
		}
		running.WriteString("\n")
	}
	if running.Len() > 0 {
		// Titles and activity lines come from the agents: data, not instructions.
		b.WriteString("\n\nYour running tasks:\n" + chat.Fence("running code tasks", strings.TrimRight(running.String(), "\n")))
	}
	return b.String()
}

var _ chat.Tool = Tasks{}

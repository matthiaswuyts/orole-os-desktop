package tools

import (
	"cmp"
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
	Model          string `json:"model,omitempty"`     // "" = the harness's default
	ModelName      string `json:"modelName,omitempty"` // the harness's name for Model, which can be an alias ("opus"): the model it runs
	Reasoning      string `json:"reasoning,omitempty"` // "" = the harness's default
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

// TaskStart is a bot's request for a new task. Harness, Model and Reasoning count only where the
// user's settings leave them to the bot (TaskChoices); the permission mode is never the bot's.
type TaskStart struct {
	ProjectID   string `json:"projectId"`
	Harness     string `json:"harness"`
	Model       string `json:"model"`
	Reasoning   string `json:"reasoning"`
	Prompt      string `json:"prompt"`
	Title       string `json:"title"`
	NewWorktree bool   `json:"newWorktree"`
	// CallID is the phone call the bot is on when it starts the task (chat.RunContext.CallID): the task's reports go
	// to that call's thread. The tool sets it from the run; the model cannot (it is not part of the arguments).
	CallID string `json:"-"`
}

// TaskChoices is how the user's settings shape the code tasks one bot starts. Harness, Model and
// Reasoning are fixed values or chat.TaskChoiceAuto: the bot chooses, and may leave model and
// reasoning empty for the harness's default. PermissionMode is always the user's.
type TaskChoices struct {
	Harness        string
	Model          string
	Reasoning      string
	PermissionMode string
	// Harnesses are the installed harnesses the bot may use (only the fixed one when the harness is
	// fixed), with the models and reasoning levels each advertises.
	Harnesses []TaskHarness
}

// TaskHarness is one installed coding harness as a bot may choose it. Empty Models or Reasoning
// means the harness lists none; CustomModel means it also takes model ids it does not list.
type TaskHarness struct {
	ID     string
	Models []string
	// ModelNames are the harness's names for listed models where they say more than the id; an
	// alias's name says which model it runs now (Claude: "Opus (claude-opus-5-5)").
	ModelNames  map[string]string
	Reasoning   []string
	CustomModel bool
}

// TaskRuntime is the daemon's agent runtime as bots may use it. actor is "bot:<id>": the
// runtime records it as the task's owner and as the policy actor. Start resolves the harness,
// model, reasoning and permission mode from the user's settings and bot's own configuration,
// ignoring what spec names for a fixed field; YOLO is honoured only when the user let bots use it.
type TaskRuntime interface {
	Projects(ctx context.Context) ([]TaskProject, error)
	Choices(ctx context.Context, bot chat.Bot) (TaskChoices, error)
	Start(ctx context.Context, actor string, bot chat.Bot, spec TaskStart) (TaskInfo, error)
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
	choices, err := t.Runtime.Choices(ctx, bot)
	if err != nil { // offer every field; Start still resolves and enforces the settings
		choices = TaskChoices{Harness: chat.TaskChoiceAuto, Model: chat.TaskChoiceAuto, Reasoning: chat.TaskChoiceAuto}
	}
	names := make([]string, 0, len(projects))
	for _, p := range projects {
		names = append(names, fmt.Sprintf("%s (%s)", p.ID, p.Name))
	}
	projectHelp := "Project id. Known projects: none registered yet; ask the user to add one in Orole."
	if len(names) > 0 {
		projectHelp = "Project id, one of: " + strings.Join(names, "; ")
	}
	return []llm.ToolSpec{
		{Name: toolStartTask, Description: "Start a code task: a coding agent (Claude Code, Codex, ...) that reads and edits files and runs commands in a project on this PC. You cannot do that yourself. The user may be asked to approve the start (in chat) unless they allowed you always. " + settingsHelp(choices) + " Give a complete, self-contained task. You hear back with the task's full result once it is finished: its turn ended and no background agents of it are still running. Until then its status is working; a report never comes early.",
			Schema: startTaskSchema(projectHelp, choices)},
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

// modeHelp says what each permission mode means for the tasks a bot starts.
var modeHelp = map[string]string{
	"manual": "risky actions wait for the user's approval",
	"auto":   "the agent's own safety checks decide",
	"yolo":   "nothing asks for approval",
}

// settingsHelp names what the user's settings fix for the bot's tasks; the permission mode always.
func settingsHelp(c TaskChoices) string {
	var fixed []string
	for _, f := range []struct{ name, value string }{{"harness", c.Harness}, {"model", c.Model}, {"reasoning effort", c.Reasoning}} {
		if f.value != chat.TaskChoiceAuto && f.value != "" {
			fixed = append(fixed, f.name+" "+f.value)
		}
	}
	mode := "the permission mode"
	if c.PermissionMode != "" {
		mode = "permission mode " + c.PermissionMode
		if help := modeHelp[c.PermissionMode]; help != "" {
			mode += " (" + help + ")"
		}
	}
	return "The user's settings decide " + strings.Join(append(fixed, mode), ", ") + "; you cannot change them."
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

// listedMax bounds the models listed per harness in a tool description.
const listedMax = 8

// startTaskSchema offers only the fields the user's settings leave to the bot: a fixed harness,
// model or reasoning is absent (and ignored if sent anyway).
func startTaskSchema(projectHelp string, c TaskChoices) json.RawMessage {
	type prop map[string]any
	props := map[string]prop{
		"projectId":   {"type": "string", "description": projectHelp},
		"prompt":      {"type": "string", "description": "The task for the agent."},
		"title":       {"type": "string", "description": "Short title (optional)."},
		"newWorktree": {"type": "boolean", "description": "Work on a new branch in a separate worktree (recommended for changes)."},
	}
	required := []string{"projectId", "prompt"}
	if c.Harness == chat.TaskChoiceAuto {
		ids := make([]string, 0, len(c.Harnesses))
		for _, h := range c.Harnesses {
			ids = append(ids, h.ID)
		}
		if len(ids) > 0 {
			props["harness"] = prop{"type": "string", "enum": ids, "description": "The coding agent to run."}
		} else {
			props["harness"] = prop{"type": "string", "description": "No coding agent is installed; ask the user to install Claude Code or Codex."}
		}
		required = append(required, "harness")
	}
	if c.Model == chat.TaskChoiceAuto {
		props["model"] = prop{"type": "string", "description": "Model id (optional; omit for the harness's default). Send only the id: where a listed id is an alias, the text after \" = \" is the model it runs now." +
			perHarness(c.Harnesses, func(h TaskHarness) ([]string, bool) {
				labels := make([]string, 0, len(h.Models))
				for _, id := range h.Models {
					if name := h.ModelNames[id]; name != "" {
						id += " = " + name
					}
					labels = append(labels, id)
				}
				return labels, h.CustomModel
			})}
	}
	if c.Reasoning == chat.TaskChoiceAuto {
		props["reasoning"] = prop{"type": "string", "description": "Reasoning effort (optional; omit for the harness's default)." + perHarness(c.Harnesses, func(h TaskHarness) ([]string, bool) { return h.Reasoning, false })}
	}
	b, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false})
	return b
}

// perHarness lists, per harness, the values it advertises (at most listedMax each).
func perHarness(harnesses []TaskHarness, values func(TaskHarness) ([]string, bool)) string {
	var parts []string
	for _, h := range harnesses {
		list, other := values(h)
		if len(list) == 0 {
			continue
		}
		s := strings.Join(list[:min(len(list), listedMax)], ", ")
		if n := len(list) - listedMax; n > 0 {
			s += fmt.Sprintf(" (+%d more)", n)
		}
		if other {
			s += ", or another id it accepts"
		}
		parts = append(parts, h.ID+": "+s)
	}
	if len(parts) == 0 {
		return ""
	}
	return " Valid values by harness: " + strings.Join(parts, "; ") + "."
}

func (t Tasks) Call(ctx context.Context, rc chat.RunContext, name string, args json.RawMessage) (chat.ToolResult, error) {
	if t.Runtime == nil || !rc.Bot.Config.CodeTasks || rc.BotID == "" {
		return errResult("code tasks are not enabled for this bot")
	}
	actor := chat.BotPrefix + rc.BotID
	switch name {
	case toolStartTask:
		var in TaskStart
		if err := decode(args, &in); err != nil {
			if strings.Contains(err.Error(), `"permissionMode"`) {
				return errResult("the permission mode of your tasks is set by the user in Orole's settings; omit permissionMode")
			}
			return errResult("invalid arguments: %s", err)
		}
		if err := text("prompt", in.Prompt, 100_000, true); err != nil {
			return errResult("%s", err)
		}
		if err := text("title", in.Title, 120, false); err != nil {
			return errResult("%s", err)
		}
		for _, f := range []struct{ name, value string }{{"harness", in.Harness}, {"model", in.Model}, {"reasoning", in.Reasoning}} {
			if err := text(f.name, f.value, 128, false); err != nil {
				return errResult("%s", err)
			}
		}
		in.CallID = rc.CallID
		info, err := t.Runtime.Start(ctx, actor, rc.Bot, in)
		if err != nil {
			return errResult("the task did not start: %s", err)
		}
		model, effort := cmp.Or(info.Model, "the harness's default"), cmp.Or(info.Reasoning, "the harness's default")
		if info.ModelName != "" {
			model += " (" + info.ModelName + ")"
		}
		note := fmt.Sprintf("Started with harness %s, model %s, reasoning effort %s and permission mode %s (the user's setting). ", info.Harness, model, effort, cmp.Or(info.PermissionMode, "manual")) +
			"You will get a report with the full result when it is finished (turn ended, no background agents running), and sooner only if it fails or needs the user. Do not call it done before that report; task_status shows status, turns and subagents meanwhile."
		res, err := taskResult(note, info, nil)
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

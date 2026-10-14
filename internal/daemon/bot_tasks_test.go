package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/tools"
	"github.com/rayorole/orole-os-desktop/internal/events"
)

// A bot's own task settings beat the global ones field by field; what neither sets is the bot's
// choice, except the permission mode, which falls back to the user's default mode and then manual.
func TestBotTaskDefaultsResolveBotThenGlobalThenFallback(t *testing.T) {
	const auto = chat.TaskChoiceAuto
	cases := []struct {
		name   string
		global agentSettings
		own    chat.BotTaskDefaults
		want   chat.BotTaskDefaults
	}{
		{"nothing set", agentSettings{}, chat.BotTaskDefaults{}, chat.BotTaskDefaults{Harness: auto, Model: auto, Reasoning: auto, PermissionMode: "manual"}},
		{"user's default mode", agentSettings{DefaultPermissionMode: "auto"}, chat.BotTaskDefaults{}, chat.BotTaskDefaults{Harness: auto, Model: auto, Reasoning: auto, PermissionMode: "auto"}},
		{"global bot-task values", agentSettings{DefaultPermissionMode: "auto", BotTasks: chat.BotTaskDefaults{Harness: "codex", Model: "gpt-5", Reasoning: "high", PermissionMode: "yolo"}},
			chat.BotTaskDefaults{}, chat.BotTaskDefaults{Harness: "codex", Model: "gpt-5", Reasoning: "high", PermissionMode: "yolo"}},
		{"per-bot override", agentSettings{DefaultPermissionMode: "yolo", BotTasks: chat.BotTaskDefaults{Harness: "codex", Model: "gpt-5", PermissionMode: "auto"}},
			chat.BotTaskDefaults{Harness: "claude", Model: auto, PermissionMode: "manual"}, chat.BotTaskDefaults{Harness: "claude", Model: auto, Reasoning: auto, PermissionMode: "manual"}},
		{"unknown mode fails closed", agentSettings{DefaultPermissionMode: "yolo"}, chat.BotTaskDefaults{PermissionMode: "plan"}, chat.BotTaskDefaults{Harness: auto, Model: auto, Reasoning: auto, PermissionMode: "manual"}},
	}
	for _, c := range cases {
		if got := resolveBotTaskDefaults(c.global, c.own); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestPlanBotTaskEnforcesTheSettings(t *testing.T) {
	const auto = chat.TaskChoiceAuto
	installed := []agents.HarnessOption{
		{ID: "claude", Reasoning: []string{"low", "high", "max"}, CustomModel: true, Models: []agents.ModelOption{{ID: "opus", Reasoning: []string{"low", "high"}}, {ID: "haiku"}}},
		{ID: "codex", Reasoning: []string{"low", "medium"}, Models: []agents.ModelOption{{ID: "gpt-5"}}},
	}
	allAuto := chat.BotTaskDefaults{Harness: auto, Model: auto, Reasoning: auto, PermissionMode: "auto"}
	cases := []struct {
		name     string
		settings chat.BotTaskDefaults
		spec     tools.TaskStart
		want     chat.BotTaskDefaults
		err      string
	}{
		{"fixed values override the bot", chat.BotTaskDefaults{Harness: "codex", Model: "gpt-5", Reasoning: "medium", PermissionMode: "manual"},
			tools.TaskStart{Harness: "claude", Model: "opus", Reasoning: "max"}, chat.BotTaskDefaults{Harness: "codex", Model: "gpt-5", Reasoning: "medium", PermissionMode: "manual"}, ""},
		{"auto passes the bot's choice", allAuto, tools.TaskStart{Harness: "claude", Model: "opus", Reasoning: "high"},
			chat.BotTaskDefaults{Harness: "claude", Model: "opus", Reasoning: "high", PermissionMode: "auto"}, ""},
		{"auto model and effort may stay the harness default", allAuto, tools.TaskStart{Harness: "codex"}, chat.BotTaskDefaults{Harness: "codex", PermissionMode: "auto"}, ""},
		{"a custom model where the harness takes one", allAuto, tools.TaskStart{Harness: "claude", Model: "claude-next"}, chat.BotTaskDefaults{Harness: "claude", Model: "claude-next", PermissionMode: "auto"}, ""},
		{"auto harness needs the bot's choice", allAuto, tools.TaskStart{}, chat.BotTaskDefaults{}, "choose one of: claude, codex"},
		{"bot picks a missing harness", allAuto, tools.TaskStart{Harness: "gemini"}, chat.BotTaskDefaults{}, `harness "gemini" is not installed`},
		{"fixed harness not installed", chat.BotTaskDefaults{Harness: "omp", Model: auto, Reasoning: auto}, tools.TaskStart{Harness: "claude"}, chat.BotTaskDefaults{}, `"omp" set for your tasks in Orole's settings is not installed`},
		{"model not offered", allAuto, tools.TaskStart{Harness: "codex", Model: "opus"}, chat.BotTaskDefaults{}, "use one of: gpt-5"},
		{"effort the model lacks", allAuto, tools.TaskStart{Harness: "claude", Model: "opus", Reasoning: "max"}, chat.BotTaskDefaults{}, "use one of: low, high"},
		{"effort the harness lacks", allAuto, tools.TaskStart{Harness: "codex", Reasoning: "max"}, chat.BotTaskDefaults{}, "use one of: low, medium"},
	}
	for _, c := range cases {
		got, err := planBotTask(c.settings, installed, c.spec)
		switch {
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%s: err = %v, want %q", c.name, err, c.err)
		case c.err == "" && (err != nil || got != c.want):
			t.Errorf("%s: %+v %v, want %+v", c.name, got, err, c.want)
		}
	}
}

func TestTaskChoicesListOnlyTheFixedHarness(t *testing.T) {
	installed := []agents.HarnessOption{{ID: "claude", Models: []agents.ModelOption{{ID: "opus"}}, Reasoning: []string{"high"}}, {ID: "codex"}}
	c := taskChoices(chat.BotTaskDefaults{Harness: "claude", Model: chat.TaskChoiceAuto, Reasoning: "high", PermissionMode: "manual"}, installed)
	if c.Harness != "claude" || c.Model != chat.TaskChoiceAuto || c.Reasoning != "high" || c.PermissionMode != "manual" ||
		len(c.Harnesses) != 1 || c.Harnesses[0].ID != "claude" || len(c.Harnesses[0].Models) != 1 || c.Harnesses[0].Models[0] != "opus" {
		t.Fatalf("choices = %+v", c)
	}
	if c := taskChoices(chat.BotTaskDefaults{Harness: chat.TaskChoiceAuto}, installed); len(c.Harnesses) != 2 {
		t.Fatalf("auto harness lists every installed one: %+v", c)
	}
}

// A YOLO mode from the settings is named explicitly, so a bot the user did not allow YOLO gets a
// refusal it can relay instead of a task silently started in Manual.
func TestBotTaskInYoloWithoutBotYoloIsRefused(t *testing.T) {
	d, err := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p, err := d.Projects.Create(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rt := newBotTasks(d)
	rt.options.list, rt.options.at = []agents.HarnessOption{{ID: "claude", CustomModel: true}}, time.Now()
	bot := chat.Bot{ID: "b1", Config: chat.BotConfig{CodeTasks: true}}
	for _, setting := range []map[string]any{
		{"botAutoApprove": true, "defaultPermissionMode": "yolo"},
		{"botAutoApprove": true, "botTasks": map[string]any{"permissionMode": "yolo"}},
	} {
		raw, _ := json.Marshal(setting)
		if _, err := d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('agents',?,0) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw)); err != nil {
			t.Fatal(err)
		}
		_, err := rt.Start(ctx, chat.BotPrefix+bot.ID, bot, tools.TaskStart{ProjectID: p.ID, Harness: "claude", Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "Bots may use Yolo") {
			t.Fatalf("%s: err = %v, want the bot YOLO refusal", raw, err)
		}
	}
	if list, err := d.Agents.ListPage(10, 0); err != nil || len(list) != 0 {
		t.Fatalf("a refused start left sessions: %d %v", len(list), err)
	}
}

// A code task a bot started on a phone call reports into that call's thread: the report never reaches the bot's
// chat, and the follow-up hook hears about it as a task with the topic's status.
func TestTaskStartedOnACallReportsIntoTheCallThread(t *testing.T) {
	d, err := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := d.Chat.SaveBot(ctx, chat.BotInput{Name: "Coder"})
	if err != nil {
		t.Fatal(err)
	}
	follow := make(chan chat.FollowUp, 4)
	d.Chat.SetFollowUpHook(func(f chat.FollowUp) bool { follow <- f; return false })

	d.reportToOwner(events.TopicAgentFailed, agents.Session{ID: "t1", Title: "Fix login", Kind: "claude", Status: agents.StatusFailed,
		Error: "boom", Owner: chat.BotPrefix + b.ID, CallID: "call-1"})
	var f chat.FollowUp
	select {
	case f = <-follow:
	case <-ctx.Done():
		t.Fatal("the follow-up hook was not told")
	}
	if f.BotID != b.ID || f.CallID != "call-1" || f.Kind != chat.FollowUpTask || f.From != "Fix login" || f.Status != "failed" || !strings.Contains(f.Text, "boom") {
		t.Fatalf("follow-up: %+v", f)
	}
	var thread []chat.Message
	for {
		if thread, err = d.Chat.CallMessages(ctx, "call-1"); err != nil {
			t.Fatal(err)
		}
		if len(thread) > 0 || ctx.Err() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(thread) == 0 || thread[0].Author != chat.TaskPrefix+"t1" || thread[0].CallID != "call-1" {
		t.Fatalf("call thread: %+v", thread)
	}
	page, err := d.Chat.Messages(ctx, chat.MessagesRequest{BotID: b.ID, Limit: 50})
	if err != nil || len(page.Messages) != 0 {
		t.Fatalf("the call's task report reached the chat: %+v %v", page.Messages, err)
	}
}

func TestTaskReportStatusNamesTheTopic(t *testing.T) {
	for topic, want := range map[string]string{events.TopicAgentFinished: "finished", events.TopicAgentWaiting: "waiting", events.TopicAgentFailed: "failed"} {
		if got := taskReportStatus(topic); got != want {
			t.Errorf("%s: %q, want %q", topic, got, want)
		}
	}
}

// fakeClaudeOnPath builds the agents tests' scripted claude stand-in as "claude" in a temp dir and puts that dir first
// on PATH, so the daemon's Claude adapter runs it instead of a real CLI.
func fakeClaudeOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", filepath.Join(dir, name), "../agents/testdata/fakeagent").CombinedOutput()
	if err != nil {
		t.Fatalf("building the fake claude: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A task a bot starts from a call thread reports there; steering it moves its reports to where it was last steered
// from: another call's thread, or (steered from chat) back to the chat.
func TestBotTaskSteeringMovesItsCallThread(t *testing.T) {
	fakeClaudeOnPath(t)
	d, err := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Bots start and steer tasks without an approval prompt.
	if _, err := d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('agents','{"botAutoApprove":true}',0) ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		t.Fatal(err)
	}
	p, err := d.Projects.Create(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rt := newBotTasks(d)
	rt.options.list, rt.options.at = []agents.HarnessOption{{ID: "claude", CustomModel: true}}, time.Now()
	bot := chat.Bot{ID: "b1", Config: chat.BotConfig{CodeTasks: true}}
	actor := chat.BotPrefix + bot.ID

	info, err := rt.Start(ctx, actor, bot, tools.TaskStart{ProjectID: p.ID, Harness: "claude", Prompt: "hello", CallID: "call-1"})
	if err != nil {
		t.Fatal(err)
	}
	settled := func(s agents.Session) bool { return s.Status != agents.StatusWorking && s.Status != agents.StatusIdle }
	callOf := func() string {
		t.Helper()
		s, err := d.Agents.WaitFor(ctx, info.ID, settled)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := d.Agents.Store().GetSession(info.ID)
		if err != nil || stored.CallID != s.CallID {
			t.Fatalf("stored call %q, live %q: %v", stored.CallID, s.CallID, err)
		}
		return s.CallID
	}
	if got := callOf(); got != "call-1" {
		t.Fatalf("started on call-1, call %q", got)
	}
	for _, step := range []struct{ thread, text string }{{"call-2", "more from the second call"}, {"", "more from chat"}} {
		if _, err := rt.Send(ctx, actor, info.ID, step.text, step.thread); err != nil {
			t.Fatal(err)
		}
		if got := callOf(); got != step.thread {
			t.Fatalf("steered from %q, call %q", step.thread, got)
		}
	}
}

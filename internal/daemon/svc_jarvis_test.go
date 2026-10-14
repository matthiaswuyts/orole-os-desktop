package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/jarvis"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

// Every daemon in this test binary keeps Jarvis secrets in its own memory store, never in the user's
// Credential Manager.
func init() {
	jarvisCredentialStore = func() credentials.Store { return credentials.NewMemory() }
}

func jarvisDaemon(t *testing.T) *Daemon {
	t.Helper()
	restore := UseMemoryLLMCredentials()
	t.Cleanup(restore)
	dir := t.TempDir()
	d, err := New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	t.Cleanup(func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return d
}

func setAgents(t *testing.T, d *Daemon, raw string) {
	t.Helper()
	if _, err := d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('agents',?,0)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, raw); err != nil {
		t.Fatal(err)
	}
}

func jarvisBotInput(id string) chat.BotInput {
	return chat.BotInput{ID: id, Name: "Atlas", Config: chat.BotConfig{CodeTasks: true, ToolAccess: chat.ToolAccessAll,
		Tasks: chat.BotTaskDefaults{PermissionMode: "yolo"}, Jarvis: chat.BotJarvis{Enabled: true}}}
}

func TestJarvisSaveCheckRequiresFullPermissions(t *testing.T) {
	d := jarvisDaemon(t)
	ctx := context.Background()
	b, err := d.Chat.SaveBot(ctx, chat.BotInput{Name: "Atlas"})
	if err != nil {
		t.Fatal(err)
	}
	in := jarvisBotInput(b.ID)
	_, err = d.Chat.SaveBot(ctx, in)
	var ie *ipc.Error
	if !errors.As(err, &ie) || ie.Code != ipc.CodeBadRequest || !strings.Contains(err.Error(), "YOLO") {
		t.Fatalf("saved without agents.botYolo: %v", err)
	}
	setAgents(t, d, `{"botYolo":true}`)
	if _, err := d.Chat.SaveBot(ctx, in); err != nil {
		t.Fatalf("full permissions rejected: %v", err)
	}
	// Already on: lowering a permission saves the bot with Jarvis switched off, and tells the owner.
	in.Config.ToolAccess = chat.ToolAccessWrite
	got, err := d.Chat.SaveBot(ctx, in)
	if err != nil || got.Config.Jarvis.Enabled || got.Config.ToolAccess != chat.ToolAccessWrite {
		t.Fatalf("lowered tool access: %+v %v", got.Config, err)
	}
	waitNotice(t, d, "Jarvis turned off for Atlas")
	// Off again: turning it on with too little access is still refused.
	in.Config.Jarvis.Enabled = true
	if _, err := d.Chat.SaveBot(ctx, in); err == nil || !strings.Contains(err.Error(), "tool access") {
		t.Fatalf("enabled Jarvis without full tool access: %v", err)
	}
}

func dialDaemon(t *testing.T, d *Daemon) *ipc.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 100; i++ {
		c := control.NewClient(d.DataDir, "jarvis-test")
		if c.Connect(ctx) == nil {
			t.Cleanup(func() { c.Close() })
			return c
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("cannot reach the daemon")
	return nil
}

func waitNotice(t *testing.T, d *Daemon, title string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ns, err := notifications.New(d.DB.DB).List(context.Background(), notifications.ListOptions{Limit: 50})
		if err == nil {
			for _, n := range ns {
				if strings.Contains(n.Title, title) {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no notice %q", title)
}

// Saving the global agents setting switches Jarvis off for bots that no longer pass the gate.
func TestAgentsSettingSweepsJarvisOff(t *testing.T) {
	d := jarvisDaemon(t)
	ctx := context.Background()
	setAgents(t, d, `{"botYolo":true}`)
	b, err := d.Chat.SaveBot(ctx, jarvisBotInput(""))
	if err != nil {
		t.Fatal(err)
	}
	c := dialDaemon(t, d)
	if err := c.Call(ctx, "settings.set", map[string]any{"key": "agents", "value": map[string]any{"botYolo": false}}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := d.Chat.Bot(ctx, b.ID)
	if got.Config.Jarvis.Enabled {
		t.Fatal("Jarvis still on after the agents setting dropped bot YOLO")
	}
	waitNotice(t, d, "Jarvis turned off for Atlas")
}

func TestJarvisDisableClearsFlag(t *testing.T) {
	d := jarvisDaemon(t)
	ctx := context.Background()
	setAgents(t, d, `{"botYolo":true}`)
	b, err := d.Chat.SaveBot(ctx, jarvisBotInput(""))
	if err != nil {
		t.Fatal(err)
	}
	h := jarvisHost{d: d}
	if err := h.Gate(ctx, b); err != nil {
		t.Fatalf("gate refused a fully permitted bot: %v", err)
	}
	h.DisableJarvis(ctx, b.ID, "tool access lowered")
	got, _ := d.Chat.Bot(ctx, b.ID)
	if got.Config.Jarvis.Enabled || got.Config.ToolAccess != chat.ToolAccessAll {
		t.Fatalf("config after disable: %+v", got.Config)
	}
	var ge *jarvis.GateError
	if err := h.Gate(ctx, got); !errors.As(err, &ge) {
		t.Fatalf("gate on a disabled bot: %v", err)
	}
}

func TestJarvisGateFollowsGlobalSettings(t *testing.T) {
	d := jarvisDaemon(t)
	ctx := context.Background()
	setAgents(t, d, `{"botYolo":true}`)
	b, err := d.Chat.SaveBot(ctx, jarvisBotInput(""))
	if err != nil {
		t.Fatal(err)
	}
	setAgents(t, d, `{"botYolo":true,"yoloDisabled":true}`)
	if err := (jarvisHost{d: d}).Gate(ctx, b); err == nil {
		t.Fatal("gate passed with YOLO disabled")
	}
}

func TestTaskEventsForwardFinishedAndFailedToJarvis(t *testing.T) {
	var mu sync.Mutex
	var got []string
	te := newTaskEvents(nil, nil)
	te.jarvis = func(topic string, s agents.Session) {
		mu.Lock()
		got = append(got, topic+":"+s.ID)
		mu.Unlock()
	}
	s := agents.Session{ID: "t1", Title: "Deploy", Status: agents.StatusWorking}
	te.created(s)
	s.Status = agents.StatusNeedsYou
	te.observe("agents.updated", s)
	s.Status = agents.StatusFailed
	te.observe("agents.updated", s)
	s2 := agents.Session{ID: "t2", Status: agents.StatusDone}
	te.finished(s2)
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, ",") != events.TopicAgentFailed+":t1,"+events.TopicAgentFinished+":t2" {
		t.Fatalf("forwarded %v", got)
	}
}

func TestJarvisTaskEvent(t *testing.T) {
	ev, ok := jarvisTaskEvent(events.TopicAgentFailed, agents.Session{ID: "t1", Title: "Deploy", Owner: chat.BotPrefix + "b1", Error: "exit 1"})
	if !ok || ev.Trigger != jarvis.TaskFailed || ev.TargetID != "t1" || ev.OwnerBotID != "b1" || !strings.Contains(ev.Title, "Deploy") {
		t.Fatalf("event %+v", ev)
	}
	if _, ok := jarvisTaskEvent(events.TopicAgentWaiting, agents.Session{ID: "t1"}); ok {
		t.Fatal("waiting forwarded")
	}
}

func TestTerminalRunsForwardOnce(t *testing.T) {
	r := newTerminalRuns()
	now := time.Unix(1000, 0)
	if !r.first("run1", now) || r.first("run1", now) {
		t.Fatal("not deduplicated")
	}
	if !r.first("run1", now.Add(2*time.Hour)) {
		t.Fatal("old entries never pruned")
	}
}

// A notification rule fires on a stored notification, and Jarvis's own notice about it does not fire it again.
func TestNotificationRuleDoesNotLoopOnOwnNotices(t *testing.T) {
	d := jarvisDaemon(t)
	ctx := context.Background()
	setAgents(t, d, `{"botYolo":true}`)
	b, err := d.Chat.SaveBot(ctx, jarvisBotInput(""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Jarvis.AddRule(ctx, jarvis.JarvisRule{BotID: b.ID, Trigger: jarvis.NotificationTrigger, Target: "failed", Repeat: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Notify(notifications.Input{Source: "github.ci", Kind: notifications.Failed, Title: "CI failed", Level: notifications.Error}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if countSource(t, d, "jarvis") >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if n := countSource(t, d, "jarvis"); n != 1 {
		t.Fatalf("jarvis notices=%d, want exactly 1 (not set up, no loop)", n)
	}
}

func countSource(t *testing.T, d *Daemon, source string) int {
	t.Helper()
	list, err := notifications.New(d.DB.DB).List(context.Background(), notifications.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range list {
		if x.Source == source {
			n++
		}
	}
	return n
}

func TestJarvisOverIPC(t *testing.T) {
	d := jarvisDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var c *ipc.Client
	for i := 0; i < 100; i++ {
		c = control.NewClient(d.DataDir, "jarvis-test")
		if c.Connect(ctx) == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer c.Close()
	var st jarvis.StatusView
	if err := c.Call(ctx, "jarvis.status", struct{}{}, &st); err != nil || st.HasRelaySecrets {
		t.Fatalf("status %+v err=%v", st, err)
	}
	var sec jarvis.RelaySecrets
	if err := c.Call(ctx, "jarvis.relaySecrets.generate", struct{}{}, &sec); err != nil || sec.RelayToken == "" || sec.LLMSecret == "" {
		t.Fatalf("secrets err=%v", err)
	}
	if err := c.Call(ctx, "jarvis.status", struct{}{}, &st); err != nil || !st.HasRelaySecrets || st.HasWebhookSecret {
		t.Fatalf("status %+v err=%v", st, err)
	}
	var res jarvis.ConfigureResult
	err := c.Call(ctx, "jarvis.configure", jarvis.PhoneSettings{RelayURL: "http://relay.example"}, &res)
	var ie *ipc.Error
	if !errors.As(err, &ie) || ie.Code != ipc.CodeBadRequest {
		t.Fatalf("plain http relay: %v", err)
	}
	if err := c.Call(ctx, "jarvis.configure", jarvis.PhoneSettings{QuietStart: "22:00", QuietEnd: "07:00"}, &res); err != nil || res.Status.Settings.QuietStart != "22:00" {
		t.Fatalf("configure res=%+v err=%v", res, err)
	}
	if got, _ := d.loadJarvisSettings(); got.QuietStart != "22:00" {
		t.Fatalf("not persisted: %+v", got)
	}
	for _, key := range []string{jarvis.SettingsKey, jarvisStateKey} {
		err = c.Call(ctx, "settings.set", map[string]any{"key": key, "value": map[string]any{"ownerVerified": true}}, nil)
		if !errors.As(err, &ie) || ie.Code != ipc.CodeBadRequest {
			t.Fatalf("settings.set wrote %s: %v", key, err)
		}
	}
	// The ticker runs once right after Start and persists its time.
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := d.loadJarvisState()
		if err == nil && st.LastTickMs > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("state %+v err=%v", st, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var rules []jarvis.JarvisRule
	if err := c.Call(ctx, "jarvis.rules.list", map[string]string{"botId": ""}, &rules); err != nil || len(rules) != 0 {
		t.Fatalf("rules %v err=%v", rules, err)
	}
}

func TestJarvisCallTranscriptOverIPC(t *testing.T) {
	d := jarvisDaemon(t)
	c := dialDaemon(t, d)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := d.Chat.SaveBot(ctx, chat.BotInput{Name: "Atlas"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`INSERT INTO jarvis_calls(id,bot_id,direction,status,conversation_id,started_at,created_at)
		VALUES('call1',?,'outbound','completed','conv-secret',1000,1000)`, b.ID); err != nil {
		t.Fatal(err)
	}
	for i, m := range []struct{ role, text, call string }{
		{"user", "typed", ""}, {"user", "hello there", "call1"}, {"assistant", "hi, all good", "call1"}, {"user", "other call", "call2"},
	} {
		if _, err := d.DB.Exec(`INSERT INTO messages(id,bot_id,seq,role,author,parts,status,created_at,text,call_id)
			VALUES(?,?,?,?,?,?,'complete',?,?,?)`, "m"+strconv.Itoa(i), b.ID, i+1, m.role, "user",
			`[{"type":"text","text":"`+m.text+`"}]`, 2000+i, m.text, m.call); err != nil {
			t.Fatal(err)
		}
	}
	var raw json.RawMessage
	if err := c.Call(ctx, "jarvis.calls.transcript", map[string]string{"callId": "call1"}, &raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "conv-secret") {
		t.Fatal("transcript leaks the provider conversation id")
	}
	var tr jarvis.CallTranscript
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Call.ID != "call1" || len(tr.Messages) != 2 || tr.Messages[0].Text() != "hello there" || tr.Messages[1].Text() != "hi, all good" || tr.Messages[1].CallID != "call1" {
		t.Fatalf("transcript %+v", tr)
	}
	var page chat.MessagePage
	if err := c.Call(ctx, "chat.messages.list", chat.MessagesRequest{BotID: b.ID}, &page); err != nil || len(page.Messages) != 1 {
		t.Fatalf("chat page %+v err=%v", page, err)
	}
	var ie *ipc.Error
	if err := c.Call(ctx, "jarvis.calls.transcript", map[string]string{"callId": "nope"}, &raw); !errors.As(err, &ie) || ie.Code != ipc.CodeNotFound {
		t.Fatalf("missing call: %v", err)
	}
}

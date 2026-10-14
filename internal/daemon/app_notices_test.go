package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func noticesDaemon(t *testing.T) *Daemon {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	d := &Daemon{DB: db, Log: slog.Default(), IPC: ipc.NewServer("token", "test", 1, slog.Default())}
	d.notices = newAppNotices(d)
	return d
}

// settled drains the queue and returns history newest first.
func settled(t *testing.T, d *Daemon) []notifications.Notification {
	t.Helper()
	d.notices.close()
	list, err := notifications.New(d.DB.DB).List(context.Background(), notifications.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestTaskNotificationsFollowTheTask(t *testing.T) {
	d := noticesDaemon(t)
	te := newTaskEvents(nil, nil)
	te.notices = d.notices
	s := agents.Session{ID: "t1", Title: "Fix login", Status: agents.StatusWorking}
	te.created(s)
	s.Status, s.Activity = agents.StatusNeedsYou, "Waiting for approval: Bash go test ./..."
	te.observe("agents.updated", s)
	te.observe("agents.updated", s) // republished: still one approval
	s.Status = agents.StatusWorking
	te.observe("agents.updated", s)
	s.Status, s.Activity = agents.StatusNeedsYou, "Waiting for your answer"
	te.observe("agents.updated", s)
	s.Status, s.Result = agents.StatusDone, "Login fixed\nDetails follow"
	te.observe("agents.updated", s)
	te.finished(s)
	// A task a bot started reports to the bot; only its waits notify.
	owned := agents.Session{ID: "t2", Title: "Bot job", Status: agents.StatusWorking, Owner: "bot:b1"}
	te.created(owned)
	owned.Status, owned.Error = agents.StatusFailed, "exit 1"
	te.observe("agents.updated", owned)

	list := settled(t, d)
	if len(list) != 3 {
		t.Fatalf("history: %+v", list)
	}
	finished, second, first := list[0], list[1], list[2]
	if first.Kind != notifications.Approval || first.NeedsYou || first.Body != "Waiting for approval: Bash go test ./..." || first.Target != "task:t1" {
		t.Fatalf("first wait: %+v", first)
	}
	if second.Kind != notifications.Approval || second.NeedsYou || second.Body != "Waiting for your answer" {
		t.Fatalf("second wait must be its own entry and end with the task: %+v", second)
	}
	if finished.Kind != notifications.Finished || finished.Title != "Fix login" || finished.Body != "Login fixed" {
		t.Fatalf("finished: %+v", finished)
	}
}

func TestBotNotifications(t *testing.T) {
	d := noticesDaemon(t)
	n := d.notices
	text := func(s string) []chat.MsgPart { return []chat.MsgPart{{Type: chat.PartText, Text: s}} }
	// A reply's author is the bot itself (chat engine: Author "bot:<id>").
	reply := chat.Message{ID: "m1", BotID: "b1", Role: "assistant", Author: "bot:b1", Status: chat.StatusComplete, Parts: text("Here is the summary.\nMore")}
	n.chatMessage("chat.message", chat.Message{ID: "m1", BotID: "b1", Role: "assistant", Author: "bot:b1", Status: chat.StatusRunning})
	n.chatMessage("chat.message", reply)
	n.chatMessage("chat.message", reply) // republished snapshot
	n.chatMessage("chat.message", chat.Message{ID: "m2", BotID: "b1", Role: "assistant", Author: "bot:b1", Status: chat.StatusComplete,
		Parts: []chat.MsgPart{{Type: chat.PartQuestion, Text: "Which repo?"}}})
	n.chatMessage("chat.message", chat.Message{ID: "m3", BotID: "b2", Role: "assistant", Author: "bot:draft", Status: chat.StatusComplete, Parts: text("builder preview")})
	n.chatMessage("chat.message", chat.Message{ID: "m4", BotID: "b1", Role: "user", Author: "user", Status: chat.StatusComplete, Parts: text("orole-os")})
	n.chatMessage("chat.message", chat.Message{ID: "m5", BotID: "b1", Role: "assistant", Author: "bot:b1", Status: chat.StatusError, Error: "rate limited"})

	list := settled(t, d)
	if len(list) != 3 {
		t.Fatalf("history: %+v", list)
	}
	failed, asked, replied := list[0], list[1], list[2]
	if replied.Kind != notifications.Reply || replied.Body != "Here is the summary." || replied.Target != "bot:b1" || replied.Title != "Bot" {
		t.Fatalf("reply: %+v", replied)
	}
	if asked.Kind != notifications.Approval || asked.Body != "Which repo?" || asked.NeedsYou {
		t.Fatalf("question must be resolved by the user's answer: %+v", asked)
	}
	if failed.Kind != notifications.Failed || failed.Body != "rate limited" {
		t.Fatalf("failure: %+v", failed)
	}
}

func TestPermissionRequestNotifications(t *testing.T) {
	d := noticesDaemon(t)
	pub := policyPublisher{pub: d.IPC, notices: d.notices}
	bot := policy.Pending{ID: "p1", Actor: "bot:b1", Action: "mcp.tool.call", Detail: "Send the email to ada@example.com"}
	mine := policy.Pending{ID: "p2", Actor: "user", Action: "github.issue.create"}
	pub.Publish("policy.pending", map[string]any{"items": []policy.Pending{bot, mine}})
	pub.Publish("policy.pending", map[string]any{"items": []policy.Pending{bot, mine}})
	pub.Publish("policy.resolved", map[string]any{"id": "p1"})

	list := settled(t, d)
	if len(list) != 1 {
		t.Fatalf("only the bot's request notifies, once: %+v", list)
	}
	if got := list[0]; got.Kind != notifications.Approval || got.Target != "bot:b1" || got.Body != bot.Detail || got.NeedsYou {
		t.Fatalf("permission: %+v", got)
	}
}

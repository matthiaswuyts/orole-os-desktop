package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/chat/tools"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

// awaitNotice returns the first stored notification whose title contains title.
func awaitNotice(t *testing.T, d *Daemon, title string) notifications.Notification {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ns, err := notifications.New(d.DB.DB).List(context.Background(), notifications.ListOptions{Limit: 50})
		if err == nil {
			for _, n := range ns {
				if strings.Contains(n.Title, title) {
					return n
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no notice %q", title)
	return notifications.Notification{}
}

// The daemon points the chat follow-up hook at Jarvis: a code task a bot started on a call that has ended is announced
// with a notification that opens the bot (its answer lands in the call's thread, which raises no chat notification).
func TestTaskFollowUpOfAnEndedCallIsAnnouncedByJarvis(t *testing.T) {
	d := jarvisDaemon(t)
	ctx := context.Background()
	setAgents(t, d, `{"botYolo":true}`)
	b, err := d.Chat.SaveBot(ctx, jarvisBotInput(""))
	if err != nil {
		t.Fatal(err)
	}
	d.reportToOwner(events.TopicAgentFailed, agents.Session{ID: "t1", Title: "Fix login", Kind: "claude", Status: agents.StatusFailed,
		Error: "boom", Owner: chat.BotPrefix + b.ID, CallID: "call-gone"})
	n := awaitNotice(t, d, "Fix login failed")
	if n.Body != "From your call with Atlas. Open the call to read it." || n.Target != "bot:"+b.ID || n.Level != notifications.Info || n.Source != jarvisSource {
		t.Fatalf("notice %+v", n)
	}
}

// A bot_reported rule a bot saved through its tool is stored (the table allows the trigger) and matches a report by the
// callee's name; with no phone set up the owner is told the bot did not call.
func TestBotReportedRuleFiresOnAReport(t *testing.T) {
	d := jarvisDaemon(t)
	ctx := context.Background()
	setAgents(t, d, `{"botYolo":true}`)
	b, err := d.Chat.SaveBot(ctx, jarvisBotInput(""))
	if err != nil {
		t.Fatal(err)
	}
	rt := jarvisTools{d}
	got, err := rt.AddRule(ctx, b.ID, tools.JarvisRule{Trigger: "bot_reported", Target: "nova"})
	if err != nil || got.Trigger != "bot_reported" || got.Target != "nova" {
		t.Fatalf("rule %+v err=%v", got, err)
	}
	if _, err := rt.AddRule(ctx, b.ID, tools.JarvisRule{Trigger: "bot_reported", Target: ""}); err == nil {
		t.Fatal("a bot_reported rule without a target was accepted")
	}
	// A report from another callee does nothing; the one the rule names fires it.
	if d.Jarvis.OnFollowUp(chat.FollowUp{BotID: b.ID, Kind: chat.FollowUpReport, From: "Atlas2", Status: "complete", Text: "x"}) {
		t.Fatal("answered by a call")
	}
	if d.Jarvis.OnFollowUp(chat.FollowUp{BotID: b.ID, Kind: chat.FollowUpReport, From: "Nova", Status: "failed", Text: "boom"}) {
		t.Fatal("answered by a call")
	}
	n := awaitNotice(t, d, "Atlas did not call")
	if !strings.Contains(n.Body, "Nova reported back (failed)") || n.Target != "bot:"+b.ID {
		t.Fatalf("notice %+v", n)
	}
	if n := countSource(t, d, jarvisSource); n != 1 {
		t.Fatalf("jarvis notices=%d, want 1", n)
	}
}

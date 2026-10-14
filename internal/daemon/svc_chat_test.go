package daemon_test

import (
	"context"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
)

// TestChatOverIPCWithoutProvider exercises the real daemon wiring: provider status, bot CRUD,
// tool availability, and a send that must fail honestly (no provider connected) instead of faking a reply.
func TestChatOverIPCWithoutProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	defer daemon.UseMemoryLLMCredentials()() // never touch the user's real Credential Manager
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := control.NewClient(dir, "chat-wiring-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	must := func(method string, params, result any) {
		t.Helper()
		if err := c.Call(ctx, method, params, result); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}

	var providers []llm.ProviderStatus
	must("llm.providers.list", struct{}{}, &providers)
	seen := map[string]bool{}
	for _, p := range providers {
		seen[p.ID] = true
		if p.Connected {
			t.Fatalf("%s connected on a fresh data dir", p.ID)
		}
	}
	if !seen[llm.ProviderOpenRouter] || !seen[llm.ProviderOpenAI] {
		t.Fatalf("built-in providers missing: %+v", providers)
	}

	var bot chat.Bot
	must("chat.bots.save", chat.BotInput{
		Name: "Researcher", Avatar: "R", Color: "blue", Instructions: "Be brief.",
		Config: chat.BotConfig{Tools: []chat.BotTool{{ID: chat.ToolWebSearch}}},
	}, &bot)
	if bot.ID == "" || bot.Name != "Researcher" {
		t.Fatalf("saved bot: %+v", bot)
	}
	var bots []chat.Bot
	must("chat.bots.list", struct{}{}, &bots)
	if len(bots) != 1 || bots[0].ID != bot.ID {
		t.Fatalf("bots: %+v", bots)
	}

	var infos []chat.ToolInfo
	must("chat.tools.list", chat.ToolsRequest{}, &infos)
	if len(infos) == 0 {
		t.Fatal("chat.tools.list returned nothing")
	}
	for _, in := range infos {
		if !in.Available && in.Reason == "" {
			t.Fatalf("unavailable tool %q has no reason", in.ID)
		}
	}

	var sent chat.Message
	must("chat.send", chat.SendRequest{BotID: bot.ID, Text: "hello", ClientNonce: "n1"}, &sent)
	if sent.Role != "user" || sent.Seq != 1 {
		t.Fatalf("stored user message: %+v", sent)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		var page chat.MessagePage
		must("chat.messages.list", chat.MessagesRequest{BotID: bot.ID}, &page)
		if n := len(page.Messages); n == 2 && page.Messages[1].Status != chat.StatusRunning && page.Messages[1].Status != chat.StatusPending {
			reply := page.Messages[1]
			if reply.Status != chat.StatusError || reply.Error == "" {
				t.Fatalf("reply without provider must be an explained error, got %+v", reply)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no terminal reply: %+v", page.Messages)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The real engine's reply (authored by the bot) reaches the notification center under the bot's name.
	for {
		var list daemon.NotificationList
		must("notifications.list", notifications.ListOptions{}, &list)
		if len(list.Items) == 1 {
			if n := list.Items[0]; n.Kind != notifications.Failed || n.Target != "bot:"+bot.ID || n.Title != "Researcher" {
				t.Fatalf("reply notification: %+v", n)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the bot's reply did not notify: %+v", list.Items)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// A retried nonce returns the stored message instead of duplicating it.
	var again chat.Message
	must("chat.send", chat.SendRequest{BotID: bot.ID, Text: "hello", ClientNonce: "n1"}, &again)
	if again.ID != sent.ID {
		t.Fatalf("nonce retry stored a new message: %s vs %s", again.ID, sent.ID)
	}

	must("chat.clear", chat.BotRef{ID: bot.ID}, nil)
	var page chat.MessagePage
	must("chat.messages.list", chat.MessagesRequest{BotID: bot.ID}, &page)
	if len(page.Messages) != 0 {
		t.Fatalf("clear left %d messages", len(page.Messages))
	}
}

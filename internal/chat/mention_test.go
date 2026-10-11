package chat

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
)

func TestMentionedBotAnswersInTheConversationWithItsContext(t *testing.T) {
	var mu sync.Mutex
	var seen []llm.Request
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta {
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
		if strings.Contains(req.System, "Be Mentioned.") {
			return []llm.Delta{llmtest.Text("mentioned here")}
		}
		return []llm.Delta{llmtest.Text("host reply")}
	})
	host := e.bot("Host")
	other := e.bot("Mentioned")

	e.send(host.ID, "first")
	e.idle(host.ID)
	if _, err := e.m.Send(context.Background(), SendRequest{BotID: host.ID, Text: "@Mentioned thoughts?", Mentions: []string{other.ID}}); err != nil {
		t.Fatal(err)
	}
	e.idle(host.ID)

	h := e.history(host.ID)
	if len(h) != 4 {
		t.Fatalf("conversation: %+v", h)
	}
	reply := h[3]
	if reply.Author != "bot:"+other.ID || replyText(reply) != "mentioned here" || reply.Status != StatusComplete {
		t.Fatalf("mention reply: %+v", reply)
	}
	if got := e.history(other.ID); len(got) != 0 {
		t.Fatalf("the mentioned bot's own conversation must stay untouched: %+v", got)
	}

	mu.Lock()
	defer mu.Unlock()
	req := seen[len(seen)-1]
	if !strings.Contains(req.System, "@mentioned you in their conversation with Host") {
		t.Fatalf("system prompt lacks the mention note: %s", req.System)
	}
	var all strings.Builder
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			all.WriteString(p.Text)
		}
	}
	if !strings.Contains(all.String(), "first") || !strings.Contains(all.String(), "host reply") {
		t.Fatalf("the conversation is not in the context: %s", all.String())
	}
}

func TestAnotherBotsReplyIsQuotedAsDataForTheHost(t *testing.T) {
	var mu sync.Mutex
	var last llm.Request
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta {
		mu.Lock()
		last = req
		mu.Unlock()
		return []llm.Delta{llmtest.Text("ok")}
	})
	host := e.bot("Host")
	other := e.bot("Mentioned")
	if _, err := e.m.Send(context.Background(), SendRequest{BotID: host.ID, Text: "@Mentioned hi", Mentions: []string{other.ID}}); err != nil {
		t.Fatal(err)
	}
	e.idle(host.ID)
	e.send(host.ID, "and you?")
	e.idle(host.ID)

	mu.Lock()
	defer mu.Unlock()
	var asAssistant, quoted bool
	for _, m := range last.Messages {
		for _, p := range m.Parts {
			if m.Role == llm.RoleAssistant && strings.Contains(p.Text, "ok") {
				asAssistant = true
			}
			if m.Role == llm.RoleUser && strings.Contains(p.Text, `<untrusted source="reply of bot Mentioned">`) {
				quoted = true
			}
		}
	}
	if asAssistant || !quoted {
		t.Fatalf("the mentioned bot's reply must reach the host as quoted data, not as its own words: %+v", last.Messages)
	}
}

func TestSeveralMentionsAnswerInOrderAndInvalidOnesAreRefused(t *testing.T) {
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta {
		switch {
		case strings.Contains(req.System, "Be A."):
			return []llm.Delta{llmtest.Text("from A")}
		case strings.Contains(req.System, "Be B."):
			return []llm.Delta{llmtest.Text("from B")}
		}
		return []llm.Delta{llmtest.Text("host")}
	})
	host, a, b := e.bot("Host"), e.bot("A"), e.bot("B")
	if _, err := e.m.Send(context.Background(), SendRequest{BotID: host.ID, Text: "@A @B", Mentions: []string{a.ID, b.ID, host.ID}}); err != nil {
		t.Fatal(err)
	}
	e.idle(host.ID)
	h := e.history(host.ID)
	if len(h) != 3 || replyText(h[1]) != "from A" || replyText(h[2]) != "from B" || h[1].Author != "bot:"+a.ID || h[2].Author != "bot:"+b.ID {
		t.Fatalf("conversation: %+v", h)
	}

	for name, req := range map[string]SendRequest{
		"unknown bot":    {BotID: host.ID, Text: "x", Mentions: []string{"ghost"}},
		"research":       {BotID: host.ID, Text: "x", Mentions: []string{a.ID}, Mode: ModeResearch},
		"too many":       {BotID: host.ID, Text: "x", Mentions: []string{a.ID, b.ID, "c", "d"}},
		"bad skill name": {BotID: host.ID, Text: "x", Skills: []string{""}},
	} {
		if _, err := e.m.Send(context.Background(), req); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestSkillMentionOnlyNamesSkillsTheAnsweringBotOwns(t *testing.T) {
	var mu sync.Mutex
	var system string
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta {
		mu.Lock()
		system = req.System
		mu.Unlock()
		return []llm.Delta{llmtest.Text("ok")}
	})
	b := e.bot("Skilled", func(in *BotInput) { in.Config.Skills = []string{"pdf"} })
	if _, err := e.m.Send(context.Background(), SendRequest{BotID: b.ID, Text: "go", Skills: []string{"pdf", "secret"}}); err != nil {
		t.Fatal(err)
	}
	e.idle(b.ID)
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(system, "these skills for this message: pdf.") || strings.Contains(system, "secret") {
		t.Fatalf("system: %s", system)
	}
}

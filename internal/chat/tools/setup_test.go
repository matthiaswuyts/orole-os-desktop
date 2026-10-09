package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

type memBots struct{ bot chat.Bot }

func (m *memBots) Bot(context.Context, string) (chat.Bot, error) { return m.bot, nil }
func (m *memBots) SaveBot(_ context.Context, in chat.BotInput) (chat.Bot, error) {
	m.bot = chat.Bot{ID: in.ID, Name: in.Name, Avatar: in.Avatar, Description: in.Description, Instructions: in.Instructions, Config: in.Config}
	return m.bot, nil
}

func TestSetupOffersToolsOnlyWhileSettingUp(t *testing.T) {
	tool := &Setup{Bots: &memBots{}}
	setting := chat.Bot{Config: chat.BotConfig{Setup: true}}
	if got := tool.Specs(setting, llm.Caps{Tools: true}); len(got) != 2 {
		t.Fatalf("setup bot: want 2 specs, got %d", len(got))
	}
	if got := tool.Specs(chat.Bot{}, llm.Caps{Tools: true}); len(got) != 1 || got[0].Name != UpdateInstructionsName {
		t.Fatalf("finished bot: want only update_instructions, got %+v", got)
	}
	if got := tool.Specs(setting, llm.Caps{}); got != nil {
		t.Fatalf("model without tools: want no specs, got %d", len(got))
	}
}

func TestAskUserEmitsQuestionPart(t *testing.T) {
	tool := &Setup{Bots: &memBots{}}
	rc := chat.RunContext{BotID: "b", Bot: chat.Bot{Config: chat.BotConfig{Setup: true}}}
	res, err := tool.Call(context.Background(), rc, AskUserName, json.RawMessage(`{"question":" What for? ","options":[{"label":"Writing","description":" Prose "},{"label":" Coding "}]}`))
	if err != nil || res.IsError {
		t.Fatalf("unexpected failure: %v %+v", err, res)
	}
	want := chat.MsgPart{Type: chat.PartQuestion, Text: "What for?", Choices: []chat.Choice{{Label: "Writing"}, {Label: "Coding"}}}
	if len(res.Parts) != 1 || res.Parts[0].Type != want.Type || res.Parts[0].Text != want.Text || len(res.Parts[0].Choices) != 2 || res.Parts[0].Choices[1].Label != "Coding" {
		t.Fatalf("parts = %+v", res.Parts)
	}
	for _, bad := range []string{`{"question":"","options":[{"label":"a"},{"label":"b"}]}`, `{"question":"q","options":[{"label":"only"}]}`, `{"question":"q","options":[{"label":"a"},{"label":"b"},{"label":"c"},{"label":"d"},{"label":"e"},{"label":"f"},{"label":"g"}]}`, `{"question":"q","options":[{"label":"a"},{"label":""}]}`} {
		if res, _ := tool.Call(context.Background(), rc, AskUserName, json.RawMessage(bad)); !res.IsError {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestSaveProfileEndsSetupAndKeepsIdentity(t *testing.T) {
	store := &memBots{bot: chat.Bot{ID: "b", Name: "Muse", Avatar: "abc123", Config: chat.BotConfig{Setup: true}}}
	tool := &Setup{Bots: store}
	rc := chat.RunContext{BotID: "b", Bot: store.bot}
	res, err := tool.Call(context.Background(), rc, SaveProfileName, json.RawMessage(`{"description":"Writes poems","instructions":"You write poems."}`))
	if err != nil || res.IsError {
		t.Fatalf("unexpected failure: %v %+v", err, res)
	}
	if store.bot.Config.Setup || store.bot.Instructions != "You write poems." || store.bot.Description != "Writes poems" || store.bot.Name != "Muse" || store.bot.Avatar != "abc123" {
		t.Fatalf("bot after save = %+v", store.bot)
	}
	if res, _ := tool.Call(context.Background(), chat.RunContext{BotID: "b", Bot: store.bot}, SaveProfileName, json.RawMessage(`{"description":"x","instructions":"y"}`)); !res.IsError {
		t.Fatal("save after setup finished must fail")
	}
}

func TestSetupPromptListsOtherBotsAsFencedData(t *testing.T) {
	self := chat.Bot{ID: "me", Name: "New"}
	got := SetupPrompt(self, []chat.Bot{self, {ID: "a", Name: "Coder", Description: "Writes\nGo"}})
	if !strings.Contains(got, "- Coder: Writes Go") || strings.Contains(got, "- New") || !strings.Contains(got, "<untrusted") {
		t.Fatalf("prompt = %q", got)
	}
	if got := SetupPrompt(self, []chat.Bot{self}); !strings.Contains(got, "no other bots") {
		t.Fatalf("empty prompt = %q", got)
	}
}

func TestUpdateInstructionsRewritesOnlyInstructionsOfAFinishedBot(t *testing.T) {
	store := &memBots{bot: chat.Bot{ID: "b", Name: "Muse", Avatar: "abc", Description: "Writes poems", Instructions: "old"}}
	tool := &Setup{Bots: store}
	rc := chat.RunContext{BotID: "b", Bot: store.bot}
	res, err := tool.Call(context.Background(), rc, UpdateInstructionsName, json.RawMessage(`{"instructions":"You write short poems."}`))
	if err != nil || res.IsError {
		t.Fatalf("unexpected failure: %v %+v", err, res)
	}
	if store.bot.Instructions != "You write short poems." || store.bot.Description != "Writes poems" || store.bot.Name != "Muse" {
		t.Fatalf("bot after update = %+v", store.bot)
	}
	if res, _ := tool.Call(context.Background(), rc, UpdateInstructionsName, json.RawMessage(`{"instructions":"  "}`)); !res.IsError {
		t.Fatal("empty instructions must be refused")
	}
	setting := chat.RunContext{BotID: "b", Bot: chat.Bot{Config: chat.BotConfig{Setup: true}}}
	if res, _ := tool.Call(context.Background(), setting, UpdateInstructionsName, json.RawMessage(`{"instructions":"x"}`)); !res.IsError {
		t.Fatal("update_instructions must not run during setup")
	}
}

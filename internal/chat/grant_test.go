package chat

import (
	"context"
	"slices"
	"testing"
)

func TestGrantSkillAddsOnlyThatSkillAndIsIdempotent(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	b := e.bot("Ada", func(in *BotInput) {
		in.Instructions = "Be Ada."
		in.Config = BotConfig{Skills: []string{"pdf"}, MCPServers: []string{"srv"}, Limits: BotLimits{MaxSteps: 4}}
	})

	if err := e.m.GrantSkill(ctx, b.ID, "grill-me"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.GrantSkill(ctx, b.ID, "grill-me"); err != nil {
		t.Fatal(err)
	}
	got, err := e.m.Bot(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Config.Skills, []string{"pdf", "grill-me"}) {
		t.Fatalf("skills = %v", got.Config.Skills)
	}
	if got.Instructions != "Be Ada." || !slices.Equal(got.Config.MCPServers, []string{"srv"}) || got.Config.Limits.MaxSteps != 4 {
		t.Fatalf("other settings changed: %+v", got)
	}
	if err := e.m.GrantSkill(ctx, "missing", "x"); err == nil {
		t.Fatal("granting to a missing bot succeeded")
	}
}

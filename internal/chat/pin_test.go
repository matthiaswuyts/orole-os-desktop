package chat

import (
	"context"
	"testing"
)

func TestPinningSurvivesSavesAndIsNeverSetByThem(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	a := e.bot("Alpha")
	if a.Config.Pinned {
		t.Fatal("a new bot starts unpinned")
	}

	if err := e.m.PinBot(ctx, a.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := e.m.Bot(ctx, a.ID)
	if !got.Config.Pinned {
		t.Fatalf("pinned: %+v", got.Config)
	}

	// The settings panel saves the whole bot with its own copy of the config: that must not unpin it.
	in := BotInput{ID: a.ID, Name: "Alpha 2", Avatar: got.Avatar, Color: got.Color, Instructions: got.Instructions, Config: got.Config}
	in.Config.Pinned = false
	saved, err := e.m.SaveBot(ctx, in)
	if err != nil || !saved.Config.Pinned || saved.Name != "Alpha 2" {
		t.Fatalf("save kept pin: %+v %v", saved.Config, err)
	}
	// ...and a save cannot pin either.
	b := e.bot("Beta")
	in = BotInput{ID: b.ID, Name: "Beta", Avatar: b.Avatar, Color: b.Color, Config: b.Config}
	in.Config.Pinned = true
	if saved, err := e.m.SaveBot(ctx, in); err != nil || saved.Config.Pinned {
		t.Fatalf("a save must not pin: %+v %v", saved.Config, err)
	}
	created, err := e.m.SaveBot(ctx, BotInput{Name: "Gamma", Config: BotConfig{Pinned: true}})
	if err != nil || created.Config.Pinned {
		t.Fatalf("a new bot cannot start pinned: %+v %v", created.Config, err)
	}

	if err := e.m.PinBot(ctx, a.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.m.Bot(ctx, a.ID); got.Config.Pinned {
		t.Fatal("unpin")
	}
	if err := e.m.PinBot(ctx, "ghost", true); err == nil {
		t.Fatal("pinning a bot that does not exist must fail")
	}
}

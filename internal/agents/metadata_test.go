package agents_test

import (
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/agents"
)

func TestRenameAndPinPersistAndUserNameIsFinal(t *testing.T) {
	e := newEnv(t)
	s := e.start(t, agents.StartSpec{Prompt: "go"})
	e.wait(t, s.ID, "turn", status(agents.StatusDone))

	got, err := e.m.Rename(s.ID, "  Fix the\nlogin\x00 redirect  ")
	if err != nil || got.Title != "Fix the login redirect" {
		t.Fatalf("rename: %+v %v", got, err)
	}
	if stored, _ := e.m.Get(s.ID); stored.Title != "Fix the login redirect" {
		t.Fatalf("rename must persist: %q", stored.Title)
	}
	if _, err := e.m.Rename(s.ID, " \n "); err == nil {
		t.Fatal("an empty title must be refused")
	}
	long, err := e.m.Rename(s.ID, strings.Repeat("x", 500))
	if err != nil || len([]rune(long.Title)) > 81 {
		t.Fatalf("a long title must be capped: %d %v", len([]rune(long.Title)), err)
	}
	if _, err := e.m.Rename("missing", "x"); err == nil {
		t.Fatal("renaming an unknown task must fail")
	}

	pinned, err := e.m.SetPinned(s.ID, true)
	if err != nil || !pinned.Pinned {
		t.Fatalf("pin: %+v %v", pinned, err)
	}
	if stored, _ := e.m.Get(s.ID); !stored.Pinned {
		t.Fatal("pin must persist")
	}
	if unpinned, err := e.m.SetPinned(s.ID, false); err != nil || unpinned.Pinned {
		t.Fatalf("unpin: %+v %v", unpinned, err)
	}
}

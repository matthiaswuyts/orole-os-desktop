package gateway

import (
	"errors"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/skills"
)

// Built-in skills are written for Chat bots (no shell, no files); coding agents never see them.
func TestHubKeepsBuiltinSkillsFromHarnesses(t *testing.T) {
	st, err := skills.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureBuiltin(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save("", "---\nname: alpha\ndescription: Alpha skill\n---\nalpha body\n", ""); err != nil {
		t.Fatal(err)
	}
	h := &Hub{Skills: st}

	list, err := h.SkillList()
	if err != nil || len(list) != 1 || list[0].Name != "alpha" {
		t.Fatalf("SkillList = %+v, %v", list, err)
	}
	if got, err := h.SkillGet("alpha", ""); err != nil || got.Content == "" {
		t.Fatalf("SkillGet(alpha) = %+v, %v", got, err)
	}
	if _, err := h.SkillGet("skill-creator", ""); !errors.Is(err, skills.ErrNotFound) {
		t.Fatalf("SkillGet(skill-creator) = %v, want not found", err)
	}
}

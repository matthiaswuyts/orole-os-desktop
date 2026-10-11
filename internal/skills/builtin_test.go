package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const creator = "skill-creator"

func newBuiltinStore(t *testing.T) *Store {
	t.Helper()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func ensure(t *testing.T, st *Store) {
	t.Helper()
	if err := st.EnsureBuiltin(); err != nil {
		t.Fatalf("EnsureBuiltin: %v", err)
	}
}

func TestBuiltinSkillCreatorIsInstalledAndWellFormed(t *testing.T) {
	st := newBuiltinStore(t)
	ensure(t, st)
	sk, err := st.Get(creator)
	if err != nil {
		t.Fatal(err)
	}
	if sk.Source != SourceBuiltin || !sk.Enabled {
		t.Fatalf("skill = source %q enabled %v", sk.Source, sk.Enabled)
	}
	meta, _, err := ParseFrontmatter(sk.Body)
	if err != nil || meta.Name != creator {
		t.Fatalf("frontmatter = %+v, %v", meta, err)
	}
	// The bot prompt cuts descriptions at 300 characters; the trigger must fit.
	if n := len([]rune(sk.Description)); n == 0 || n > 300 {
		t.Fatalf("description is %d characters, want 1..300", n)
	}
	if len(sk.Files) != 1 || sk.Files[0] != "references/examples.md" {
		t.Fatalf("files = %v", sk.Files)
	}
	if b, err := st.ReadFile(creator, "references/examples.md"); err != nil || !strings.Contains(string(b), "propose_skill") {
		t.Fatalf("examples = %q, %v", b, err)
	}
}

func TestBuiltinInstallIsIdempotent(t *testing.T) {
	st := newBuiltinStore(t)
	ensure(t, st)
	first, _ := st.Get(creator)
	st.now = func() time.Time { return time.Now().Add(time.Hour) }
	ensure(t, st)
	again, _ := st.Get(creator)
	if again.UpdatedAt != first.UpdatedAt {
		t.Fatalf("second run rewrote the skill: %d -> %d", first.UpdatedAt, again.UpdatedAt)
	}
}

func TestBuiltinDeletedSkillStaysDeleted(t *testing.T) {
	st := newBuiltinStore(t)
	ensure(t, st)
	if err := st.Delete(creator); err != nil {
		t.Fatal(err)
	}
	ensure(t, st)
	if _, err := st.Get(creator); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted built-in skill came back: %v", err)
	}
}

func TestBuiltinEditedSkillIsNotOverwrittenButUntouchedOneUpgrades(t *testing.T) {
	st := newBuiltinStore(t)
	ensure(t, st)
	path := filepath.Join(st.Root(), creator, "SKILL.md")

	// Simulate the shipped text changing: the installed copy is an older, unmodified version.
	old := "---\nname: skill-creator\ndescription: old version\n---\nold\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := HashTree(filepath.Join(st.Root(), creator))
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	doc, _ := st.load()
	doc.Builtin[creator] = h
	if err := st.store(doc); err != nil {
		t.Fatal(err)
	}
	st.mu.Unlock()
	if _, err := st.SetEnabled(creator, false); err != nil {
		t.Fatal(err)
	}
	ensure(t, st)
	sk, _ := st.Get(creator)
	if sk.Description == "old version" {
		t.Fatal("an untouched older copy was not upgraded")
	}
	if sk.Enabled {
		t.Fatal("upgrade re-enabled a skill the user disabled")
	}

	// A user edit after install is kept.
	edited := "---\nname: skill-creator\ndescription: mine\n---\nmy own text\n"
	if _, err := st.Save(creator, edited, ""); err != nil {
		t.Fatal(err)
	}
	ensure(t, st)
	if sk, _ := st.Get(creator); sk.Description != "mine" || sk.Source != SourceBuiltin {
		t.Fatalf("user edit lost or source changed: %+v", sk)
	}
}

func TestBuiltinLeavesSameNamedUserSkillAlone(t *testing.T) {
	st := newBuiltinStore(t)
	mine := "---\nname: skill-creator\ndescription: my own creator\n---\nmine\n"
	if _, err := st.Save(creator, mine, "orole"); err != nil {
		t.Fatal(err)
	}
	ensure(t, st)
	sk, _ := st.Get(creator)
	if sk.Description != "my own creator" || sk.Source != "orole" {
		t.Fatalf("user skill replaced: %+v", sk)
	}
}

func TestBuiltinSkillsAreHiddenFromHarnesses(t *testing.T) {
	r := newRig(t)
	ensure(t, r.store)
	if _, err := r.store.Save("", skillText("alpha", "Alpha skill"), ""); err != nil {
		t.Fatal(err)
	}
	if en, _ := r.store.Enabled(); len(en) != 2 {
		t.Fatalf("Enabled = %d skills, want the built-in and alpha", len(en))
	}
	harness, _ := r.store.EnabledForHarnesses()
	if len(harness) != 1 || harness[0].Name != "alpha" {
		t.Fatalf("EnabledForHarnesses = %+v", harness)
	}
	if strings.Contains(r.store.PromptIndex(), creator) {
		t.Fatal("the harness prompt index lists a bots-only skill")
	}
	st := r.doSync(HarnessClaude)
	if len(st.Synced) != 1 || st.Synced[0] != "alpha" {
		t.Fatalf("sync = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(r.def(HarnessClaude).SyncDir, creator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("built-in skill was written to the harness directory: %v", err)
	}
}

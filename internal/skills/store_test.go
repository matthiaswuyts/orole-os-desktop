package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodSkill = "---\nname: pdf-tools\ndescription: Work with PDF files\n---\n# PDF\nUse pdftotext.\n"

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestValidName(t *testing.T) {
	good := []string{"a", "pdf-tools", "a1-b2-c3", strings.Repeat("a", 64)}
	bad := []string{"", "Upper", "has space", "a--b", "-a", "a-", "a_b", "../x", "a/b", `a\b`, "a.b", strings.Repeat("a", 65), "con", "nul", "aux", "prn", "com1", "lpt9"}
	for _, n := range good {
		if err := ValidName(n); err != nil {
			t.Errorf("ValidName(%q) = %v, want ok", n, err)
		}
	}
	for _, n := range bad {
		if err := ValidName(n); !errors.Is(err, ErrInvalid) {
			t.Errorf("ValidName(%q) = %v, want ErrInvalid", n, err)
		}
	}
}

func TestStoreCRUD(t *testing.T) {
	s := newStore(t)
	sk, err := s.Save("ignored", goodSkill, "")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "pdf-tools" || sk.Description != "Work with PDF files" || !sk.Enabled || sk.Source != "orole" || sk.Body != goodSkill || sk.UpdatedAt == 0 {
		t.Fatalf("saved = %+v", sk)
	}
	// Frontmatter name wins; the argument name is only a fallback.
	if _, err := os.Stat(filepath.Join(s.Root(), "pdf-tools", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "ignored")); err == nil {
		t.Fatal("argument name must not create a directory when frontmatter names the skill")
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Body != "" {
		t.Fatalf("list = %+v, %v (bodies are omitted from lists)", list, err)
	}
	got, err := s.Get("pdf-tools")
	if err != nil || got.Body != goodSkill {
		t.Fatalf("get = %+v, %v", got, err)
	}
	// Replace keeps the enabled flag and resource files.
	if err := os.WriteFile(filepath.Join(s.Root(), "pdf-tools", "notes.md"), []byte("n"), 0o600); err != nil {
		t.Fatal(err)
	}
	off, err := s.SetEnabled("pdf-tools", false)
	if err != nil || off.Enabled {
		t.Fatalf("disable = %+v, %v", off, err)
	}
	again, err := s.Save("", strings.Replace(goodSkill, "Use pdftotext.", "Use qpdf.", 1), "")
	if err != nil || again.Enabled || len(again.Files) != 1 || again.Files[0] != "notes.md" || !strings.Contains(again.Body, "qpdf") {
		t.Fatalf("replace = %+v, %v", again, err)
	}
	if en, _ := s.Enabled(); len(en) != 0 {
		t.Fatalf("disabled skill listed as enabled: %+v", en)
	}
	if _, err := s.SetEnabled("pdf-tools", true); err != nil {
		t.Fatal(err)
	}
	if en, _ := s.Enabled(); len(en) != 1 || en[0].Body == "" {
		t.Fatalf("enabled = %+v", en)
	}
	if err := s.Delete("pdf-tools"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("pdf-tools"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
	if err := s.Delete("pdf-tools"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
}

func TestSaveRejectsBadInput(t *testing.T) {
	s := newStore(t)
	cases := map[string]struct{ name, body string }{
		"no frontmatter":   {"x", "# hi"},
		"no description":   {"x", "---\nname: x\n---\nbody"},
		"no name at all":   {"", "---\ndescription: d\n---\n"},
		"bad slug in name": {"x", "---\nname: ../evil\ndescription: d\n---\n"},
		"traversal arg":    {"../evil", "---\ndescription: d\n---\n"},
		"too big":          {"x", "---\nname: x\ndescription: d\n---\n" + strings.Repeat("a", MaxSkillMD)},
		"nul byte":         {"x", "---\nname: x\ndescription: d\n---\n\x00"},
		"long description": {"x", "---\nname: x\ndescription: " + strings.Repeat("d", MaxDescription+1) + "\n---\n"},
	}
	for n, c := range cases {
		if _, err := s.Save(c.name, c.body, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", n, err)
		}
	}
	entries, _ := os.ReadDir(s.Root())
	for _, e := range entries {
		if e.Name() != registryFile {
			t.Errorf("rejected input left %q behind", e.Name())
		}
	}
}

func TestReadFileTraversalAndLinks(t *testing.T) {
	s := newStore(t)
	if _, err := s.Save("", goodSkill, ""); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), "pdf-tools")
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "refs", "a.md"), []byte("ref a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root(), "secret.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := s.ReadFile("pdf-tools", "refs/a.md"); err != nil || string(b) != "ref a" {
		t.Fatalf("read = %q, %v", b, err)
	}
	if b, err := s.ReadFile("pdf-tools", ""); err != nil || string(b) != goodSkill {
		t.Fatalf("read SKILL.md = %q, %v", b, err)
	}
	bad := []string{"../secret.txt", "refs/../../secret.txt", "/etc/passwd", `refs\a.md`, "C:/Windows/win.ini", "C:win.ini", "refs//a.md", "./refs/a.md", "refs/.", ".orole-managed", "refs/nul", "a\x00b", strings.Repeat("a/", 20) + "b"}
	for _, rel := range bad {
		if _, err := s.ReadFile("pdf-tools", rel); !errors.Is(err, ErrInvalid) {
			t.Errorf("ReadFile(%q) = %v, want ErrInvalid", rel, err)
		}
	}
	for _, name := range []string{"../pdf-tools", "pdf-tools/..", "..", "PDF-TOOLS"} {
		if _, err := s.ReadFile(name, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("ReadFile name %q = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := s.ReadFile("pdf-tools", "refs/missing.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing = %v", err)
	}
	// A symlink planted inside the skill must not be followed (skipped on platforms without symlinks).
	if err := os.Symlink(filepath.Join(s.Root(), "secret.txt"), filepath.Join(dir, "refs", "link.md")); err == nil {
		if _, err := s.ReadFile("pdf-tools", "refs/link.md"); !errors.Is(err, ErrNotFound) {
			t.Errorf("symlink read = %v, want ErrNotFound", err)
		}
		sk, _ := s.Get("pdf-tools")
		for _, f := range sk.Files {
			if strings.Contains(f, "link") {
				t.Errorf("symlink listed as a resource file: %v", sk.Files)
			}
		}
	}
}

func TestCorruptRegistryIsReported(t *testing.T) {
	s := newStore(t)
	if _, err := s.Save("", goodSkill, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root(), registryFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("list on corrupt registry = %v", err)
	}
}

func TestPromptIndex(t *testing.T) {
	s := newStore(t)
	if got := s.PromptIndex(); got != "" {
		t.Fatalf("empty registry index = %q", got)
	}
	if _, err := s.Save("", goodSkill, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("", "---\nname: hidden\ndescription: off\n---\n", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetEnabled("hidden", false); err != nil {
		t.Fatal(err)
	}
	idx := s.PromptIndex()
	if !strings.Contains(idx, "- pdf-tools: Work with PDF files") || strings.Contains(idx, "hidden") || !strings.Contains(idx, "orole_get_skill") {
		t.Fatalf("index = %q", idx)
	}
}

func TestHashTreeIgnoresHiddenAndTracksContent(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("SKILL.md", goodSkill)
	write("a/b.txt", "1")
	h1, err := HashTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(markerFile, "{}")
	write(".DS_Store", "x")
	if h2, _ := HashTree(dir); h2 != h1 {
		t.Fatal("hidden files must not affect the hash")
	}
	write("a/b.txt", "2")
	if h3, _ := HashTree(dir); h3 == h1 {
		t.Fatal("content change must change the hash")
	}
}

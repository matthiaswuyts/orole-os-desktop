package skills

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type rig struct {
	t     *testing.T
	home  string
	store *Store
	sync  *Syncer
}

func newRig(t *testing.T) *rig {
	t.Helper()
	home := t.TempDir()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, home: home, store: st, sync: &Syncer{
		Store: st, Loc: NewLocations(home, nil), GatewayExe: `C:\Orole\orole-daemon.exe`, DataDir: `C:\data\orole`,
		Installed: func(string) bool { return true },
	}}
}

func (r *rig) def(id string) harnessDef {
	d, ok := r.sync.Loc.def(id)
	if !ok {
		r.t.Fatalf("no harness %s", id)
	}
	return d
}

func (r *rig) writeFile(p, body string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) read(p string) string {
	r.t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func (r *rig) state(harness string) SyncState {
	r.t.Helper()
	st, err := r.sync.State(harness)
	if err != nil || len(st) != 1 {
		r.t.Fatalf("state: %v %v", st, err)
	}
	return st[0]
}

func (r *rig) doSync(harness string) SyncState {
	r.t.Helper()
	st, err := r.sync.Sync(harness)
	if err != nil || len(st) != 1 {
		r.t.Fatalf("sync: %v %v", st, err)
	}
	return st[0]
}

func skillText(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\nInstructions for " + name + "\n"
}

func TestSyncSkillsLifecycle(t *testing.T) {
	r := newRig(t)
	if _, err := r.store.Save("", skillText("alpha", "Alpha skill"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Save("", skillText("beta", "Beta skill"), ""); err != nil {
		t.Fatal(err)
	}
	r.writeFile(filepath.Join(r.store.Root(), "alpha", "refs", "r.md"), "resource")
	claude := r.def(HarnessClaude)

	if st := r.state(HarnessClaude); len(st.Missing) != 2 || len(st.Synced) != 0 {
		t.Fatalf("before sync: %+v", st)
	}
	st := r.doSync(HarnessClaude)
	if len(st.Synced) != 2 || len(st.Missing) != 0 || len(st.Outdated) != 0 || st.Error != "" {
		t.Fatalf("after sync: %+v", st)
	}
	if got := r.read(filepath.Join(claude.SyncDir, "alpha", "refs", "r.md")); got != "resource" {
		t.Fatalf("resource not synced: %q", got)
	}
	if !readMarker(filepath.Join(claude.SyncDir, "alpha", markerFile), "alpha") {
		t.Fatal("synced skill has no marker")
	}

	// Idempotent: a second sync changes nothing on disk.
	before := r.read(filepath.Join(claude.SyncDir, "alpha", "SKILL.md"))
	fi1, _ := os.Stat(filepath.Join(claude.SyncDir, "alpha", "SKILL.md"))
	r.doSync(HarnessClaude)
	fi2, _ := os.Stat(filepath.Join(claude.SyncDir, "alpha", "SKILL.md"))
	if !fi1.ModTime().Equal(fi2.ModTime()) || before != r.read(filepath.Join(claude.SyncDir, "alpha", "SKILL.md")) {
		t.Fatal("second sync rewrote an up-to-date skill")
	}

	// Registry edit => outdated until synced.
	if _, err := r.store.Save("", skillText("alpha", "Alpha skill v2"), ""); err != nil {
		t.Fatal(err)
	}
	if st := r.state(HarnessClaude); len(st.Outdated) != 1 || st.Outdated[0] != "alpha" {
		t.Fatalf("after edit: %+v", st)
	}
	r.doSync(HarnessClaude)
	if got := r.read(filepath.Join(claude.SyncDir, "alpha", "SKILL.md")); !strings.Contains(got, "v2") {
		t.Fatalf("edit not synced: %q", got)
	}

	// Disable removes Orole's copy; delete too.
	if _, err := r.store.SetEnabled("beta", false); err != nil {
		t.Fatal(err)
	}
	r.doSync(HarnessClaude)
	if _, err := os.Stat(filepath.Join(claude.SyncDir, "beta")); err == nil {
		t.Fatal("disabled skill still synced")
	}
	if err := r.store.Delete("alpha"); err != nil {
		t.Fatal(err)
	}
	st = r.doSync(HarnessClaude)
	if _, err := os.Stat(filepath.Join(claude.SyncDir, "alpha")); err == nil || len(st.Synced) != 0 {
		t.Fatalf("deleted skill still synced: %+v", st)
	}
}

func TestSyncNeverTouchesUserSkills(t *testing.T) {
	r := newRig(t)
	claude := r.def(HarnessClaude)
	r.writeFile(filepath.Join(claude.SyncDir, "alpha", "SKILL.md"), skillText("alpha", "the user's own"))
	r.writeFile(filepath.Join(claude.SyncDir, "unrelated", "SKILL.md"), skillText("unrelated", "user"))
	if _, err := r.store.Save("", skillText("alpha", "Orole's alpha"), ""); err != nil {
		t.Fatal(err)
	}
	st := r.doSync(HarnessClaude)
	if got := r.read(filepath.Join(claude.SyncDir, "alpha", "SKILL.md")); !strings.Contains(got, "the user's own") {
		t.Fatalf("user skill overwritten: %q", got)
	}
	if len(st.Missing) != 1 || !strings.Contains(st.Error, "alpha") || !strings.Contains(st.Error, "import") {
		t.Fatalf("conflict not reported: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(claude.SyncDir, "alpha", markerFile)); err == nil {
		t.Fatal("marker written into a user skill")
	}
	// Disabling/deleting in the registry never removes unmarked skills.
	if err := r.store.Delete("alpha"); err != nil {
		t.Fatal(err)
	}
	r.doSync(HarnessClaude)
	for _, n := range []string{"alpha", "unrelated"} {
		if _, err := os.Stat(filepath.Join(claude.SyncDir, n, "SKILL.md")); err != nil {
			t.Fatalf("user skill %s removed: %v", n, err)
		}
	}
}

func TestSyncAdoptsIdenticalImportedSkill(t *testing.T) {
	r := newRig(t)
	claude := r.def(HarnessClaude)
	r.writeFile(filepath.Join(claude.SyncDir, "alpha", "SKILL.md"), skillText("alpha", "mine"))
	found, err := Discover(r.store, r.sync.Loc)
	if err != nil || len(found) != 1 || found[0].Harness != HarnessClaude || found[0].Name != "alpha" || found[0].Description != "mine" {
		t.Fatalf("discover = %+v, %v", found, err)
	}
	sk, err := Import(r.store, r.sync.Loc, HarnessClaude, "alpha")
	if err != nil || sk.Source != "imported:claude" || !sk.Enabled {
		t.Fatalf("import = %+v, %v", sk, err)
	}
	if again, _ := Discover(r.store, r.sync.Loc); len(again) != 0 {
		t.Fatalf("imported skill still discovered: %+v", again)
	}
	st := r.doSync(HarnessClaude)
	if len(st.Synced) != 1 || st.Error != "" {
		t.Fatalf("sync after import: %+v", st)
	}
	if !readMarker(filepath.Join(claude.SyncDir, "alpha", markerFile), "alpha") {
		t.Fatal("identical imported copy not adopted")
	}
}

func TestDiscoverAndImport(t *testing.T) {
	r := newRig(t)
	codex, omp := r.def(HarnessCodex), r.def(HarnessOmp)
	r.writeFile(filepath.Join(codex.ScanDirs[1], "legacy", "SKILL.md"), skillText("legacy", "from ~/.codex/skills"))
	r.writeFile(filepath.Join(codex.ScanDirs[0], "shared", "SKILL.md"), skillText("shared", "from ~/.agents/skills"))
	r.writeFile(filepath.Join(codex.ScanDirs[0], "shared", "scripts", "run.sh"), "echo hi")
	r.writeFile(filepath.Join(omp.SyncDir, "ompskill", "SKILL.md"), skillText("ompskill", "omp"))
	r.writeFile(filepath.Join(omp.SyncDir, "nodesc", "SKILL.md"), "# no frontmatter")  // importable by dir name only if valid
	r.writeFile(filepath.Join(omp.SyncDir, "not-a-skill", "README.md"), "x")           // no SKILL.md
	r.writeFile(filepath.Join(omp.SyncDir, ".hidden", "SKILL.md"), skillText("h", "")) // hidden
	r.writeFile(filepath.Join(omp.SyncDir, "orole-made", "SKILL.md"), skillText("orole-made", "ours"))
	if err := writeMarker(filepath.Join(omp.SyncDir, "orole-made"), "orole-made", "h"); err != nil {
		t.Fatal(err)
	}
	found, err := Discover(r.store, r.sync.Loc)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range found {
		names = append(names, d.Harness+"/"+d.Name)
	}
	want := "codex/legacy codex/shared omp/nodesc omp/ompskill"
	if strings.Join(names, " ") != want {
		t.Fatalf("discovered %v, want %s", names, want)
	}

	sk, err := Import(r.store, r.sync.Loc, HarnessCodex, "shared")
	if err != nil || sk.Source != "imported:codex" || len(sk.Files) != 1 || sk.Files[0] != "scripts/run.sh" {
		t.Fatalf("import = %+v, %v", sk, err)
	}
	if b, err := r.store.ReadFile("shared", "scripts/run.sh"); err != nil || string(b) != "echo hi" {
		t.Fatalf("resource import: %q %v", b, err)
	}
	// Importing the same name again must not overwrite.
	if _, err := Import(r.store, r.sync.Loc, HarnessCodex, "shared"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("re-import = %v, want ErrInvalid", err)
	}
	// Import cannot be steered outside the harness directories.
	for _, name := range []string{"../shared", "..", "shared/../legacy", ""} {
		if _, err := Import(r.store, r.sync.Loc, HarnessCodex, name); !errors.Is(err, ErrNotFound) {
			t.Errorf("Import(%q) = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := Import(r.store, r.sync.Loc, "nope", "x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown harness = %v", err)
	}
	// A skill without frontmatter description cannot be imported (the registry requires one).
	if _, err := Import(r.store, r.sync.Loc, HarnessOmp, "nodesc"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("frontmatter-less import = %v", err)
	}
	// The source directory is untouched.
	if _, err := os.Stat(filepath.Join(codex.ScanDirs[0], "shared", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestSyncRefusesLinkedTargetDir(t *testing.T) {
	r := newRig(t)
	claude := r.def(HarnessClaude)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(claude.SyncDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(outside, claude.SyncDir); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := r.store.Save("", skillText("alpha", "a"), ""); err != nil {
		t.Fatal(err)
	}
	st := r.doSync(HarnessClaude)
	if !strings.Contains(st.Error, "link") {
		t.Fatalf("linked target dir accepted: %+v", st)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("wrote through the link: %v", ents)
	}
}

func TestSyncDoesNotRemoveThroughSymlinkedSkill(t *testing.T) {
	r := newRig(t)
	claude := r.def(HarnessClaude)
	outside := t.TempDir()
	r.writeFile(filepath.Join(outside, "SKILL.md"), skillText("linked", "x"))
	if err := writeMarker(outside, "linked", "h"); err != nil { // would qualify for removal if the link were followed
		t.Fatal(err)
	}
	if err := os.MkdirAll(claude.SyncDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(outside, filepath.Join(claude.SyncDir, "linked")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	r.doSync(HarnessClaude)
	if _, err := os.Stat(filepath.Join(outside, "SKILL.md")); err != nil {
		t.Fatalf("symlink target damaged: %v", err)
	}
}

func TestSyncUnavailableAndUnknownHarness(t *testing.T) {
	r := newRig(t)
	r.sync.Installed = func(id string) bool { return id != HarnessCodex }
	if _, err := r.sync.Sync("nope"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown harness: %v", err)
	}
	st := r.doSync(HarnessCodex)
	if st.Available || !strings.Contains(st.Error, "not installed") {
		t.Fatalf("unavailable: %+v", st)
	}
	if _, err := os.Stat(r.def(HarnessCodex).MCPFile); err == nil {
		t.Fatal("wrote config for an uninstalled harness")
	}
	all, err := r.sync.Sync("")
	if err != nil || len(all) != 3 {
		t.Fatalf("sync all = %v %v", all, err)
	}
	for _, s := range all {
		if s.Harness == HarnessCodex && s.Error != "" {
			t.Fatalf("sync-all must skip uninstalled harnesses silently: %+v", s)
		}
	}
}

// ---- gateway entries ----

func (r *rig) jsonServers(p string) map[string]map[string]any {
	r.t.Helper()
	var doc struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(r.read(p)), &doc); err != nil {
		r.t.Fatal(err)
	}
	return doc.MCPServers
}

func TestClaudeGatewayEntry(t *testing.T) {
	for _, h := range []string{HarnessClaude, HarnessOmp} {
		t.Run(h, func(t *testing.T) {
			r := newRig(t)
			d := r.def(h)
			if st := r.state(h); st.Gateway != GatewayMissing || st.Error != "" {
				t.Fatalf("no config yet: %+v", st)
			}
			// Existing config with other servers, formatting and unknown keys survives.
			orig := "{\n  \"numStartups\": 12345678901234567890,\n  \"mcpServers\": {\n    \"github\": {\"type\": \"http\", \"url\": \"https://x\"}\n  },\n  \"projects\": {\"C:/a\": {\"allowedTools\": []}}\n}\n"
			r.writeFile(d.MCPFile, orig)
			st := r.doSync(h)
			if st.Gateway != GatewayInstalled || st.Error != "" {
				t.Fatalf("after sync: %+v", st)
			}
			got := r.read(d.MCPFile)
			if !strings.Contains(got, "12345678901234567890") {
				t.Fatalf("number precision lost:\n%s", got)
			}
			servers := r.jsonServers(d.MCPFile)
			if servers["github"]["url"] != "https://x" {
				t.Fatalf("other server lost: %v", servers)
			}
			o := servers["orole"]
			if o["command"] != `C:\Orole\orole-daemon.exe` || o["type"] != "stdio" {
				t.Fatalf("orole entry = %v", o)
			}
			if args, _ := o["args"].([]any); len(args) != 1 || args[0] != "mcp-gateway" {
				t.Fatalf("args = %v", o["args"])
			}
			if env, _ := o["env"].(map[string]any); env["OROLE_HOME"] != `C:\data\orole` {
				t.Fatalf("env = %v", o["env"])
			}
			if !strings.Contains(got, "\"allowedTools\": []") || !strings.Contains(got, "\"C:/a\"") {
				t.Fatalf("projects not preserved:\n%s", got)
			}
			if r.read(d.MCPFile+backupSuffix) != orig {
				t.Fatal("backup is not the original file")
			}
			// Idempotent: no rewrite, no new bytes.
			fi1, _ := os.Stat(d.MCPFile)
			r.doSync(h)
			fi2, _ := os.Stat(d.MCPFile)
			if !fi1.ModTime().Equal(fi2.ModTime()) || r.read(d.MCPFile) != got {
				t.Fatal("second sync modified an up-to-date config")
			}
			// Outdated (executable moved) is detected and repaired; backup keeps the original.
			r.sync.GatewayExe = `D:\new\orole-daemon.exe`
			if st := r.state(h); st.Gateway != GatewayOutdated {
				t.Fatalf("moved exe: %+v", st)
			}
			if st := r.doSync(h); st.Gateway != GatewayInstalled {
				t.Fatalf("repair: %+v", st)
			}
			if r.jsonServers(d.MCPFile)["orole"]["command"] != `D:\new\orole-daemon.exe` {
				t.Fatal("entry not updated")
			}
			if r.read(d.MCPFile+backupSuffix) != orig {
				t.Fatal("backup must keep the pre-Orole original")
			}
		})
	}
}

func TestGatewayJSONRefusesInvalidConfig(t *testing.T) {
	for name, content := range map[string]string{
		"invalid json":        "{\"mcpServers\": {",
		"array root":          "[]",
		"servers not object":  `{"mcpServers": []}`,
		"duplicate servers":   `{"mcpServers": {}, "mcpServers": {}}`,
		"duplicate orole key": `{"mcpServers": {"orole": {}, "orole": {}}}`,
		"trailing garbage":    `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			d := r.def(HarnessClaude)
			r.writeFile(d.MCPFile, content)
			st := r.doSync(HarnessClaude)
			if st.Error == "" || !strings.Contains(st.Error, d.MCPFile) {
				t.Fatalf("invalid config accepted: %+v", st)
			}
			if r.read(d.MCPFile) != content {
				t.Fatalf("invalid config modified: %q", r.read(d.MCPFile))
			}
			if _, err := os.Stat(d.MCPFile + backupSuffix); err == nil {
				t.Fatal("backup made for a refused file")
			}
		})
	}
}

func TestGatewayJSONCreatesMissingFileAndCompactStaysCompact(t *testing.T) {
	r := newRig(t)
	d := r.def(HarnessClaude)
	r.doSync(HarnessClaude)
	if got := r.read(d.MCPFile); !strings.HasPrefix(got, "{\n") || !json.Valid([]byte(got)) {
		t.Fatalf("created = %q", got)
	}
	if _, err := os.Stat(d.MCPFile + backupSuffix); err == nil {
		t.Fatal("backup of a file that did not exist")
	}

	r2 := newRig(t)
	d2 := r2.def(HarnessClaude)
	r2.writeFile(d2.MCPFile, `{"a":1}`)
	r2.doSync(HarnessClaude)
	if got := r2.read(d2.MCPFile); strings.Contains(got, "\n") || !strings.HasPrefix(got, `{"a":1,"mcpServers":{"orole":`) {
		t.Fatalf("compact file reformatted: %q", got)
	}
}

func TestCodexGatewayBlock(t *testing.T) {
	r := newRig(t)
	d := r.def(HarnessCodex)
	orig := "model = \"gpt\"\n\n[desktop]\nx = 1\n\n[mcp_servers.other]\ncommand = \"foo\"\n"
	r.writeFile(d.MCPFile, orig)
	st := r.doSync(HarnessCodex)
	if st.Gateway != GatewayInstalled || st.Error != "" {
		t.Fatalf("sync: %+v", st)
	}
	got := r.read(d.MCPFile)
	if !strings.HasPrefix(got, orig) || !strings.Contains(got, "[mcp_servers.orole]\ncommand = \"C:\\\\Orole\\\\orole-daemon.exe\"\nargs = [\"mcp-gateway\"]\nenv = { \"OROLE_HOME\" = \"C:\\\\data\\\\orole\" }\n") {
		t.Fatalf("config:\n%s", got)
	}
	if r.read(d.MCPFile+backupSuffix) != orig {
		t.Fatal("backup is not the original")
	}
	// Idempotent.
	r.doSync(HarnessCodex)
	if r.read(d.MCPFile) != got || strings.Count(got, tomlBegin) != 1 {
		t.Fatalf("second sync changed the file:\n%s", r.read(d.MCPFile))
	}
	// Outdated: managed block replaced in place; text around it stays.
	r.sync.GatewayExe = `D:\x\orole-daemon.exe`
	if st := r.state(HarnessCodex); st.Gateway != GatewayOutdated {
		t.Fatalf("outdated: %+v", st)
	}
	r.writeFile(d.MCPFile, r.read(d.MCPFile)+"\n[after]\nk = 1\n")
	r.doSync(HarnessCodex)
	got = r.read(d.MCPFile)
	if !strings.Contains(got, `D:\\x\\orole-daemon.exe`) || strings.Contains(got, `C:\\Orole`) || !strings.HasSuffix(got, "[after]\nk = 1\n") || strings.Count(got, "[mcp_servers.orole]") != 1 {
		t.Fatalf("update:\n%s", got)
	}
}

func TestCodexGatewayCRLF(t *testing.T) {
	r := newRig(t)
	d := r.def(HarnessCodex)
	r.writeFile(d.MCPFile, "a = 1\r\n")
	r.doSync(HarnessCodex)
	got := r.read(d.MCPFile)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("mixed line endings: %q", got)
	}
	if st := r.state(HarnessCodex); st.Gateway != GatewayInstalled {
		t.Fatalf("crlf block not recognised: %+v", st)
	}
}

func TestCodexGatewayRefusesConflicts(t *testing.T) {
	cases := map[string]string{
		"user table":      "[mcp_servers.orole]\ncommand = \"mine\"\n",
		"user subtable":   "[mcp_servers.orole.env]\nX = \"1\"\n",
		"quoted table":    "[mcp_servers.\"orole\"]\ncommand = \"mine\"\n",
		"inside parent":   "[mcp_servers]\norole = { command = \"mine\" }\n",
		"root dotted":     "mcp_servers.orole.command = \"mine\"\n",
		"lone begin":      tomlBegin + "\n[mcp_servers.orole]\n",
		"lone end":        "x = 1\n" + tomlEnd + "\n",
		"duplicate block": tomlBegin + "\n" + tomlEnd + "\n" + tomlBegin + "\n" + tomlEnd + "\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			d := r.def(HarnessCodex)
			r.writeFile(d.MCPFile, content)
			st := r.doSync(HarnessCodex)
			if st.Error == "" {
				t.Fatalf("conflict accepted: %+v", st)
			}
			if r.read(d.MCPFile) != content {
				t.Fatalf("file modified:\n%s", r.read(d.MCPFile))
			}
		})
	}
	// Other servers named like orole do not count.
	r := newRig(t)
	d := r.def(HarnessCodex)
	r.writeFile(d.MCPFile, "[mcp_servers.orole-other]\ncommand = \"x\"\n[mcp_servers]\nzorole = 1\n")
	if st := r.doSync(HarnessCodex); st.Gateway != GatewayInstalled || st.Error != "" {
		t.Fatalf("false positive: %+v", st)
	}
}

func TestGatewayUnsupportedWithoutExecutable(t *testing.T) {
	r := newRig(t)
	r.sync.GatewayExe = ""
	for _, st := range func() []SyncState { s, _ := r.sync.Sync(""); return s }() {
		if st.Gateway != GatewayUnsupported {
			t.Fatalf("%s gateway = %q", st.Harness, st.Gateway)
		}
	}
	if _, err := os.Stat(r.def(HarnessClaude).MCPFile); err == nil {
		t.Fatal("config written without a gateway executable")
	}
}

func TestEnvOverridesLocations(t *testing.T) {
	env := map[string]string{"CLAUDE_CONFIG_DIR": "/cfg/claude", "CODEX_HOME": "/cfg/codex", "PI_CODING_AGENT_DIR": "/cfg/omp"}
	l := NewLocations("/home/u", func(k string) string { return env[k] })
	c, _ := l.def(HarnessClaude)
	x, _ := l.def(HarnessCodex)
	o, _ := l.def(HarnessOmp)
	for got, want := range map[string]string{
		c.SyncDir: filepath.FromSlash("/cfg/claude/skills"), c.MCPFile: filepath.FromSlash("/cfg/claude/.claude.json"),
		x.MCPFile: filepath.FromSlash("/cfg/codex/config.toml"), x.ScanDirs[1]: filepath.FromSlash("/cfg/codex/skills"), x.SyncDir: filepath.FromSlash("/home/u/.agents/skills"),
		o.SyncDir: filepath.FromSlash("/cfg/omp/skills"), o.MCPFile: filepath.FromSlash("/cfg/omp/mcp.json"),
	} {
		if got != want {
			t.Errorf("got %s want %s", got, want)
		}
	}
}

// linkDir makes link point at target: a symlink where permitted, else (Windows) a junction.
func linkDir(target, link string) error {
	err := os.Symlink(target, link)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
}

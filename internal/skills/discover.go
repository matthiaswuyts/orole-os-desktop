package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Harness identifiers used in SyncState, Discovered and the IPC methods.
const (
	HarnessClaude = "claude"
	HarnessCodex  = "codex"
	HarnessOmp    = "omp"
)

// harnessDef says where one harness keeps its skills and its MCP configuration.
type harnessDef struct {
	ID, Name  string
	Binary    string   // executable looked up on PATH
	ConfigDir string   // exists once the harness has run
	SyncDir   string   // native skills directory Orole writes into
	ScanDirs  []string // directories Discover and Import read (SyncDir included)
	MCPFile   string   // native MCP configuration
	MCPTOML   bool     // MCPFile is Codex's config.toml (managed block) instead of a JSON mcpServers file
}

// Locations resolves every harness's directories from a home directory and the harness's own
// environment overrides. Tests pass a temp home and a fake env.
type Locations struct {
	Home string
	Defs []harnessDef
}

// DefaultLocations resolves the real locations of the current user.
func DefaultLocations() (Locations, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Locations{}, err
	}
	return NewLocations(home, os.Getenv), nil
}

// NewLocations resolves locations below home. env supplies CLAUDE_CONFIG_DIR, CODEX_HOME and
// PI_CODING_AGENT_DIR (the variables the harnesses themselves honour); nil means none are set.
func NewLocations(home string, env func(string) string) Locations {
	if env == nil {
		env = func(string) string { return "" }
	}
	or := func(v, def string) string {
		if v != "" {
			return v
		}
		return def
	}
	claudeDir := or(env("CLAUDE_CONFIG_DIR"), filepath.Join(home, ".claude"))
	claudeMCP := filepath.Join(home, ".claude.json")
	if env("CLAUDE_CONFIG_DIR") != "" {
		claudeMCP = filepath.Join(claudeDir, ".claude.json")
	}
	codexHome := or(env("CODEX_HOME"), filepath.Join(home, ".codex"))
	agentsSkills := filepath.Join(home, ".agents", "skills")
	ompDir := or(env("PI_CODING_AGENT_DIR"), filepath.Join(home, ".omp", "agent"))
	return Locations{Home: home, Defs: []harnessDef{
		{ID: HarnessClaude, Name: "Claude Code", Binary: "claude", ConfigDir: claudeDir,
			SyncDir: filepath.Join(claudeDir, "skills"), ScanDirs: []string{filepath.Join(claudeDir, "skills")}, MCPFile: claudeMCP},
		{ID: HarnessCodex, Name: "Codex", Binary: "codex", ConfigDir: codexHome,
			SyncDir: agentsSkills, ScanDirs: []string{agentsSkills, filepath.Join(codexHome, "skills")},
			MCPFile: filepath.Join(codexHome, "config.toml"), MCPTOML: true},
		{ID: HarnessOmp, Name: "Oh My Pi", Binary: "omp", ConfigDir: ompDir,
			SyncDir: filepath.Join(ompDir, "skills"), ScanDirs: []string{filepath.Join(ompDir, "skills")},
			MCPFile: filepath.Join(ompDir, "mcp.json")},
	}}
}

func (l Locations) def(id string) (harnessDef, bool) {
	for _, d := range l.Defs {
		if d.ID == id {
			return d, true
		}
	}
	return harnessDef{}, false
}

// installed is the default install probe: the CLI is on PATH or its config directory exists.
func (d harnessDef) installed() bool {
	if _, err := exec.LookPath(d.Binary); err == nil {
		return true
	}
	fi, err := os.Stat(d.ConfigDir)
	return err == nil && fi.IsDir()
}

// scanEntry is one candidate skill directory found in a harness's skills directories.
type scanEntry struct {
	dirName string
	path    string // symlink-resolved directory holding SKILL.md
	meta    Meta
}

// scan lists skill directories of a harness that Orole did not create itself. A top-level skill
// directory may itself be a link (skills installers do this); links inside a skill are never
// followed (see listFiles).
func (d harnessDef) scan() []scanEntry {
	var out []scanEntry
	seen := map[string]bool{}
	for _, dir := range d.ScanDirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := e.Name()
			if strings.HasPrefix(name, ".") || seen[name] {
				continue
			}
			p := filepath.Join(dir, name)
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				continue
			}
			if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
				continue
			}
			if _, ok := isRegular(filepath.Join(real, skillFile)); !ok {
				continue
			}
			if readMarker(filepath.Join(p, markerFile), name) { // Orole's own copy
				continue
			}
			b, err := readCapped(filepath.Join(real, skillFile), MaxSkillMD)
			if err != nil {
				continue
			}
			meta, _, _ := ParseFrontmatter(string(b))
			seen[name] = true
			out = append(out, scanEntry{dirName: name, path: real, meta: meta})
		}
	}
	return out
}

func (e scanEntry) registryName() string {
	if e.meta.Name != "" && ValidName(e.meta.Name) == nil {
		return e.meta.Name
	}
	return e.dirName
}

// Discover lists skills found in harness skill directories that the registry does not hold.
// Skills Orole synced itself (marked) and skills whose name cannot be a registry slug are left out.
func Discover(s *Store, loc Locations) ([]Discovered, error) {
	held := map[string]bool{}
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, sk := range list {
		held[sk.Name] = true
	}
	out := []Discovered{}
	for _, d := range loc.Defs {
		for _, e := range d.scan() {
			n := e.registryName()
			if held[n] || ValidName(n) != nil {
				continue
			}
			out = append(out, Discovered{Harness: d.ID, Name: e.dirName, Description: e.meta.Description, Path: e.path})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Harness != out[j].Harness {
			return out[i].Harness < out[j].Harness
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Import copies a discovered skill (SKILL.md and its resource files) into the registry. The
// harness's directory is only read. An existing registry skill is never overwritten.
func Import(s *Store, loc Locations, harness, name string) (Skill, error) {
	d, ok := loc.def(harness)
	if !ok {
		return Skill{}, invalidf("unknown harness %q", harness)
	}
	var found *scanEntry
	for _, e := range d.scan() {
		if e.dirName == name { // exact match against listed entries: name is never joined into a path
			e := e
			found = &e
			break
		}
	}
	if found == nil {
		return Skill{}, fmt.Errorf("%w: %s has no skill %q", ErrNotFound, d.Name, name)
	}
	tmp, err := os.MkdirTemp(s.root, ".import-")
	if err != nil {
		return Skill{}, err
	}
	defer os.RemoveAll(tmp)
	if err := copyTree(found.path, tmp); err != nil {
		return Skill{}, err
	}
	b, err := readCapped(filepath.Join(tmp, skillFile), MaxSkillMD)
	if err != nil {
		return Skill{}, err
	}
	final, err := checkBody(found.dirName, string(b))
	if err != nil {
		return Skill{}, err
	}
	return s.adopt(tmp, final, "imported:"+harness)
}

// adopt moves a prepared directory into the registry as a new skill.
func (s *Store) adopt(tmp, name, source string) (Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return Skill{}, err
	}
	dir := filepath.Join(s.root, name)
	if _, err := os.Lstat(dir); err == nil {
		return Skill{}, invalidf("%q is already in the registry", name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Skill{}, err
	}
	if len(doc.Skills) >= MaxSkills {
		return Skill{}, invalidf("the registry already holds %d skills", MaxSkills)
	}
	if err := os.Rename(tmp, dir); err != nil {
		return Skill{}, err
	}
	now := s.now().Unix()
	doc.Skills[name] = regEntry{Enabled: true, Source: source, UpdatedAt: now, AddedAt: now}
	if err := s.store(doc); err != nil {
		_ = os.RemoveAll(dir)
		return Skill{}, err
	}
	return s.build(name, doc, true)
}

package skills

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// markerFile marks a native skill directory Orole created. Orole only ever overwrites or removes
// directories that carry it; a same-named skill without it belongs to the user.
const markerFile = ".orole-managed"

type marker struct {
	ManagedBy string `json:"managedBy"`
	Name      string `json:"name"`
	Hash      string `json:"hash"`
}

// readMarker reports whether p is a valid Orole marker for the skill directory called name.
func readMarker(p, name string) bool {
	if _, ok := isRegular(p); !ok {
		return false
	}
	b, err := readCapped(p, 4<<10)
	if err != nil {
		return false
	}
	var m marker
	return json.Unmarshal(b, &m) == nil && m.ManagedBy == "orole" && m.Name == name
}

// Gateway states of SyncState.Gateway.
const (
	GatewayInstalled   = "installed"
	GatewayOutdated    = "outdated"
	GatewayMissing     = "missing"
	GatewayUnsupported = "unsupported"
)

// Syncer writes the registry into the harnesses' native directories and keeps the single "orole"
// MCP entry in their MCP configuration current.
type Syncer struct {
	Store *Store
	Loc   Locations
	// GatewayExe is the daemon executable the gateway entry launches as `<exe> mcp-gateway`
	// ("" = no gateway entry is managed; SyncState.Gateway is "unsupported").
	GatewayExe string
	// DataDir is passed to the gateway as OROLE_HOME.
	DataDir string
	// Installed overrides harness detection (nil = CLI on PATH or config directory present).
	Installed func(harness string) bool
}

func (y *Syncer) available(d harnessDef) bool {
	if y.Installed != nil {
		return y.Installed(d.ID)
	}
	return d.installed()
}

func (y *Syncer) defs(harness string) ([]harnessDef, error) {
	if harness == "" {
		return y.Loc.Defs, nil
	}
	d, ok := y.Loc.def(harness)
	if !ok {
		return nil, invalidf("unknown harness %q", harness)
	}
	return []harnessDef{d}, nil
}

type syncStatus int

const (
	stMissing syncStatus = iota
	stSynced
	stOutdated
	stConflict // a user-owned directory with the same name that differs from the registry
)

// classify compares one enabled registry skill with the harness's native copy. adopt is true for
// an identical copy that has no marker yet.
func classify(d harnessDef, sk Skill, regHash string) (st syncStatus, managed bool) {
	target := filepath.Join(d.SyncDir, sk.Name)
	fi, err := os.Lstat(target)
	if err != nil {
		return stMissing, false
	}
	if fi.Mode().Type() != fs.ModeDir {
		return stConflict, false // a file or link in the way: never write through it
	}
	managed = readMarker(filepath.Join(target, markerFile), sk.Name)
	h, err := HashTree(target)
	if err == nil && h == regHash {
		return stSynced, managed
	}
	if managed {
		return stOutdated, true
	}
	return stConflict, false
}

// State reports, per harness ("" = all), which enabled skills are present and current and the
// state of the gateway entry. It writes nothing.
func (y *Syncer) State(harness string) ([]SyncState, error) {
	defs, err := y.defs(harness)
	if err != nil {
		return nil, err
	}
	enabled, err := y.Store.EnabledForHarnesses()
	if err != nil {
		return nil, err
	}
	out := make([]SyncState, 0, len(defs))
	for _, d := range defs {
		st, _ := y.state(d, enabled)
		out = append(out, st)
	}
	return out, nil
}

func (y *Syncer) state(d harnessDef, enabled []Skill) (SyncState, []string) {
	st := SyncState{Harness: d.ID, Name: d.Name, Available: y.available(d), Dir: d.SyncDir,
		Synced: []string{}, Outdated: []string{}, Missing: []string{}, Gateway: GatewayMissing}
	var conflicts []string
	if y.GatewayExe == "" {
		st.Gateway = GatewayUnsupported
	}
	if !st.Available {
		return st, nil
	}
	for _, sk := range enabled {
		dir, err := y.Store.Dir(sk.Name)
		if err != nil {
			continue
		}
		h, err := HashTree(dir)
		if err != nil {
			st.Error = joinErr(st.Error, fmt.Sprintf("%s: %v", sk.Name, err))
			continue
		}
		switch s, _ := classify(d, sk, h); s {
		case stSynced:
			st.Synced = append(st.Synced, sk.Name)
		case stOutdated:
			st.Outdated = append(st.Outdated, sk.Name)
		case stConflict:
			conflicts = append(conflicts, sk.Name)
			st.Missing = append(st.Missing, sk.Name)
		default:
			st.Missing = append(st.Missing, sk.Name)
		}
	}
	if len(conflicts) > 0 {
		st.Error = joinErr(st.Error, fmt.Sprintf("left untouched, you already have a different skill with the same name: %s (import it to manage it with Orole)", strings.Join(conflicts, ", ")))
	}
	if y.GatewayExe != "" {
		g, err := y.gatewayState(d)
		st.Gateway = g
		if err != nil {
			st.Error = joinErr(st.Error, err.Error())
		}
	}
	return st, conflicts
}

func joinErr(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// Sync writes the enabled registry skills into the harness's native skills directory ("" = every
// installed harness), removes the copies Orole made of skills that are gone or disabled, and
// installs or updates the "orole" MCP entry. It returns the resulting states.
func (y *Syncer) Sync(harness string) ([]SyncState, error) {
	defs, err := y.defs(harness)
	if err != nil {
		return nil, err
	}
	enabled, err := y.Store.EnabledForHarnesses()
	if err != nil {
		return nil, err
	}
	out := make([]SyncState, 0, len(defs))
	for _, d := range defs {
		if !y.available(d) {
			st, _ := y.state(d, enabled)
			if harness != "" {
				st.Error = d.Name + " is not installed"
			}
			out = append(out, st)
			continue
		}
		var problems []string
		if err := y.syncSkills(d, enabled); err != nil {
			problems = append(problems, err.Error())
		}
		if y.GatewayExe != "" {
			if err := y.installGateway(d); err != nil {
				problems = append(problems, err.Error())
			}
		}
		st, _ := y.state(d, enabled)
		for _, p := range problems {
			st.Error = joinErr(st.Error, p)
		}
		out = append(out, st)
	}
	return out, nil
}

func (y *Syncer) syncSkills(d harnessDef, enabled []Skill) error {
	if fi, err := os.Lstat(d.SyncDir); err == nil && fi.Mode().Type() != fs.ModeDir {
		return fmt.Errorf("%s is a link or file; Orole does not write skills through it", d.SyncDir)
	}
	if err := os.MkdirAll(d.SyncDir, 0o755); err != nil {
		return err
	}
	want := map[string]bool{}
	var errs []string
	for _, sk := range enabled {
		want[sk.Name] = true
		dir, err := y.Store.Dir(sk.Name)
		if err != nil {
			continue
		}
		h, err := HashTree(dir)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", sk.Name, err))
			continue
		}
		st, managed := classify(d, sk, h)
		target := filepath.Join(d.SyncDir, sk.Name)
		switch {
		case st == stSynced && managed, st == stConflict:
		case st == stSynced: // identical user copy (usually just imported): take ownership
			if err := writeMarker(target, sk.Name, h); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", sk.Name, err))
			}
		default:
			if err := installSkill(d, dir, sk.Name, h); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", sk.Name, err))
			}
		}
	}
	// Remove what Orole created and no longer wants. Only marked real directories qualify.
	ents, err := os.ReadDir(d.SyncDir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		n := e.Name()
		if want[n] || strings.HasPrefix(n, ".") || e.Type() != fs.ModeDir {
			continue
		}
		p := filepath.Join(d.SyncDir, n)
		if readMarker(filepath.Join(p, markerFile), n) {
			if err := os.RemoveAll(p); err != nil {
				errs = append(errs, fmt.Sprintf("remove %s: %v", n, err))
			}
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func writeMarker(dir, name, hash string) error {
	b, err := json.Marshal(marker{ManagedBy: "orole", Name: name, Hash: hash})
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, markerFile), b, 0o644)
}

// installSkill copies the registry skill into a hidden sibling directory, marks it, and swaps it
// into place, so the native directory is never seen half-written.
func installSkill(d harnessDef, regDir, name, hash string) error {
	fresh := filepath.Join(d.SyncDir, ".orole-new-"+name)
	_ = os.RemoveAll(fresh)
	_ = os.RemoveAll(fresh + ".old")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(fresh)
		}
	}()
	if err := copyTree(regDir, fresh); err != nil {
		return err
	}
	if err := writeMarker(fresh, name, hash); err != nil {
		return err
	}
	if err := replaceDir(fresh, filepath.Join(d.SyncDir, name)); err != nil {
		return err
	}
	ok = true
	return nil
}

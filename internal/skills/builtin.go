package skills

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// SourceBuiltin marks the skills that ship with Orole. They are for Chat bots only: every bot can
// read them, and they are never synced to harness directories or served by the MCP gateway.
const SourceBuiltin = "builtin"

//go:embed all:builtin
var builtinFS embed.FS

// EnsureBuiltin installs the embedded skills into the registry and upgrades them when a newer
// version ships. It never overrides a decision of the user:
//
//   - a skill the user deleted stays deleted (the registry remembers every name it installed);
//   - a skill the user edited is left alone, because its content no longer matches the hash that
//     was recorded at install time;
//   - a skill of the same name from another source is not touched.
//
// Disabling a built-in skill is the supported way to switch it off.
func (s *Store) EnsureBuiltin() error {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return err
	}
	var errs []error
	changed := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		did, err := s.ensureOne(&doc, e.Name())
		if err != nil {
			errs = append(errs, fmt.Errorf("built-in skill %s: %w", e.Name(), err))
		}
		changed = changed || did
	}
	if changed {
		if err := s.store(doc); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ensureOne installs or upgrades one embedded skill and reports whether the registry document changed.
func (s *Store) ensureOne(doc *regDoc, name string) (bool, error) {
	if err := ValidName(name); err != nil {
		return false, err
	}
	dir := filepath.Join(s.root, name)
	seeded, known := doc.Builtin[name]
	entry, registered := doc.Skills[name]
	present := isRealDir(dir)
	switch {
	case !present && known:
		return false, nil // deleted by the user
	case present && (!registered || entry.Source != SourceBuiltin):
		return false, nil // a skill of the same name from somewhere else
	case !present && len(doc.Skills) >= MaxSkills:
		return false, invalidf("the registry already holds %d skills", MaxSkills)
	}

	fresh, err := os.MkdirTemp(s.root, ".builtin-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(fresh)
	if err := writeEmbedded(path.Join("builtin", name), fresh); err != nil {
		return false, err
	}
	want, err := HashTree(fresh)
	if err != nil {
		return false, err
	}
	if present {
		have, err := HashTree(dir)
		if err != nil {
			return false, err
		}
		// Upgrade only a copy that is exactly what was installed, and only when a newer one ships.
		if !known || have != seeded || have == want {
			return false, nil
		}
	}
	if err := replaceDir(fresh, dir); err != nil {
		return false, err
	}
	doc.Skills[name] = regEntry{Enabled: !present || entry.Enabled, Source: SourceBuiltin, UpdatedAt: s.now().Unix()}
	doc.Builtin[name] = want
	return true, nil
}

// writeEmbedded copies the embedded directory src into the existing directory dst.
func writeEmbedded(src, dst string) error {
	return fs.WalkDir(builtinFS, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := builtinFS.ReadFile(p)
		if err != nil {
			return err
		}
		rel := p[len(src)+1:]
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return writeFileAtomic(target, b, 0o600)
	})
}

// EnabledForHarnesses is Enabled without the built-in skills: what coding agents may see.
func (s *Store) EnabledForHarnesses() ([]Skill, error) {
	all, err := s.Enabled()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, sk := range all {
		if sk.Source != SourceBuiltin {
			out = append(out, sk)
		}
	}
	return out, nil
}

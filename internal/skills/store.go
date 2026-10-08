package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Size caps. Skills are text; anything bigger is almost certainly the wrong directory.
const (
	MaxSkillMD     = 256 << 10 // SKILL.md
	MaxFileSize    = 1 << 20   // one resource file
	MaxFiles       = 200       // files per skill, SKILL.md included
	MaxTreeBytes   = 8 << 20   // whole skill
	MaxDepth       = 6         // directory depth below the skill root
	MaxSkills      = 500       // registry entries
	MaxDescription = 1024
	MaxNameLen     = 64

	skillFile    = "SKILL.md"
	registryFile = ".registry.json"
)

var (
	// ErrNotFound reports a skill (or a file inside one) that does not exist.
	ErrNotFound = errors.New("skill not found")
	// ErrInvalid reports input that is not an acceptable skill.
	ErrInvalid = errors.New("invalid skill")
)

func invalidf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedNames are Windows device names; they cannot be file or directory names there.
var reservedNames = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

func isReserved(name string) bool {
	base := strings.ToLower(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if reservedNames[base] {
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9'
}

// ValidName reports whether name is an acceptable skill slug: lowercase letters, digits and
// single hyphens, at most 64 characters, never a Windows device name.
func ValidName(name string) error {
	switch {
	case name == "":
		return invalidf("name is required")
	case len(name) > MaxNameLen:
		return invalidf("name %q is longer than %d characters", name, MaxNameLen)
	case !slugRE.MatchString(name):
		return invalidf("name %q must be lowercase letters, digits and single hyphens (for example my-skill)", name)
	case isReserved(name):
		return invalidf("name %q is reserved on Windows", name)
	}
	return nil
}

// cleanRel validates a resource path inside a skill and returns it slash-separated. Absolute
// paths, "..", drive letters, backslashes, hidden or device-named components are rejected, so a
// path that survives can only name a file below the skill directory.
func cleanRel(rel string) (string, error) {
	if rel == "" || len(rel) > 240 || strings.ContainsAny(rel, "\\\x00:") || strings.HasPrefix(rel, "/") {
		return "", invalidf("file path %q is not a relative path inside the skill", rel)
	}
	parts := strings.Split(rel, "/")
	if len(parts) > MaxDepth+1 {
		return "", invalidf("file path %q is nested too deeply", rel)
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") || strings.ContainsAny(p, "*?\"<>|") ||
			strings.HasSuffix(p, " ") || strings.HasSuffix(p, ".") || isReserved(p) || !utf8.ValidString(p) {
			return "", invalidf("file path %q is not a relative path inside the skill", rel)
		}
		for _, r := range p {
			if r < 0x20 {
				return "", invalidf("file path %q contains control characters", rel)
			}
		}
	}
	return path.Join(parts...), nil
}

type regEntry struct {
	Enabled   bool   `json:"enabled"`
	Source    string `json:"source"`
	UpdatedAt int64  `json:"updatedAt"`
}

type regDoc struct {
	Version int                 `json:"version"`
	Skills  map[string]regEntry `json:"skills"`
}

// Store is the registry on disk: <dir>/<name>/SKILL.md plus resource files, and a small
// sidecar (.registry.json) with each skill's enabled flag and source. Safe for concurrent use.
type Store struct {
	root string
	mu   sync.Mutex
	now  func() time.Time
}

// NewStore opens (creating) the registry under <dataDir>/skills.
func NewStore(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "skills")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root, now: time.Now}, nil
}

// Root returns the registry directory.
func (s *Store) Root() string { return s.root }

// Dir returns the directory of a skill after validating its name.
func (s *Store) Dir(name string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.root, name), nil
}

func (s *Store) load() (regDoc, error) {
	doc := regDoc{Version: 1, Skills: map[string]regEntry{}}
	b, err := os.ReadFile(filepath.Join(s.root, registryFile))
	if errors.Is(err, fs.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return doc, fmt.Errorf("skills registry %s is corrupt: %w", filepath.Join(s.root, registryFile), err)
	}
	if doc.Skills == nil {
		doc.Skills = map[string]regEntry{}
	}
	return doc, nil
}

func (s *Store) store(doc regDoc) error {
	doc.Version = 1
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.root, registryFile), b, 0o600)
}

// isRealDir reports whether p is a plain directory (not a symlink, junction or other reparse point).
func isRealDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().Type() == fs.ModeDir
}

func isRegular(p string) (fs.FileInfo, bool) {
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, false
	}
	return fi, true
}

// names lists registry directories that look like skills.
func (s *Store) names() ([]string, error) {
	ents, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.Type() != fs.ModeDir || ValidName(n) != nil {
			continue
		}
		if _, ok := isRegular(filepath.Join(s.root, n, skillFile)); ok {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) build(name string, doc regDoc, withBody bool) (Skill, error) {
	dir := filepath.Join(s.root, name)
	fi, ok := isRegular(filepath.Join(dir, skillFile))
	if !ok {
		return Skill{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	b, err := readCapped(filepath.Join(dir, skillFile), MaxSkillMD)
	if err != nil {
		return Skill{}, err
	}
	meta, _, _ := ParseFrontmatter(string(b))
	files, _ := listFiles(dir, false)
	rels := make([]string, 0, len(files))
	for _, f := range files {
		rels = append(rels, f.rel)
	}
	e, has := doc.Skills[name]
	if !has {
		e = regEntry{Enabled: true, Source: "orole", UpdatedAt: fi.ModTime().Unix()}
	}
	sk := Skill{Name: name, Description: meta.Description, Files: rels, Enabled: e.Enabled, Source: e.Source, UpdatedAt: e.UpdatedAt}
	if sk.Source == "" {
		sk.Source = "orole"
	}
	if withBody {
		sk.Body = string(b)
	}
	return sk, nil
}

// List returns every registry skill, name-sorted. Bodies are omitted (use Get).
func (s *Store) List() ([]Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(false)
}

func (s *Store) listLocked(withBody bool) ([]Skill, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	names, err := s.names()
	if err != nil {
		return nil, err
	}
	out := make([]Skill, 0, len(names))
	for _, n := range names {
		sk, err := s.build(n, doc, withBody)
		if err != nil {
			continue // vanished or unreadable between listing and reading
		}
		out = append(out, sk)
	}
	return out, nil
}

// Enabled returns the enabled skills with bodies, name-sorted.
func (s *Store) Enabled() ([]Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.listLocked(true)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, sk := range all {
		if sk.Enabled {
			out = append(out, sk)
		}
	}
	return out, nil
}

// Get returns one skill with its full SKILL.md.
func (s *Store) Get(name string) (Skill, error) {
	if err := ValidName(name); err != nil {
		return Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return Skill{}, err
	}
	return s.build(name, doc, true)
}

// Save creates or replaces a skill from SKILL.md text. The frontmatter name wins over name.
// source is recorded for new skills ("" = "orole"); an existing skill keeps its source.
// Resource files already present next to SKILL.md are kept.
func (s *Store) Save(name, body, source string) (Skill, error) {
	final, err := checkBody(name, body)
	if err != nil {
		return Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return Skill{}, err
	}
	dir := filepath.Join(s.root, final)
	if _, exists := doc.Skills[final]; !exists && !isRealDir(dir) && len(doc.Skills) >= MaxSkills {
		return Skill{}, invalidf("the registry already holds %d skills", MaxSkills)
	}
	if fi, err := os.Lstat(dir); err == nil && fi.Mode().Type() != fs.ModeDir {
		return Skill{}, invalidf("%s exists in the registry but is not a skill directory", final)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Skill{}, err
	}
	if err := writeFileAtomic(filepath.Join(dir, skillFile), []byte(body), 0o600); err != nil {
		return Skill{}, err
	}
	e, has := doc.Skills[final]
	if !has {
		e = regEntry{Enabled: true, Source: source}
	}
	if e.Source == "" {
		e.Source = "orole"
	}
	e.UpdatedAt = s.now().Unix()
	doc.Skills[final] = e
	if err := s.store(doc); err != nil {
		return Skill{}, err
	}
	return s.build(final, doc, true)
}

// checkBody validates SKILL.md text and returns the resolved skill name.
func checkBody(name, body string) (string, error) {
	if len(body) > MaxSkillMD {
		return "", invalidf("SKILL.md is larger than %d KiB", MaxSkillMD>>10)
	}
	if !utf8.ValidString(body) || strings.IndexByte(body, 0) >= 0 {
		return "", invalidf("SKILL.md must be UTF-8 text")
	}
	meta, _, err := ParseFrontmatter(body)
	if err != nil {
		return "", invalidf("%v", err)
	}
	final := meta.Name
	if final == "" {
		final = name
	}
	if err := ValidName(final); err != nil {
		return "", err
	}
	if meta.Description == "" {
		return "", invalidf("frontmatter needs a description")
	}
	if utf8.RuneCountInString(meta.Description) > MaxDescription {
		return "", invalidf("description is longer than %d characters", MaxDescription)
	}
	return final, nil
}

// Delete removes a skill and its resource files from the registry.
func (s *Store) Delete(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return err
	}
	dir := filepath.Join(s.root, name)
	if !isRealDir(dir) {
		if _, ok := doc.Skills[name]; !ok {
			return fmt.Errorf("%w: %s", ErrNotFound, name)
		}
	} else if err := os.RemoveAll(dir); err != nil { // RemoveAll never follows links
		return err
	}
	delete(doc.Skills, name)
	return s.store(doc)
}

// SetEnabled turns a skill on or off.
func (s *Store) SetEnabled(name string, enabled bool) (Skill, error) {
	if err := ValidName(name); err != nil {
		return Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return Skill{}, err
	}
	sk, err := s.build(name, doc, false)
	if err != nil {
		return Skill{}, err
	}
	e := doc.Skills[name]
	if _, has := doc.Skills[name]; !has {
		e = regEntry{Source: sk.Source, UpdatedAt: sk.UpdatedAt}
	}
	e.Enabled = enabled
	doc.Skills[name] = e
	if err := s.store(doc); err != nil {
		return Skill{}, err
	}
	return s.build(name, doc, true)
}

// ReadFile returns SKILL.md (rel "" or "SKILL.md") or a resource file of a skill. Every path
// component must be a plain file or directory inside the skill; links are refused.
func (s *Store) ReadFile(name, rel string) ([]byte, error) {
	dir, err := s.Dir(name)
	if err != nil {
		return nil, err
	}
	if rel == "" || rel == skillFile {
		b, err := readCapped(filepath.Join(dir, skillFile), MaxSkillMD)
		return b, notFound(err, name)
	}
	clean, err := cleanRel(rel)
	if err != nil {
		return nil, err
	}
	cur := dir
	parts := strings.Split(clean, "/")
	for i, p := range parts {
		cur = filepath.Join(cur, p)
		fi, err := os.Lstat(cur)
		if err != nil {
			return nil, notFound(err, name+"/"+clean)
		}
		last := i == len(parts)-1
		if (last && !fi.Mode().IsRegular()) || (!last && fi.Mode().Type() != fs.ModeDir) {
			return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, name, clean)
		}
	}
	b, err := readCapped(cur, MaxFileSize)
	return b, notFound(err, name+"/"+clean)
}

func notFound(err error, what string) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, what)
	}
	return err
}

// readCapped reads a regular file of at most max bytes.
func readCapped(p string, max int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, invalidf("%s is not a regular file", filepath.Base(p))
	}
	if fi.Size() > max {
		return nil, invalidf("%s is larger than %d KiB", filepath.Base(p), max>>10)
	}
	return io.ReadAll(io.LimitReader(f, max+1))
}

// PromptIndex builds the short list of enabled skills appended to a harness's system prompt.
// It carries names and descriptions only; the instructions come from orole_get_skill.
func (s *Store) PromptIndex() string {
	list, err := s.Enabled()
	if err != nil || len(list) == 0 {
		return ""
	}
	const maxLines, maxDesc = 60, 300
	var b strings.Builder
	b.WriteString("## Orole skills\n\nSkills are shared across every agent through the \"orole\" MCP server. ")
	b.WriteString("When a task matches a skill below, call the `orole_get_skill` tool with its name and follow the returned instructions before you start; ")
	b.WriteString("`orole_list_skills` lists them again.\n\n")
	for i, sk := range list {
		if i == maxLines {
			fmt.Fprintf(&b, "- ... and %d more (call `orole_list_skills`)\n", len(list)-maxLines)
			break
		}
		fmt.Fprintf(&b, "- %s: %s\n", sk.Name, oneLine(sk.Description, maxDesc))
	}
	return b.String()
}

// oneLine collapses whitespace, drops control characters and truncates on a rune boundary.
func oneLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// ---- tree helpers shared by import and sync ----

type fileEntry struct {
	rel  string // slash-separated, relative to the skill root
	size int64
}

// listFiles lists the plain files of a skill directory (links, hidden entries and anything
// deeper than MaxDepth are skipped), sorted by path. includeRoot adds SKILL.md. It fails when
// the caps are exceeded.
func listFiles(dir string, includeRoot bool) ([]fileEntry, error) {
	var out []fileEntry
	var total int64
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Type() != fs.ModeDir || strings.Count(rel, "/") >= MaxDepth {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if rel == skillFile && !includeRoot {
			return nil
		}
		if _, cerr := cleanRel(rel); cerr != nil && rel != skillFile {
			return nil // a name we would never serve or write
		}
		fi, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if fi.Size() > MaxFileSize {
			return invalidf("%s is larger than %d KiB", rel, MaxFileSize>>10)
		}
		total += fi.Size()
		if len(out) >= MaxFiles || total > MaxTreeBytes {
			return invalidf("skill has more than %d files or %d MiB", MaxFiles, MaxTreeBytes>>20)
		}
		out = append(out, fileEntry{rel: rel, size: fi.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

// HashTree returns a content hash over a skill directory (SKILL.md and resource files, no
// hidden files), used to tell whether a native copy matches the registry.
func HashTree(dir string) (string, error) {
	files, err := listFiles(dir, true)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, f := range files {
		b, err := readCapped(filepath.Join(dir, filepath.FromSlash(f.rel)), MaxFileSize)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(b)
		fmt.Fprintf(h, "%s\x00%s\n", f.rel, hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyTree copies the plain files of src (see listFiles) into the existing directory dst.
func copyTree(src, dst string) error {
	files, err := listFiles(src, true)
	if err != nil {
		return err
	}
	hasSkill := false
	for _, f := range files {
		hasSkill = hasSkill || f.rel == skillFile
	}
	if !hasSkill {
		return invalidf("%s has no %s", filepath.Base(src), skillFile)
	}
	for _, f := range files {
		b, err := readCapped(filepath.Join(src, filepath.FromSlash(f.rel)), MaxFileSize)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := writeFileAtomic(target, b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// writeFileAtomic writes data to a temp file beside p and renames it into place.
func writeFileAtomic(p string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(p), ".orole-tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	_ = f.Chmod(perm) // best effort: Windows ignores most mode bits
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	ok = true
	return nil
}

// replaceDir atomically swaps the directory dst for the directory fresh (same parent). The
// old directory is moved aside first and removed afterwards, so dst is never half-written.
func replaceDir(fresh, dst string) error {
	old := ""
	if _, err := os.Lstat(dst); err == nil {
		old = fresh + ".old"
		if err := os.Rename(dst, old); err != nil {
			return err
		}
	}
	if err := os.Rename(fresh, dst); err != nil {
		if old != "" {
			_ = os.Rename(old, dst) // restore
		}
		return err
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return nil
}

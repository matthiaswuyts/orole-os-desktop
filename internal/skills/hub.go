package skills

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

// The skills.sh hub: browse and search the public skills.sh catalogue and add a skill to the
// registry. skills.sh lists skills (owner/repo + skill id); the skill itself lives in a public
// GitHub repository, so installing downloads that repository's archive and copies one skill
// directory out of it.
//
// skills.sh's documented API (/api/v1) needs a Vercel OIDC token, which a desktop app cannot have.
// Browsing therefore uses the unauthenticated JSON endpoints the skills.sh site itself calls
// (/api/skills/<view>/<page>, /api/search). They are not a published contract: a change on the
// site surfaces as an error in Discover, never as wrong data, because every field is validated.
//
// Everything from skills.sh and GitHub is untrusted. Names are validated and sanitised, a skill is
// copied with the same caps and path rules as an import, links are never followed, and nothing is
// executed. A skill is still instructions an agent will follow, so Preview shows the complete
// SKILL.md and lists script files before anything is added.

const (
	// HubSourcePrefix marks registry skills added from skills.sh: "skills.sh:<owner>/<repo>/<skill>".
	HubSourcePrefix = "skills.sh:"

	hubMaxJSON      = 4 << 20
	hubMaxPage      = 1 << 20
	hubMaxArchive   = 128 << 20
	hubMaxCandidate = 400
	hubDescribeMax  = 40
	hubDescribeTTL  = time.Hour
	hubDescribeConc = 6
)

var (
	hubPartRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	hubSourceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	hubMetaRE   = regexp.MustCompile(`(?is)<meta\s+name="description"\s+content="([^"]*)"`)

	// ErrHub reports a failure talking to skills.sh or GitHub.
	ErrHub = errors.New("skills.sh unavailable")

	scriptExt = map[string]bool{
		".sh": true, ".bash": true, ".zsh": true, ".ps1": true, ".bat": true, ".cmd": true, ".py": true, ".js": true,
		".mjs": true, ".cjs": true, ".ts": true, ".rb": true, ".pl": true, ".php": true, ".exe": true, ".dll": true,
	}
)

// HubEntry is one skills.sh catalogue row.
type HubEntry struct {
	Source   string `json:"source"`  // GitHub "owner/repo"
	SkillID  string `json:"skillId"` // the skill's directory/name inside the repository
	Name     string `json:"name"`
	Installs int64  `json:"installs"`
	Official bool   `json:"official"`
}

// HubPage is one page of a listing.
type HubPage struct {
	Skills  []HubEntry `json:"skills"`
	Page    int        `json:"page"`
	HasMore bool       `json:"hasMore"`
	Total   int        `json:"total"`
}

// HubRef names a skill on skills.sh.
type HubRef struct {
	Source  string `json:"source"`
	SkillID string `json:"skillId"`
}

// HubDescription is the one-line description skills.sh shows for a skill ("" when unknown).
type HubDescription struct {
	Source      string `json:"source"`
	SkillID     string `json:"skillId"`
	Description string `json:"description"`
}

// HubPreview is what Add will copy into the registry, shown before anything is written.
type HubPreview struct {
	Source      string   `json:"source"`
	SkillID     string   `json:"skillId"`
	Name        string   `json:"name"` // registry name (frontmatter name wins)
	Description string   `json:"description"`
	Body        string   `json:"body"`    // full SKILL.md
	Files       []string `json:"files"`   // resource files next to SKILL.md
	Scripts     []string `json:"scripts"` // the subset of Files that are executable script types
	URL         string   `json:"url"`     // the skill's skills.sh page
}

// Hub talks to skills.sh and GitHub. The zero value is not usable; use NewHub.
type Hub struct {
	Client    *http.Client
	SiteURL   string // https://www.skills.sh
	GitHubURL string // https://github.com

	mu   sync.Mutex
	desc map[string]hubDesc
}

type hubDesc struct {
	text string
	at   time.Time
}

// NewHub returns a Hub for the public skills.sh and github.com.
func NewHub() *Hub {
	return &Hub{
		Client: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return errors.New("redirect to a non-https address refused")
			}
			return nil
		}},
		SiteURL:   "https://www.skills.sh",
		GitHubURL: "https://github.com",
		desc:      map[string]hubDesc{},
	}
}

func hubErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrHub, fmt.Sprintf(format, a...))
}

// ValidHubRef checks the two parts of a reference; they end up in URLs and a registry source.
func ValidHubRef(source, skillID string) error {
	if !hubSourceRE.MatchString(source) || strings.Contains(source, "..") {
		return invalidf("%q is not a GitHub owner/repo", untrusted.Line(source, 120))
	}
	if !hubPartRE.MatchString(skillID) || strings.Contains(skillID, "..") {
		return invalidf("%q is not a skill id", untrusted.Line(skillID, 120))
	}
	return nil
}

func (h *Hub) get(ctx context.Context, rawURL string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Orole-OS")
	req.Header.Set("Accept", "application/json, text/html;q=0.8, */*;q=0.5")
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, hubErr("%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, hubErr("%s answered %d", req.URL.Host, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, hubErr("%v", err)
	}
	if int64(len(b)) > max {
		return nil, hubErr("response from %s is larger than %d KiB", req.URL.Host, max>>10)
	}
	return b, nil
}

type hubRow struct {
	Source   string `json:"source"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int64  `json:"installs"`
	Official bool   `json:"isOfficial"`
}

func entries(rows []hubRow) []HubEntry {
	out := make([]HubEntry, 0, len(rows))
	for _, r := range rows {
		if ValidHubRef(r.Source, r.SkillID) != nil {
			continue // not a GitHub skill we could install; never shown
		}
		name := untrusted.Line(r.Name, 80)
		if name == "" {
			name = r.SkillID
		}
		out = append(out, HubEntry{Source: r.Source, SkillID: r.SkillID, Name: name, Installs: max(r.Installs, 0), Official: r.Official})
	}
	return out
}

// Browse lists one page of the catalogue. view is "all-time" or "trending"; pages start at 0.
func (h *Hub) Browse(ctx context.Context, view string, page int) (HubPage, error) {
	if view != "all-time" && view != "trending" {
		return HubPage{}, invalidf("unknown listing %q", view)
	}
	if page < 0 || page > 1000 {
		return HubPage{}, invalidf("page out of range")
	}
	b, err := h.get(ctx, fmt.Sprintf("%s/api/skills/%s/%d", h.SiteURL, view, page), hubMaxJSON)
	if err != nil {
		return HubPage{}, err
	}
	var doc struct {
		Skills  []hubRow `json:"skills"`
		HasMore bool     `json:"hasMore"`
		Total   int      `json:"total"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return HubPage{}, hubErr("unexpected listing format")
	}
	return HubPage{Skills: entries(doc.Skills), Page: page, HasMore: doc.HasMore, Total: doc.Total}, nil
}

// HubTopic is one of skills.sh's curated topics.
type HubTopic struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// hubTopics are the topics skills.sh publishes at /topic. There is no JSON API for them: the list is
// fixed here and a topic's skills are read from its page (see Topic).
var hubTopics = []HubTopic{
	{"agent-workflows", "Agent workflows"}, {"databases", "Databases"}, {"design", "Design & UI"}, {"marketing", "Marketing"},
	{"mobile", "Mobile"}, {"nextjs", "Next.js"}, {"react", "React"}, {"testing", "Testing"},
}

// HubTopics lists the topics Topic accepts.
func HubTopics() []HubTopic { return append([]HubTopic(nil), hubTopics...) }

var hubLinkRE = regexp.MustCompile(`href=\\?"/([A-Za-z0-9][A-Za-z0-9._-]*)/([A-Za-z0-9][A-Za-z0-9._-]*)/([A-Za-z0-9][A-Za-z0-9._-]*)\\?"`)

// Topic lists the skills of one topic. skills.sh has no API for topics, so this reads the links to
// skill pages from the topic's HTML page; counts are not on it (Installs is 0). If the page layout
// changes, the result is an empty list or an error, never wrong data: each link is validated.
func (h *Hub) Topic(ctx context.Context, id string) (HubPage, error) {
	known := false
	for _, t := range hubTopics {
		known = known || t.ID == id
	}
	if !known {
		return HubPage{}, invalidf("unknown topic %q", untrusted.Line(id, 60))
	}
	b, err := h.get(ctx, h.SiteURL+"/topic/"+id, 2<<20)
	if err != nil {
		return HubPage{}, err
	}
	seen := map[string]bool{}
	var rows []hubRow
	for _, m := range hubLinkRE.FindAllSubmatch(b, -1) {
		source, skillID := string(m[1])+"/"+string(m[2]), string(m[3])
		switch string(m[1]) {
		case "topic", "docs", "api", "packs", "official", "audits":
			continue
		}
		if seen[source+"/"+skillID] {
			continue
		}
		seen[source+"/"+skillID] = true
		rows = append(rows, hubRow{Source: source, SkillID: skillID, Name: skillID})
	}
	list := entries(rows)
	return HubPage{Skills: list, Total: len(list)}, nil
}

// Search finds skills by name. The query needs at least two characters.
func (h *Hub) Search(ctx context.Context, query string) (HubPage, error) {
	query = strings.Join(strings.Fields(query), " ")
	if n := len([]rune(query)); n < 2 || n > 100 {
		return HubPage{}, invalidf("search needs 2 to 100 characters")
	}
	b, err := h.get(ctx, h.SiteURL+"/api/search?limit=40&q="+url.QueryEscape(query), hubMaxJSON)
	if err != nil {
		return HubPage{}, err
	}
	var doc struct {
		Skills []hubRow `json:"skills"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return HubPage{}, hubErr("unexpected search format")
	}
	list := entries(doc.Skills)
	return HubPage{Skills: list, Total: len(list)}, nil
}

// Describe returns the descriptions skills.sh shows for up to 40 skills, fetched concurrently and
// cached for an hour. A skill whose description cannot be read comes back with "".
func (h *Hub) Describe(ctx context.Context, refs []HubRef) []HubDescription {
	if len(refs) > hubDescribeMax {
		refs = refs[:hubDescribeMax]
	}
	out := make([]HubDescription, len(refs))
	sem := make(chan struct{}, hubDescribeConc)
	var wg sync.WaitGroup
	for i, r := range refs {
		out[i] = HubDescription{Source: r.Source, SkillID: r.SkillID}
		if ValidHubRef(r.Source, r.SkillID) != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			out[i].Description = h.describeOne(ctx, r)
		}()
	}
	wg.Wait()
	return out
}

func (h *Hub) describeOne(ctx context.Context, r HubRef) string {
	key := r.Source + "/" + r.SkillID
	h.mu.Lock()
	c, ok := h.desc[key]
	h.mu.Unlock()
	if ok && time.Since(c.at) < hubDescribeTTL {
		return c.text
	}
	b, err := h.get(ctx, h.SiteURL+"/"+r.Source+"/"+r.SkillID, hubMaxPage)
	if err != nil {
		// A page can be larger than the cap; the description sits in <head>, so retry is pointless.
		return ""
	}
	m := hubMetaRE.FindSubmatch(b)
	if m == nil {
		return ""
	}
	text := untrusted.Line(strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " "), 300)
	h.mu.Lock()
	if len(h.desc) > 2000 {
		h.desc = map[string]hubDesc{}
	}
	h.desc[key] = hubDesc{text: text, at: time.Now()}
	h.mu.Unlock()
	return text
}

// hubSkill is one skill directory read out of a repository archive.
type hubSkill struct {
	body  string
	files map[string][]byte // resource files by slash-separated relative path
}

// fetch downloads the repository's archive and extracts the skill directory.
func (h *Hub) fetch(ctx context.Context, source, skillID string) (*hubSkill, error) {
	if err := ValidHubRef(source, skillID); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.GitHubURL+"/"+source+"/archive/HEAD.zip", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Orole-OS")
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, hubErr("%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s is not a public GitHub repository", ErrNotFound, source)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, hubErr("GitHub answered %d for %s", resp.StatusCode, source)
	}
	tmp, err := os.CreateTemp("", "orole-skill-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, hubMaxArchive+1))
	if err != nil {
		return nil, hubErr("%v", err)
	}
	if n > hubMaxArchive {
		return nil, invalidf("the repository archive is larger than %d MiB", hubMaxArchive>>20)
	}
	zr, err := zip.NewReader(tmp, n)
	if err != nil {
		return nil, hubErr("the repository archive is unreadable")
	}
	return extractSkill(zr, skillID)
}

// archivePath drops the archive's single top-level directory and rejects unsafe paths.
func archivePath(name string) (string, bool) {
	_, rest, ok := strings.Cut(name, "/")
	if !ok || rest == "" || strings.ContainsAny(rest, "\\\x00") || path.IsAbs(rest) {
		return "", false
	}
	for _, part := range strings.Split(rest, "/") {
		if part == ".." {
			return "", false
		}
	}
	return rest, true
}

func hiddenPath(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") || part == "node_modules" {
			return true
		}
	}
	return false
}

func readZipFile(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, invalidf("%s is larger than %d KiB", path.Base(f.Name), limit>>10)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1)) // the declared size can lie
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, invalidf("%s is larger than %d KiB", path.Base(f.Name), limit>>10)
	}
	return b, nil
}

// extractSkill finds the skill directory for skillID in an archive and reads its files. A
// directory named skillID wins; otherwise the SKILL.md whose frontmatter name is skillID.
func extractSkill(zr *zip.Reader, skillID string) (*hubSkill, error) {
	type cand struct {
		dir string
		f   *zip.File
	}
	var cands []cand
	for _, f := range zr.File {
		rel, ok := archivePath(f.Name)
		if !ok || f.Mode()&os.ModeType != 0 || path.Base(rel) != skillFile || hiddenPath(rel) {
			continue
		}
		dir := path.Dir(rel)
		if dir == "." {
			dir = ""
		}
		if cands = append(cands, cand{dir, f}); len(cands) > hubMaxCandidate {
			return nil, invalidf("the repository holds more than %d skills", hubMaxCandidate)
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		di, dj := strings.Count(cands[i].dir, "/"), strings.Count(cands[j].dir, "/")
		if di != dj {
			return di < dj
		}
		return cands[i].dir < cands[j].dir
	})
	var pick *cand
	for i := range cands {
		if path.Base(cands[i].dir) == skillID {
			pick = &cands[i]
			break
		}
	}
	if pick == nil {
		for i := range cands {
			b, err := readZipFile(cands[i].f, MaxSkillMD)
			if err != nil {
				continue
			}
			if meta, _, err := ParseFrontmatter(string(b)); err == nil && meta.Name == skillID {
				pick = &cands[i]
				break
			}
		}
	}
	if pick == nil {
		return nil, fmt.Errorf("%w: the repository has no skill %q", ErrNotFound, skillID)
	}

	prefix := ""
	if pick.dir != "" {
		prefix = pick.dir + "/"
	}
	sk := &hubSkill{files: map[string][]byte{}}
	var total int64
	count := 0
	for _, f := range zr.File {
		rel, ok := archivePath(f.Name)
		if !ok || f.Mode()&os.ModeType != 0 || !strings.HasPrefix(rel, prefix) {
			continue
		}
		inner := strings.TrimPrefix(rel, prefix)
		if inner == "" || strings.HasSuffix(f.Name, "/") {
			continue
		}
		if inner == skillFile {
			b, err := readZipFile(f, MaxSkillMD)
			if err != nil {
				return nil, err
			}
			sk.body = string(b)
			count++
			total += int64(len(b))
			continue
		}
		if hiddenPath(inner) || strings.Count(inner, "/") >= MaxDepth {
			continue
		}
		clean, err := cleanRel(inner)
		if err != nil || path.Base(clean) == skillFile {
			continue // unsafe names and nested skills are not copied
		}
		if count++; count > MaxFiles {
			return nil, invalidf("the skill has more than %d files", MaxFiles)
		}
		b, err := readZipFile(f, MaxFileSize)
		if err != nil {
			return nil, err
		}
		if total += int64(len(b)); total > MaxTreeBytes {
			return nil, invalidf("the skill is larger than %d MiB", MaxTreeBytes>>20)
		}
		sk.files[clean] = b
	}
	if sk.body == "" {
		return nil, invalidf("SKILL.md is empty")
	}
	return sk, nil
}

// Preview downloads a skill and reports exactly what Install would add. Nothing is written.
func (h *Hub) Preview(ctx context.Context, source, skillID string) (HubPreview, error) {
	sk, err := h.fetch(ctx, source, skillID)
	if err != nil {
		return HubPreview{}, err
	}
	name, err := checkBody(skillID, sk.body)
	if err != nil {
		return HubPreview{}, err
	}
	meta, _, _ := ParseFrontmatter(sk.body)
	p := HubPreview{
		Source: source, SkillID: skillID, Name: name, Description: meta.Description, Body: sk.body,
		Files: []string{}, Scripts: []string{}, URL: h.SiteURL + "/" + source + "/" + skillID,
	}
	for rel := range sk.files {
		p.Files = append(p.Files, rel)
		if scriptExt[strings.ToLower(path.Ext(rel))] {
			p.Scripts = append(p.Scripts, rel)
		}
	}
	sort.Strings(p.Files)
	sort.Strings(p.Scripts)
	return p, nil
}

// Install adds a skill to the registry, enabled, with source "skills.sh:<owner>/<repo>/<skill>".
// It refuses to replace a skill that is already there.
func (h *Hub) Install(ctx context.Context, s *Store, source, skillID string) (Skill, error) {
	sk, err := h.fetch(ctx, source, skillID)
	if err != nil {
		return Skill{}, err
	}
	final, err := checkBody(skillID, sk.body)
	if err != nil {
		return Skill{}, err
	}
	tmp, err := os.MkdirTemp(s.root, ".import-")
	if err != nil {
		return Skill{}, err
	}
	defer os.RemoveAll(tmp)
	if err := writeFileAtomic(filepath.Join(tmp, skillFile), []byte(sk.body), 0o600); err != nil {
		return Skill{}, err
	}
	for rel, data := range sk.files {
		dst := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return Skill{}, err
		}
		if err := writeFileAtomic(dst, data, 0o600); err != nil {
			return Skill{}, err
		}
	}
	return s.adopt(tmp, final, HubSourcePrefix+source+"/"+skillID)
}

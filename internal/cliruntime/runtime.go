package cliruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Status is what the UI shows for one provider.
type Status struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Installed is true when a CLI runs; Path and Source say which one.
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Source    string `json:"source,omitempty"`
	Version   string `json:"version,omitempty"`
	// Latest is the newest release on the vendor's channel ("" when it could not be looked up).
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	// Auth is signed_in, signed_out or unknown; Account names who is signed in when the CLI says.
	Auth    string `json:"auth"`
	Account string `json:"account,omitempty"`
	// Installable is true when Orole can download a managed copy for this computer.
	Installable bool `json:"installable"`
	// Problem explains what could not be read (latest version, sign-in); it never blocks the rest.
	Problem string `json:"problem,omitempty"`
}

// Config wires a Manager. Zero values use production defaults.
type Config struct {
	// StoreDir holds managed copies: <StoreDir>/<id>/<version>/<binary>.
	StoreDir string
	// Overrides maps a provider id to an executable path that wins over everything else.
	Overrides map[string]string
	HTTP      *http.Client
	Exec      Runner
	LookPath  func(string) (string, error)
	GOOS      string
	GOARCH    string
	NPM       string // registry base
	GitHub    string // Codex "latest release" API URL
	Grok      string // Grok release base
	Now       func() time.Time
}

// Manager reads and changes the state of the provider CLIs.
type Manager struct {
	cfg Config

	mu      sync.Mutex // serialises installs
	latestM sync.Mutex
	latest  map[string]latestEntry
}

type latestEntry struct {
	version string
	err     string
	at      time.Time
}

// latestTTL is how long a looked-up release number is reused; vendors rate-limit anonymous lookups.
const latestTTL = 30 * time.Minute

// New returns a Manager.
func New(cfg Config) *Manager {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 20 * time.Minute}
	}
	if cfg.Exec == nil {
		cfg.Exec = execRunner
	}
	if cfg.LookPath == nil {
		cfg.LookPath = exec.LookPath
	}
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	cfg.NPM = strings.TrimRight(valueOr(cfg.NPM, defaultNPM), "/")
	cfg.GitHub = valueOr(cfg.GitHub, defaultGitHub)
	cfg.Grok = strings.TrimRight(valueOr(cfg.Grok, defaultGrok), "/")
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Manager{cfg: cfg, latest: map[string]latestEntry{}}
}

func valueOr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// ErrUnknown is returned for a provider id nobody registered.
var ErrUnknown = errors.New("unknown provider")

// All returns the status of every provider.
func (m *Manager) All(ctx context.Context) []Status {
	out := make([]Status, len(specs))
	var wg sync.WaitGroup
	for i, s := range specs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = m.status(ctx, s)
		}()
	}
	wg.Wait()
	return out
}

// Status returns the status of one provider.
func (m *Manager) Status(ctx context.Context, id string) (Status, error) {
	s, ok := specOf(id)
	if !ok {
		return Status{}, ErrUnknown
	}
	return m.status(ctx, s), nil
}

// Path returns the executable that runs for id, or "" when none does. Chat and Code use it to start the CLI.
func (m *Manager) Path(id string) string {
	s, ok := specOf(id)
	if !ok {
		return ""
	}
	path, _ := m.resolve(s)
	return path
}

func (m *Manager) status(ctx context.Context, s spec) Status {
	st := Status{ID: s.id, Name: s.name, Auth: Unknown, Installable: m.installable(s.id)}
	var problems []string
	path, source := m.resolve(s)
	if path != "" {
		st.Installed, st.Path, st.Source = true, path, source
		if v, err := m.version(ctx, path); err != nil {
			problems = append(problems, "could not read the version")
		} else {
			st.Version = v
		}
	}
	if latest, err := m.latestVersion(ctx, s.id); err != nil {
		problems = append(problems, "could not check for updates")
	} else {
		st.Latest = latest
		st.UpdateAvailable = st.Installed && st.Version != "" && compareVersions(st.Version, latest) < 0
	}
	if st.Installed && s.authArgs != nil {
		st.Auth, st.Account = m.auth(ctx, s, path)
	}
	st.Problem = strings.Join(problems, "; ")
	return st
}

// resolve picks the CLI to run: an explicit override, then the newest managed copy, then PATH.
func (m *Manager) resolve(s spec) (path, source string) {
	if p := m.cfg.Overrides[s.id]; p != "" {
		if fileExists(p) {
			return p, SourceOverride
		}
		return "", ""
	}
	if p := m.managed(s); p != "" {
		return p, SourceManaged
	}
	if p, err := m.cfg.LookPath(s.binary); err == nil {
		return p, SourceSystem
	}
	return "", ""
}

func (m *Manager) exeName(s spec) string {
	if m.cfg.GOOS == "windows" {
		return s.binary + ".exe"
	}
	return s.binary
}

// managed returns the newest managed copy that is present, or "".
func (m *Manager) managed(s spec) string {
	if m.cfg.StoreDir == "" {
		return ""
	}
	entries, err := os.ReadDir(filepath.Join(m.cfg.StoreDir, s.id))
	if err != nil {
		return ""
	}
	best, bestPath := "", ""
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(m.cfg.StoreDir, s.id, e.Name(), m.exeName(s))
		if fileExists(p) && (best == "" || compareVersions(e.Name(), best) > 0) {
			best, bestPath = e.Name(), p
		}
	}
	return bestPath
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+`)

// version asks the CLI for its version.
func (m *Manager) version(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := m.cfg.Exec(ctx, path, "--version")
	if v := versionRE.FindString(out); v != "" {
		return v, nil
	}
	if err == nil {
		err = errors.New("no version in the output")
	}
	return "", err
}

// auth reads the sign-in state with the CLI's own status command.
func (m *Manager) auth(ctx context.Context, s spec, path string) (state, account string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := m.cfg.Exec(ctx, path, s.authArgs...)
	switch s.id {
	case Claude:
		var v struct {
			LoggedIn bool   `json:"loggedIn"`
			Email    string `json:"email"`
		}
		if json.Unmarshal([]byte(out), &v) != nil {
			return Unknown, ""
		}
		if v.LoggedIn {
			return SignedIn, v.Email
		}
		return SignedOut, ""
	case Codex:
		low := strings.ToLower(out)
		switch {
		case strings.Contains(low, "not logged in"):
			return SignedOut, ""
		case strings.Contains(low, "logged in"):
			account = strings.TrimSpace(out)
			if i := strings.Index(low, "using "); i >= 0 {
				account = strings.TrimSpace(out[i+len("using "):])
			}
			return SignedIn, account
		case err != nil:
			return SignedOut, ""
		}
	}
	return Unknown, ""
}

// Login runs the CLI's own sign-in, which opens the vendor's page in the browser, and returns the new status.
// Orole never sees the credentials: the CLI stores them.
func (m *Manager) Login(ctx context.Context, id string) (Status, error) {
	return m.authAction(ctx, id, func(s spec) []string { return s.login }, "sign in")
}

// Logout signs the CLI out.
func (m *Manager) Logout(ctx context.Context, id string) (Status, error) {
	return m.authAction(ctx, id, func(s spec) []string { return s.logout }, "sign out")
}

func (m *Manager) authAction(ctx context.Context, id string, args func(spec) []string, verb string) (Status, error) {
	s, ok := specOf(id)
	if !ok {
		return Status{}, ErrUnknown
	}
	path, _ := m.resolve(s)
	if path == "" {
		return Status{}, fmt.Errorf("%s is not installed", s.name)
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if out, err := m.cfg.Exec(rctx, path, args(s)...); err != nil {
		return m.status(ctx, s), fmt.Errorf("could not %s %s: %s", verb, s.name, firstLine(out, err))
	}
	return m.status(ctx, s), nil
}

func firstLine(out string, err error) string {
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return err.Error()
}

// latestVersion returns the newest release number, reused for latestTTL.
func (m *Manager) latestVersion(ctx context.Context, id string) (string, error) {
	m.latestM.Lock()
	if e, ok := m.latest[id]; ok && m.cfg.Now().Sub(e.at) < latestTTL {
		m.latestM.Unlock()
		if e.err != "" {
			return "", errors.New(e.err)
		}
		return e.version, nil
	}
	m.latestM.Unlock()

	v, err := m.fetchLatest(ctx, id)
	e := latestEntry{version: v, at: m.cfg.Now()}
	if err != nil {
		e.err = err.Error()
	}
	m.latestM.Lock()
	m.latest[id] = e
	m.latestM.Unlock()
	return v, err
}

func (m *Manager) fetchLatest(ctx context.Context, id string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	switch id {
	case Claude:
		var v struct {
			Version string `json:"version"`
		}
		if err := m.getJSON(ctx, m.cfg.NPM+"/@anthropic-ai/claude-code/latest", &v); err != nil {
			return "", err
		}
		return validVersion(v.Version)
	case Codex:
		var v struct {
			Tag string `json:"tag_name"`
		}
		if err := m.getJSON(ctx, m.cfg.GitHub, &v); err != nil {
			return "", err
		}
		return validVersion(strings.TrimPrefix(v.Tag, "rust-v"))
	case Grok:
		b, err := m.get(ctx, m.cfg.Grok+"/stable")
		if err != nil {
			return "", err
		}
		return validVersion(strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0]))
	}
	return "", ErrUnknown
}

func validVersion(v string) (string, error) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(v) {
		return "", fmt.Errorf("unexpected version %q", v)
	}
	return v, nil
}

func (m *Manager) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "orole-os")
	resp, err := m.cfg.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func (m *Manager) getJSON(ctx context.Context, url string, out any) error {
	b, err := m.get(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// compareVersions compares dotted numeric versions: -1, 0 or 1. A missing or non-numeric part counts as 0.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		x, y := part(pa, i), part(pb, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func part(p []string, i int) int {
	if i >= len(p) {
		return 0
	}
	n, _ := strconv.Atoi(strings.SplitN(p[i], "-", 2)[0])
	return n
}

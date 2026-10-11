package cliruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCLI answers --version and the auth commands like the real CLIs do.
type fakeCLI struct {
	version string
	out     map[string]string // joined args -> output
	fail    map[string]bool
}

func (f *fakeCLI) run(_ context.Context, _ string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	if key == "--version" {
		return "tool " + f.version + " (build)\n", nil
	}
	out, ok := f.out[key]
	if !ok {
		return "", errors.New("unknown command " + key)
	}
	if f.fail[key] {
		return out, errors.New("exit status 1")
	}
	return out, nil
}

func tarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	tw.Write(data)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func newTestManager(t *testing.T, cli *fakeCLI, over func(*Config)) *Manager {
	t.Helper()
	cfg := Config{
		StoreDir: t.TempDir(),
		Exec:     cli.run,
		LookPath: func(string) (string, error) { return "", errors.New("not on PATH") },
		GOOS:     "linux",
		GOARCH:   "amd64",
	}
	if over != nil {
		over(&cfg)
	}
	return New(cfg)
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"2.1.289", "2.1.296", -1}, {"2.1.296", "2.1.289", 1}, {"1.0.50", "1.0.50", 0},
		{"0.156.1", "0.162.1", -1}, {"1.10.0", "1.9.9", 1}, {"1.0", "1.0.0", 0},
	} {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestStatusReadsVersionUpdateAndSignIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/claude-code/latest"):
			fmt.Fprint(w, `{"version":"2.1.296"}`)
		case r.URL.Path == "/codex":
			fmt.Fprint(w, `{"tag_name":"rust-v0.162.1"}`)
		case r.URL.Path == "/grok/stable":
			fmt.Fprint(w, "1.0.50\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	claudeCLI := &fakeCLI{version: "2.1.289", out: map[string]string{"auth status": `{"loggedIn":true,"email":"ray@example.com"}`}}
	m := newTestManager(t, claudeCLI, func(c *Config) {
		c.NPM, c.GitHub, c.Grok = srv.URL, srv.URL+"/codex", srv.URL+"/grok"
		c.LookPath = func(name string) (string, error) {
			if name == "claude" || name == "codex" {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("missing")
		}
	})
	// Codex shares the fake runner; give it its own answers.
	claudeCLI.out["login status"] = "Logged in using ChatGPT"

	all := m.All(context.Background())
	by := map[string]Status{}
	for _, s := range all {
		by[s.ID] = s
	}
	c := by[Claude]
	if !c.Installed || c.Source != SourceSystem || c.Version != "2.1.289" || c.Latest != "2.1.296" || !c.UpdateAvailable || c.Auth != SignedIn || c.Account != "ray@example.com" {
		t.Fatalf("claude: %+v", c)
	}
	x := by[Codex]
	if x.Auth != SignedIn || x.Account != "ChatGPT" || x.Latest != "0.162.1" {
		t.Fatalf("codex: %+v", x)
	}
	g := by[Grok]
	if g.Installed || g.UpdateAvailable || g.Latest != "1.0.50" || g.Auth != Unknown || !g.Installable {
		t.Fatalf("grok: %+v", g)
	}

	// Grok has no status command: `models` says whether a sign-in exists.
	for out, want := range map[string]string{
		"You are not authenticated.\n\nDefault model: grok-4.6\n\nAvailable models:\n  * grok-4.6 (default)\n": SignedOut,
		"Default model: grok-4.6\n\nAvailable models:\n  * grok-4.6 (default)\n":                               SignedIn,
		"something unexpected": Unknown,
	} {
		cli := &fakeCLI{version: "1.0.50", out: map[string]string{"models": out}}
		gm := newTestManager(t, cli, func(c *Config) {
			c.NPM, c.GitHub, c.Grok = srv.URL, srv.URL+"/codex", srv.URL+"/grok"
			c.LookPath = func(string) (string, error) { return "/bin/grok", nil }
		})
		if st, _ := gm.Status(context.Background(), Grok); st.Auth != want {
			t.Errorf("grok auth for %q = %s, want %s", out, st.Auth, want)
		}
	}
}

func TestAStatusLookupFailureNeverHidesTheRest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "rate limited", 429) }))
	defer srv.Close()
	cli := &fakeCLI{version: "0.156.1", out: map[string]string{"login status": "Not logged in"}, fail: map[string]bool{"login status": true}}
	m := newTestManager(t, cli, func(c *Config) {
		c.NPM, c.GitHub, c.Grok = srv.URL, srv.URL, srv.URL
		c.LookPath = func(string) (string, error) { return "/bin/codex", nil }
	})
	st, err := m.Status(context.Background(), Codex)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || st.Version != "0.156.1" || st.Auth != SignedOut || st.Latest != "" || st.UpdateAvailable || !strings.Contains(st.Problem, "updates") {
		t.Fatalf("codex: %+v", st)
	}
	if _, err := m.Status(context.Background(), "nope"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown provider: %v", err)
	}
}

func TestResolvePrefersOverrideThenManagedThenPath(t *testing.T) {
	cli := &fakeCLI{version: "1.0.0"}
	dir := t.TempDir()
	override := filepath.Join(dir, "my-grok")
	os.WriteFile(override, []byte("x"), 0o700)
	m := newTestManager(t, cli, func(c *Config) {
		c.Overrides = map[string]string{Grok: override}
		c.LookPath = func(string) (string, error) { return "/usr/bin/tool", nil }
	})
	for _, v := range []string{"1.0.5", "1.0.50", "1.0.9"} {
		p := filepath.Join(m.cfg.StoreDir, Grok, v)
		os.MkdirAll(p, 0o700)
		os.WriteFile(filepath.Join(p, "grok"), []byte("x"), 0o700)
	}
	if got, src := m.resolve(mustSpec(Grok)); got != override || src != SourceOverride {
		t.Fatalf("override: %q %q", got, src)
	}
	if got := m.Path(Grok); got != override {
		t.Fatalf("Path: %q", got)
	}
	m.cfg.Overrides = nil
	if got, src := m.resolve(mustSpec(Grok)); !strings.HasSuffix(filepath.ToSlash(got), "grok/1.0.50/grok") || src != SourceManaged {
		t.Fatalf("the newest managed copy must win: %q %q", got, src)
	}
	if got, src := m.resolve(mustSpec(Claude)); got != "/usr/bin/tool" || src != SourceSystem {
		t.Fatalf("PATH: %q %q", got, src)
	}
	m.cfg.Overrides = map[string]string{Claude: filepath.Join(dir, "missing")}
	if got, _ := m.resolve(mustSpec(Claude)); got != "" {
		t.Fatalf("a missing override must not fall back silently: %q", got)
	}
}

func mustSpec(id string) spec { s, _ := specOf(id); return s }

func TestInstallCodexChecksTheHashAndKeepsTheOldVersionOnFailure(t *testing.T) {
	binary := []byte("codex binary")
	archive := tarGz(t, "codex-x86_64-unknown-linux-musl", binary)
	sum := sha256.Sum256(archive)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			fmt.Fprintf(w, `{"tag_name":"rust-v0.162.1","assets":[{"name":"codex-x86_64-unknown-linux-musl.tar.gz","digest":%q,"browser_download_url":%q}]}`, digest, srv.URL+"/asset")
		case "/asset":
			w.Write(archive)
		}
	}))
	defer srv.Close()

	cli := &fakeCLI{version: "0.162.1"}
	m := newTestManager(t, cli, func(c *Config) { c.GitHub = srv.URL + "/release" })
	st, err := m.Install(context.Background(), Codex)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || st.Source != SourceManaged || st.Version != "0.162.1" || st.UpdateAvailable {
		t.Fatalf("installed: %+v", st)
	}
	data, err := os.ReadFile(filepath.Join(m.cfg.StoreDir, Codex, "0.162.1", "codex"))
	if err != nil || !bytes.Equal(data, binary) {
		t.Fatalf("binary: %q %v", data, err)
	}
	// Installing again changes nothing.
	if _, err := m.Install(context.Background(), Codex); err != nil {
		t.Fatal(err)
	}

	// A tampered download is refused and the working copy stays.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/release" {
			fmt.Fprintf(w, `{"tag_name":"rust-v0.163.0","assets":[{"name":"codex-x86_64-unknown-linux-musl.tar.gz","digest":%q,"browser_download_url":%q}]}`, digest, "http://"+r.Host+"/asset")
			return
		}
		w.Write(tarGz(t, "codex-x86_64-unknown-linux-musl", []byte("evil")))
	}))
	defer bad.Close()
	m.cfg.GitHub = bad.URL + "/release"
	if _, err := m.Install(context.Background(), Codex); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("tampered download: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.cfg.StoreDir, Codex, "0.162.1", "codex")); err != nil {
		t.Fatalf("the working copy must stay: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(m.cfg.StoreDir, Codex))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Fatalf("staging left behind: %s", e.Name())
		}
	}
}

func TestInstallClaudeFromNpmAndRefusesACopyThatReportsAnotherVersion(t *testing.T) {
	binary := []byte("claude binary")
	archive := tarGz(t, "package/claude", binary)
	sum := sha512.Sum512(archive)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/claude-code/latest"):
			fmt.Fprint(w, `{"version":"2.1.296"}`)
		case strings.HasSuffix(r.URL.Path, "/claude-code-linux-x64/2.1.296"):
			fmt.Fprintf(w, `{"dist":{"tarball":%q,"integrity":%q}}`, srv.URL+"/tgz", integrity)
		case r.URL.Path == "/tgz":
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cli := &fakeCLI{version: "2.1.289"} // the downloaded binary claims an older version
	m := newTestManager(t, cli, func(c *Config) { c.NPM = srv.URL })
	if _, err := m.Install(context.Background(), Claude); err == nil || !strings.Contains(err.Error(), "does not start as version 2.1.296") {
		t.Fatalf("a binary reporting another version must be refused: %v", err)
	}
	if m.managed(mustSpec(Claude)) != "" {
		t.Fatal("a refused install left a managed copy")
	}
	cli.version = "2.1.296"
	st, err := m.Install(context.Background(), Claude)
	if err != nil || st.Source != SourceManaged || st.Version != "2.1.296" {
		t.Fatalf("install: %+v %v", st, err)
	}
}

func TestInstallGrokDownloadsTheReleaseBinaryAndPrunesOldVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stable":
			fmt.Fprint(w, "1.0.50\n")
		case "/grok-1.0.50-linux-x86_64":
			w.Write([]byte("grok binary"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cli := &fakeCLI{version: "1.0.50"}
	m := newTestManager(t, cli, func(c *Config) { c.Grok = srv.URL })
	for _, v := range []string{"1.0.1", "1.0.2", "1.0.3"} {
		os.MkdirAll(filepath.Join(m.cfg.StoreDir, Grok, v), 0o700)
	}
	if _, err := m.Install(context.Background(), Grok); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(m.cfg.StoreDir, Grok))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "1.0.3,1.0.50" {
		t.Fatalf("only the two newest versions stay: %v", names)
	}
	if m2 := newTestManager(t, cli, func(c *Config) { c.GOOS, c.GOARCH = "plan9", "386" }); m2.installable(Grok) {
		t.Fatal("an unsupported platform must not offer a download")
	}
}

func TestLoginAndLogoutRunTheCLIsOwnCommands(t *testing.T) {
	cli := &fakeCLI{version: "2.1.296", out: map[string]string{"auth login --claudeai": "", "auth logout": "", "auth status": `{"loggedIn":false}`}}
	m := newTestManager(t, cli, func(c *Config) {
		c.LookPath = func(n string) (string, error) {
			if n == "claude" {
				return "/bin/claude", nil
			}
			return "", errors.New("missing")
		}
		c.Now = func() time.Time { return time.Unix(0, 0) }
	})
	st, err := m.Login(context.Background(), Claude)
	if err != nil || st.Auth != SignedOut {
		t.Fatalf("login: %+v %v", st, err)
	}
	if _, err := m.Logout(context.Background(), Claude); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Login(context.Background(), Grok); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("login without the CLI: %v", err)
	}
}

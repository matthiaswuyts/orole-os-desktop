package cliruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallCodexWindowsRepairsLegacyCopy(t *testing.T) {
	const prefix = "package/vendor/x86_64-pc-windows-msvc/"
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, rel := range []string{"bin/codex.exe", "bin/codex-code-mode-host.exe", "codex-resources/codex-command-runner.exe", "codex-resources/codex-windows-sandbox-setup.exe"} {
		if err := tw.WriteHeader(&tar.Header{Name: prefix + rel, Typeflag: tar.TypeReg, Size: 2}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte("ok")); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(b.Bytes())
	var srv *httptest.Server
	downloads := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			fmt.Fprint(w, `{"tag_name":"rust-v0.162.1"}`)
		case "/@openai/codex/0.162.1-win32-x64":
			fmt.Fprintf(w, `{"dist":{"tarball":%q,"integrity":%q}}`, srv.URL+"/asset", "sha512-"+base64.StdEncoding.EncodeToString(sum[:]))
		case "/asset":
			downloads++
			_, _ = w.Write(b.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m := newTestManager(t, &fakeCLI{version: "0.162.1"}, func(c *Config) { c.GOOS = "windows"; c.NPM = srv.URL; c.GitHub = srv.URL + "/release" })
	legacy := filepath.Join(m.cfg.StoreDir, Codex, "0.162.1")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "codex.exe"), []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if m.Path(Codex) != "" {
		t.Fatal("incomplete legacy runtime selected")
	}
	st, err := m.Install(context.Background(), Codex)
	if err != nil {
		t.Fatal(err)
	}
	if st.Path != filepath.Join(legacy, "bin", "codex.exe") || st.Source != SourceManaged || !st.Installed {
		t.Fatalf("status: %+v", st)
	}
	if _, err := m.Install(context.Background(), Codex); err != nil {
		t.Fatal(err)
	}
	if downloads != 1 {
		t.Fatalf("complete installation downloaded again: %d", downloads)
	}
}

func TestCodexWindowsBundle(t *testing.T) {
	const prefix = "package/vendor/x86_64-pc-windows-msvc/"
	files := []string{"bin/codex.exe", "bin/codex-code-mode-host.exe", "codex-resources/codex-command-runner.exe", "codex-resources/codex-windows-sandbox-setup.exe", "codex-resources/voice/manifest.json"}
	for _, tc := range []struct {
		name      string
		entries   []string
		wantError bool
	}{
		{"complete", files, false},
		{"missing-helper", files[:1], true},
		{"traversal", []string{"../escape.exe"}, true},
		{"windows-path", []string{`bin\escape.exe`}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			gz := gzip.NewWriter(&b)
			tw := tar.NewWriter(gz)
			for _, name := range tc.entries {
				if err := tw.WriteHeader(&tar.Header{Name: prefix + name, Typeflag: tar.TypeReg, Size: 2}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write([]byte("ok")); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "bundle.tgz")
			if err := os.WriteFile(archive, b.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			dst := t.TempDir()
			err := extractCodexWindows(archive, prefix, dst)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if !tc.wantError {
				m := New(Config{GOOS: "windows", GOARCH: "amd64"})
				s, _ := specOf(Codex)
				if !m.completeInstall(s, dst) {
					t.Fatal("complete bundle rejected")
				}
				if err := os.Remove(filepath.Join(dst, "bin", "codex-code-mode-host.exe")); err != nil {
					t.Fatal(err)
				}
				if m.completeInstall(s, dst) {
					t.Fatal("incomplete bundle accepted")
				}
			}
		})
	}
}

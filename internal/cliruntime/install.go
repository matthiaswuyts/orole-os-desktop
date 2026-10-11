package cliruntime

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// keepVersions is how many managed versions stay on disk, so a bad release can be rolled back by hand.
const keepVersions = 2

func (m *Manager) installable(id string) bool {
	if m.cfg.StoreDir == "" {
		return false
	}
	switch id {
	case Claude:
		return claudePlatform(m.cfg.GOOS, m.cfg.GOARCH) != ""
	case Codex:
		return codexTriple(m.cfg.GOOS, m.cfg.GOARCH) != ""
	case Grok:
		return grokPlatform(m.cfg.GOOS, m.cfg.GOARCH) != ""
	}
	return false
}

// Install downloads the latest release of a provider's CLI into Orole's store and returns the new status.
// It is also the update: a newer release is installed beside the old one, which runs until the new one has
// been started once (`--version`), so a release that does not start never replaces a working CLI.
func (m *Manager) Install(ctx context.Context, id string) (Status, error) {
	s, ok := specOf(id)
	if !ok {
		return Status{}, ErrUnknown
	}
	if !m.installable(id) {
		return Status{}, fmt.Errorf("%s cannot be downloaded for this computer", s.name)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.latestM.Lock()
	delete(m.latest, id) // an install decides on the freshest answer
	m.latestM.Unlock()
	version, err := m.latestVersion(ctx, id)
	if err != nil {
		return Status{}, fmt.Errorf("could not find the latest %s release: %w", s.name, err)
	}
	root := filepath.Join(m.cfg.StoreDir, id)
	dest := filepath.Join(root, version)
	if fileExists(filepath.Join(dest, m.exeName(s))) {
		return m.status(ctx, s), nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Status{}, err
	}
	stage, err := os.MkdirTemp(root, ".staging-")
	if err != nil {
		return Status{}, err
	}
	defer os.RemoveAll(stage)
	exe := filepath.Join(stage, m.exeName(s))

	switch id {
	case Claude:
		err = m.fetchClaude(ctx, version, stage, exe)
	case Codex:
		err = m.fetchCodex(ctx, stage, exe)
	case Grok:
		err = m.fetchGrok(ctx, version, exe)
	}
	if err != nil {
		return Status{}, fmt.Errorf("could not download %s: %w", s.name, err)
	}
	if err := os.Chmod(exe, 0o755); err != nil {
		return Status{}, err
	}
	if got, err := m.version(ctx, exe); err != nil || compareVersions(got, version) != 0 {
		return Status{}, fmt.Errorf("the downloaded %s does not start as version %s", s.name, version)
	}
	if err := os.RemoveAll(dest); err != nil {
		return Status{}, err
	}
	if err := os.Rename(stage, dest); err != nil {
		return Status{}, err
	}
	m.prune(root, keepVersions)
	return m.status(ctx, s), nil
}

// prune removes all but the newest keep versions in root; a version that cannot be removed (running on
// Windows) stays and is retried next time.
func (m *Manager) prune(root string, keep int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			versions = append(versions, e.Name())
		}
	}
	for len(versions) > keep {
		oldest := 0
		for i, v := range versions {
			if compareVersions(v, versions[oldest]) < 0 {
				oldest = i
			}
		}
		_ = os.RemoveAll(filepath.Join(root, versions[oldest]))
		versions = append(versions[:oldest], versions[oldest+1:]...)
	}
}

// fetchClaude downloads Claude Code's native binary from its npm platform package and checks npm's sha512.
func (m *Manager) fetchClaude(ctx context.Context, version, stage, exe string) error {
	pkg := "@anthropic-ai/claude-code-" + claudePlatform(m.cfg.GOOS, m.cfg.GOARCH)
	var meta struct {
		Dist struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	if err := m.getJSON(ctx, m.cfg.NPM+"/"+pkg+"/"+version, &meta); err != nil {
		return err
	}
	sum, ok := strings.CutPrefix(meta.Dist.Integrity, "sha512-")
	if !ok || meta.Dist.Tarball == "" {
		return errors.New("npm published no sha512 hash for this release")
	}
	want, err := base64.StdEncoding.DecodeString(sum)
	if err != nil {
		return errors.New("npm published an unreadable hash")
	}
	archive := filepath.Join(stage, "download.tgz")
	if err := m.download(ctx, meta.Dist.Tarball, archive, sha512.New(), want); err != nil {
		return err
	}
	return extractTarGz(archive, "package/"+filepath.Base(exe), exe)
}

// fetchCodex downloads Codex's release asset for this computer and checks the sha256 GitHub publishes for it.
func (m *Manager) fetchCodex(ctx context.Context, stage, exe string) error {
	var rel struct {
		Assets []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
			URL    string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := m.getJSON(ctx, m.cfg.GitHub, &rel); err != nil {
		return err
	}
	name := "codex-" + codexTriple(m.cfg.GOOS, m.cfg.GOARCH)
	if m.cfg.GOOS == "windows" {
		name += ".exe"
	}
	name += ".tar.gz"
	for _, a := range rel.Assets {
		if a.Name != name {
			continue
		}
		hexSum, ok := strings.CutPrefix(a.Digest, "sha256:")
		if !ok {
			return errors.New("GitHub published no sha256 hash for this release")
		}
		want, err := hex.DecodeString(hexSum)
		if err != nil {
			return errors.New("GitHub published an unreadable hash")
		}
		archive := filepath.Join(stage, "download.tar.gz")
		if err := m.download(ctx, a.URL, archive, sha256.New(), want); err != nil {
			return err
		}
		return extractTarGz(archive, "", exe)
	}
	return fmt.Errorf("the release has no %s", name)
}

// fetchGrok downloads Grok's release binary. x.ai publishes no hash, so it is trusted on TLS alone.
func (m *Manager) fetchGrok(ctx context.Context, version, exe string) error {
	url := fmt.Sprintf("%s/grok-%s-%s", m.cfg.Grok, version, grokPlatform(m.cfg.GOOS, m.cfg.GOARCH))
	if m.cfg.GOOS == "windows" {
		url += ".exe"
	}
	return m.download(ctx, url, exe, nil, nil)
}

// download streams url into dst. With a hash, the bytes must match want or dst is removed.
func (m *Manager) download(ctx context.Context, url, dst string, h hash.Hash, want []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "orole-os")
	resp, err := m.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	var w io.Writer = f
	if h != nil {
		w = io.MultiWriter(f, h)
	}
	_, copyErr := io.Copy(w, resp.Body)
	if err := f.Close(); copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		os.Remove(dst)
		return copyErr
	}
	if h != nil && !equalBytes(h.Sum(nil), want) {
		os.Remove(dst)
		return errors.New("the download does not match its published hash")
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// extractTarGz writes one file of a .tar.gz to dst: the entry called want, or (want == "") the first regular file.
func extractTarGz(archive, want, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive does not contain the CLI")
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || (want != "" && path.Clean(hdr.Name) != want) {
			continue
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		if err := out.Close(); copyErr == nil {
			copyErr = err
		}
		return copyErr
	}
}

package daemon_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/testrepo"
)

// Git fixtures are built once per test binary and copied per test (see internal/testrepo).
// TestMain removes the templates, including the one in the internal-package tests.
func TestMain(m *testing.M) {
	code := m.Run()
	testrepo.CloseAll()
	os.Exit(code)
}

// emptyRepoTemplate is a repository on main whose only commit is empty.
var emptyRepoTemplate = testrepo.New(func(dir string) error {
	if err := testrepo.Git(dir, "init", "-b", "main"); err != nil {
		return err
	}
	return testrepo.Git(dir, "-c", "user.name=T", "-c", "user.email=t@e.c", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
})

// copyEmptyRepo puts a copy of emptyRepoTemplate at dir, creating it if needed.
func copyEmptyRepo(t *testing.T, dir string) {
	t.Helper()
	emptyRepoTemplate.Copy(t, dir)
}

// publishTemplate holds <work> and <remote.git>: work is on main with a base commit, origin
// https://github.com/owner/repo.git is already pushed to the bare remote through a url.<bare>.insteadOf
// rewrite, and origin/HEAD is set. The rewrite is not stored in the template, because it names the
// template's own path; copyPublishRepo adds it for the copy.
var publishTemplate = testrepo.New(func(dir string) error {
	work, bare := filepath.Join(dir, "work"), filepath.Join(dir, "remote.git")
	if err := os.Mkdir(work, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("base\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Daemon Test"},
		{"config", "user.email", "daemon@example.test"},
		{"config", "commit.gpgsign", "false"},
		{"add", "."},
		{"commit", "-m", "base"},
		{"init", "--bare", "-b", "main", bare},
		{"remote", "add", "origin", "https://github.com/owner/repo.git"},
		{"-c", "url." + bare + ".insteadOf=https://github.com/owner/repo.git", "push", "origin", "main"},
		{"remote", "set-head", "origin", "main"},
	} {
		if err := testrepo.Git(work, args...); err != nil {
			return err
		}
	}
	return nil
})

// copyPublishRepo returns a working repository whose github.com origin is rewritten to a local bare
// remote, so pushes are real and need no network, and that bare remote.
func copyPublishRepo(t *testing.T) (repo, bare string) {
	t.Helper()
	root := t.TempDir()
	publishTemplate.Copy(t, root)
	repo, bare = filepath.Join(root, "work"), filepath.Join(root, "remote.git")
	publishGit(t, repo, "config", "url."+bare+".insteadOf", "https://github.com/owner/repo.git")
	return repo, bare
}

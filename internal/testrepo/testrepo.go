// Package testrepo is test support: it builds a git fixture repository once per test binary and
// copies it for each test, instead of spawning `git init`, `add` and `commit` (several process
// launches, slow on Windows) in every test. It is imported only from _test.go files and is never
// linked into a shipped binary.
//
// Declare templates as package variables and remove them when the tests finish:
//
//	var tmpl = testrepo.New(func(dir string) error { ... testrepo.Git(dir, "init", "-b", "main") ... })
//
//	func TestMain(m *testing.M) {
//		code := m.Run()
//		testrepo.CloseAll()
//		os.Exit(code)
//	}
//
// The build function must not record absolute paths of its own directory in the repository (no
// remotes rewritten to local paths, worktrees or submodules): the copy lives elsewhere. Tests
// mutate their copy freely; the template is only ever read.
package testrepo

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	regMu    sync.Mutex
	registry []*Template
)

// Template is a lazily built directory tree. The zero value is not usable; call New.
type Template struct {
	build func(dir string) error
	once  sync.Once
	root  string // temp directory holding the template
	dir   string // the tree inside root
	err   error
}

// New returns a Template whose contents are created by build on first use. build receives an
// empty directory and must leave a complete repository (working tree and .git), or a directory
// holding several, in it.
func New(build func(dir string) error) *Template {
	tp := &Template{build: build}
	regMu.Lock()
	registry = append(registry, tp)
	regMu.Unlock()
	return tp
}

// Copy builds the template if needed and copies it into dst, creating dst if it does not exist.
func (tp *Template) Copy(tb testing.TB, dst string) {
	tb.Helper()
	tp.once.Do(func() {
		tp.root, tp.err = os.MkdirTemp("", "orole-testrepo-")
		if tp.err != nil {
			return
		}
		tp.dir = filepath.Join(tp.root, "tree")
		if tp.err = os.Mkdir(tp.dir, 0o755); tp.err == nil {
			tp.err = tp.build(tp.dir)
		}
	})
	if tp.err != nil {
		tb.Fatalf("building the template repository: %v", tp.err)
	}
	if err := copyTree(tp.dir, dst); err != nil {
		tb.Fatalf("copying the template repository: %v", err)
	}
}

// CloseAll removes every template directory created so far. Call it from TestMain after m.Run.
func CloseAll() {
	regMu.Lock()
	defer regMu.Unlock()
	for _, tp := range registry {
		if tp.root != "" {
			_ = os.RemoveAll(tp.root)
		}
	}
	registry = nil
}

// Git runs git in dir and returns an error carrying its combined output on failure. It is meant
// for build functions, which have no *testing.T.
func Git(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s: unsupported file type %v in template", path, info.Mode().Type())
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()|0o200)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

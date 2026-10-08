package files

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// NewGit returns the "git" provider: the local path policy applied to project
// (git repository) roots only. Compared with the local provider it is
// read-only inside .git (repository state is changed by git, not by file
// edits) and its listings carry `git status` markers. Its roots are its own:
// approving a directory for the local provider grants nothing here.
func NewGit(src RootSource) *Local {
	l := &Local{id: "git", src: src}
	l.rootCheck = func(real string) error {
		if _, err := os.Lstat(filepath.Join(real, ".git")); err != nil {
			return fmt.Errorf("%w: %s is not a git repository", ErrDenied, real)
		}
		return nil
	}
	l.guard = func(clean string) error {
		vol := filepath.VolumeName(clean)
		for _, c := range splitComponents(clean[len(vol):]) {
			if foldPath(c) == ".git" {
				return fmt.Errorf("%w: .git is read-only here", ErrReadOnly)
			}
		}
		return nil
	}
	l.annotate = gitAnnotate
	return l
}

// gitAnnotate sets Entry.Git from `git status`. Failures leave entries
// unmarked: status is decoration, never a reason to fail a listing.
func gitAnnotate(ctx context.Context, dir string, entries []Entry) {
	git, err := exec.LookPath("git")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	run := func(args ...string) ([]byte, error) {
		// core.fsmonitor/hooksPath in an untrusted repo's config can run programs.
		base := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=NUL", "-C", dir}
		c := exec.CommandContext(ctx, git, append(base, args...)...)
		c.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
		hideWindow(c)
		var out bytes.Buffer
		c.Stdout = &out
		err := c.Run()
		return out.Bytes(), err
	}
	pfx, err := run("rev-parse", "--show-prefix")
	if err != nil {
		return
	}
	prefix := strings.TrimSpace(string(pfx))
	out, err := run("status", "--porcelain=v1", "-z", "--untracked-files=normal", "--", ".")
	if err != nil {
		return
	}
	status := parsePorcelain(out, prefix)
	for i := range entries {
		if s, ok := status[entries[i].Name]; ok {
			entries[i].Git = s
		}
	}
}

// parsePorcelain maps the immediate child name (relative to prefix) to a
// two-letter status; directories get the status of their first changed child.
func parsePorcelain(out []byte, prefix string) map[string]string {
	res := map[string]string{}
	recs := bytes.Split(out, []byte{0})
	for i := 0; i < len(recs); i++ {
		r := string(recs[i])
		if len(r) < 4 {
			continue
		}
		xy, p := r[:2], r[3:]
		if xy[0] == 'R' || xy[0] == 'C' || xy[1] == 'R' || xy[1] == 'C' {
			i++ // the original path follows as its own record
		}
		p = strings.TrimSuffix(p, "/")
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := p[len(prefix):]
		name := rest
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			name = rest[:j]
		}
		if name == "" {
			continue
		}
		if _, seen := res[name]; !seen {
			res[name] = xy
		}
	}
	return res
}

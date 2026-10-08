package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/git"
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
	top, err := git.Toplevel(ctx, dir)
	if err != nil {
		return
	}
	relative, err := filepath.Rel(top, dir)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, err := git.GetStatus(ctx, dir)
	if err != nil {
		return
	}
	prefix := ""
	if relative != "." {
		prefix = filepath.ToSlash(relative) + "/"
	}
	status := immediateGitStatus(result.Files, prefix)
	for i := range entries {
		if s, ok := status[entries[i].Name]; ok {
			entries[i].Git = s
		}
	}
}

// immediateGitStatus maps repository-relative paths to immediate children. A
// directory carries its first changed child's two-letter porcelain status.
func immediateGitStatus(files []git.FileStatus, prefix string) map[string]string {
	res := map[string]string{}
	for _, file := range files {
		path := strings.TrimSuffix(file.Path, "/")
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		name := strings.TrimPrefix(path, prefix)
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[:i]
		}
		if name != "" {
			if _, seen := res[name]; !seen {
				res[name] = strings.ReplaceAll(file.Index+file.Worktree, ".", " ")
			}
		}
	}
	return res
}

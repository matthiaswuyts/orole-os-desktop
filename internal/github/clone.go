package github

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// CloneProgress is one progress update of a clone.
type CloneProgress struct {
	Line    string `json:"line"`
	Percent int    `json:"percent"` // -1 when unknown
}

// cloneBaseURL is where repositories are cloned from (overridden in tests).
var cloneBaseURL = "https://github.com/"

var pctRe = regexp.MustCompile(`(\d{1,3})%`)

// CloneTarget returns the directory a clone of repo into destDir will create.
func CloneTarget(repo, destDir string) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("github: invalid repository %q", repo)
	}
	if destDir == "" || !filepath.IsAbs(destDir) {
		return "", errors.New("github: destination must be an absolute path")
	}
	_, name, _ := strings.Cut(repo, "/")
	return filepath.Join(filepath.Clean(destDir), name), nil
}

// Clone runs `git clone` for repo (owner/name over https) into destDir/<name>,
// reporting progress. Credentials come from the user's own git configuration
// (Git Credential Manager); this package never hands the API token to git.
// git is executed directly to avoid a dependency on internal/git.
func Clone(ctx context.Context, gitExe, repo, destDir string, progress func(CloneProgress)) (string, error) {
	target, err := CloneTarget(repo, destDir)
	if err != nil {
		return "", err
	}
	if ents, err := os.ReadDir(target); err == nil && len(ents) > 0 {
		return "", fmt.Errorf("github: %s already exists and is not empty", target)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	if gitExe == "" {
		gitExe = "git"
	}
	cmd := exec.CommandContext(ctx, gitExe, "clone", "--progress", "--", cloneBaseURL+repo+".git", target)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	hideWindow(cmd)
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = pw
	cmd.Stdout = nil
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return "", fmt.Errorf("github: git not available: %w", err)
	}
	pw.Close()
	var last string
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	sc.Split(splitCRLF)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		last = line
		p := -1
		if m := pctRe.FindStringSubmatch(line); m != nil {
			p, _ = strconv.Atoi(m[1])
		}
		if progress != nil {
			progress(CloneProgress{Line: line, Percent: p})
		}
	}
	pr.Close()
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("github: git clone failed: %s", last)
	}
	return target, nil
}

// splitCRLF splits on \n or \r (git rewrites progress lines with \r).
func splitCRLF(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

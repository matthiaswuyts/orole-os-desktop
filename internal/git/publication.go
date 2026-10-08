package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Publication is the immutable content presented for publication approval.
type Publication struct {
	Head     string
	BaseHead string
	Diff     string
}

// publicationRepo uses the source objects and index, but never its configuration,
// hooks, refs/replace, remote URLs, filters, or credential commands. Read-only
// inspection of source metadata uses run; content operations use this isolated
// Git directory. No API token is passed to Git.
type publicationRepo struct {
	dir      string
	worktree string
	objects  string
	index    string
	options  []string
}

func publicationRead(ctx context.Context, dir string, args ...string) (string, error) {
	out, truncated, err := run(ctx, DefaultTimeout, dir, args...)
	if truncated {
		return "", errors.New("git: publication output exceeds review limit")
	}
	return strings.TrimSpace(string(out)), err
}

func publicationHead(ctx context.Context, dir, branch string) (string, error) {
	if err := ValidBranchName(ctx, branch); err != nil {
		return "", err
	}
	ref, err := publicationRead(ctx, dir, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || ref != "refs/heads/"+branch {
		return "", errors.New("git: publication requires the expected checked-out branch")
	}
	head, err := publicationRead(ctx, dir, "rev-parse", "--verify", "--end-of-options", ref)
	if err != nil || !publicationSHA(head) {
		return "", errors.New("git: publication branch has no valid commit")
	}
	return head, nil
}

func publicationSHA(s string) bool {
	return (len(s) == 40 || len(s) == 64) && strings.Trim(s, "0123456789abcdef") == ""
}

func openPublicationRepo(ctx context.Context, dir, head string) (*publicationRepo, error) {
	root, err := publicationRead(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	objects, err := publicationRead(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	if err != nil {
		return nil, err
	}
	index, err := publicationRead(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "orole-publication-")
	if err != nil {
		return nil, err
	}
	p := &publicationRepo{dir: tmp, worktree: root, objects: objects, index: index}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(tmp)
		}
	}()
	for _, name := range []string{"objects", "refs"} {
		if err := os.Mkdir(filepath.Join(tmp, name), 0o700); err != nil {
			return nil, err
		}
	}
	config := "[core]\n\trepositoryformatversion = 0\n\tbare = false\n"
	if len(head) == 64 {
		config = "[core]\n\trepositoryformatversion = 1\n\tbare = false\n[extensions]\n\tobjectformat = sha256\n"
	}
	if err := os.WriteFile(filepath.Join(tmp, "config"), []byte(config), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "HEAD"), []byte(head+"\n"), 0o600); err != nil {
		return nil, err
	}
	// Preserve only built-in normalization, never command-valued settings.
	for _, key := range []string{"core.autocrlf", "core.eol", "core.filemode", "core.symlinks", "core.ignorecase"} {
		value, err := publicationRead(ctx, dir, "config", "--get", key)
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 1 {
				continue
			}
			return nil, errors.New("git: could not read worktree normalization settings")
		}
		p.options = append(p.options, "-c", key+"="+value)
	}
	ok = true
	return p, nil
}

func (p *publicationRepo) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"--no-replace-objects", "--git-dir=" + p.dir, "--work-tree=" + p.worktree,
		"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + filepath.Join(p.dir, "no-hooks"),
		"-c", "core.attributesFile=" + os.DevNull, "-c", "core.pager=cat",
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "http.followRedirects=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}, p.options...)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = p.dir
	env := sanitizedEnv()
	for i := len(env) - 1; i >= 0; i-- {
		key, _, _ := strings.Cut(env[i], "=")
		if strings.EqualFold(key, "GIT_ALLOW_PROTOCOL") {
			env = append(env[:i], env[i+1:]...)
		}
	}
	cmd.Env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_OBJECT_DIRECTORY="+p.objects, "GIT_INDEX_FILE="+p.index, "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=https")
	stdout := &limitBuffer{max: maxOutput, cancel: cancel}
	stderr := &limitBuffer{max: 1 << 20, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	hideWindow(cmd)
	err := cmd.Run()
	if stdout.truncated || stderr.truncated {
		return nil, errors.New("git: publication output exceeds review limit")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		// In particular, do not surface credential helper output or URLs containing
		// secrets. The original exit error preserves executable/exit diagnostics.
		return nil, fmt.Errorf("git: isolated publication command failed: %w", err)
	}
	return stdout.buf.Bytes(), nil
}

// PublicationSnapshot requires committed changes on the owned branch against a
// local ancestor branch. It never commits. Config-dependent filters, split/sparse
// indexes, and submodules are not supported: ambiguous cleanliness fails closed.
func PublicationSnapshot(ctx context.Context, dir, branch, base string) (Publication, error) {
	var result Publication
	head, err := publicationHead(ctx, dir, branch)
	if err != nil {
		return result, err
	}
	if err := ValidBranchName(ctx, base); err != nil {
		return result, err
	}
	baseRef := "refs/heads/" + base
	baseHead, err := publicationRead(ctx, dir, "rev-parse", "--verify", "--end-of-options", baseRef)
	if err != nil || !publicationSHA(baseHead) {
		return result, errors.New("git: publication base must be an existing local branch")
	}
	p, err := openPublicationRepo(ctx, dir, head)
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(p.dir)
	if err := p.clean(ctx); err != nil {
		return result, err
	}
	if _, err := p.run(ctx, DefaultTimeout, "merge-base", "--is-ancestor", baseHead, head); err != nil {
		return result, errors.New("git: publication base is not an ancestor of the owned branch")
	}
	diff, err := p.run(ctx, DefaultTimeout, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--binary", "--full-index", "--ignore-submodules=none", baseHead, head, "--")
	if err != nil {
		return result, err
	}
	if len(diff) == 0 {
		return result, errors.New("git: publication requires a nonempty committed diff")
	}
	current, err := publicationHead(ctx, dir, branch)
	if err != nil || current != head {
		return result, errors.New("git: publication branch changed during review")
	}
	currentBase, err := publicationRead(ctx, dir, "rev-parse", "--verify", "--end-of-options", baseRef)
	if err != nil || currentBase != baseHead {
		return result, errors.New("git: publication base changed during review")
	}
	if err := p.clean(ctx); err != nil {
		return result, err
	}
	return Publication{Head: head, BaseHead: baseHead, Diff: string(diff)}, nil
}

func (p *publicationRepo) clean(ctx context.Context) error {
	files, err := p.run(ctx, DefaultTimeout, "ls-files", "--stage", "-v", "-z")
	if err != nil {
		return err
	}
	for _, record := range strings.Split(string(files), "\x00") {
		if record == "" {
			continue
		}
		if record[0] != 'H' || strings.HasPrefix(record, "H 160000 ") {
			return errors.New("git: publication does not support submodules or hidden index entries")
		}
	}
	out, err := p.run(ctx, DefaultTimeout, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none", "--no-renames")
	if err != nil {
		return err
	}
	if len(out) != 0 {
		return errors.New("git: commit reviewed changes and clean the worktree before preparing publication")
	}
	return nil
}

func publicationRepoName(repo string) bool {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 100 || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.-", r)) {
				return false
			}
		}
	}
	return true
}

// PushPublication pushes exactly head to the owned branch, without force, hooks,
// tags, submodule pushes, URL rewrites, or repository-supplied commands. Credentials
// come only from the user's system/global Git credential configuration, as in
// Clone; signing in to the GitHub API alone does not configure Git credentials.
func PushPublication(ctx context.Context, dir, repo, branch, head string) error {
	if !publicationRepoName(repo) || !publicationSHA(head) {
		return errors.New("git: publication requires owner/repo and a full commit SHA")
	}
	current, err := publicationHead(ctx, dir, branch)
	if err != nil {
		return err
	}
	if current != head {
		return errors.New("git: approved publication head no longer matches the owned branch")
	}
	p, err := openPublicationRepo(ctx, dir, head)
	if err != nil {
		return err
	}
	defer os.RemoveAll(p.dir)
	if err := p.clean(ctx); err != nil {
		return err
	}
	args, err := publicationCredentials(ctx, p.dir)
	if err != nil {
		return err
	}
	current, err = publicationHead(ctx, dir, branch)
	if err != nil || current != head {
		return errors.New("git: approved publication branch changed before push")
	}
	args = append(args, "-c", "credential.interactive=false", "push", "--porcelain", "--no-verify", "--no-signed", "--no-follow-tags", "--recurse-submodules=no", "--", "https://github.com/"+repo+".git", head+":refs/heads/"+branch)
	if _, err := p.run(ctx, CloneTimeout, args...); err != nil {
		return fmt.Errorf("git: draft publication push failed; check Git credentials, repository write access, and branch protection (no force push attempted): %w", err)
	}
	return nil
}

func publicationCredentials(ctx context.Context, dir string) ([]string, error) {
	// Implicit config loading permits absent global/system files. Scope labels
	// retain provenance through includes; no local/worktree/command value is
	// allowed to install a helper. This command only reads configuration.
	out, truncated, err := run(ctx, DefaultTimeout, dir, "config", "--includes", "--null", "--show-scope", "--list")
	if truncated || err != nil {
		return nil, errors.New("git: could not read trusted Git credential configuration")
	}
	var args []string
	records := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(records); i += 2 {
		scope, record := records[i], records[i+1]
		if scope != "global" && scope != "system" {
			continue
		}
		key, value, _ := strings.Cut(record, "\n")
		if strings.HasPrefix(key, "credential.") {
			args = append(args, "-c", key+"="+value)
		}
	}
	return args, nil
}

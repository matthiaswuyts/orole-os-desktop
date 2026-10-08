package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// reviewRepo snapshots refs and the index into a disposable Git directory. Unlike
// a temporary index in the source repository, this cannot run source-configured
// clean/process filters. Only native normalization and attribute/ignore data are
// imported. Objects written while reviewing never touch the original repository.
type reviewRepo struct {
	dir        string
	root       string
	env        []string
	submodules []string
}

func reviewMetadata(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, truncated, err := runInput(ctx, DefaultTimeout, dir, []string{"GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL="}, nil, args...)
	if truncated {
		return nil, errors.New("git: review metadata exceeds output limit")
	}
	return out, err
}

func openReviewRepo(ctx context.Context, dir string) (_ *reviewRepo, err error) {
	paths := make(map[string]string, 4)
	for _, name := range []string{"objects", "HEAD", "info/exclude", "info/attributes"} {
		out, err := reviewMetadata(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", name)
		if err != nil {
			return nil, err
		}
		paths[name] = strings.TrimSpace(string(out))
	}
	root, err := Toplevel(ctx, dir)
	if err != nil {
		return nil, err
	}
	format, err := reviewMetadata(ctx, dir, "rev-parse", "--show-object-format")
	if err != nil {
		return nil, err
	}
	index, err := reviewMetadata(ctx, root, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	refs, err := reviewMetadata(ctx, root, "for-each-ref", "--format=%(objectname) %(refname)")
	if err != nil {
		return nil, err
	}
	config, err := reviewMetadata(ctx, root, "config", "--null", "--list")
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "orole-review-*")
	if err != nil {
		return nil, err
	}
	p := &reviewRepo{dir: tmp, root: root}
	defer func() {
		if err != nil {
			p.close()
		}
	}()
	for _, name := range []string{"objects/info", "refs", "info"} {
		if err := os.MkdirAll(filepath.Join(tmp, name), 0700); err != nil {
			return nil, err
		}
	}
	gitConfig := "[core]\n\trepositoryformatversion = 0\n\tbare = false\n"
	switch strings.TrimSpace(string(format)) {
	case "sha1":
	case "sha256":
		gitConfig = "[core]\n\trepositoryformatversion = 1\n\tbare = false\n[extensions]\n\tobjectformat = sha256\n"
	default:
		return nil, errors.New("git: unsupported repository object format")
	}
	if err := os.WriteFile(filepath.Join(tmp, "config"), []byte(gitConfig), 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "packed-refs"), refs, 0600); err != nil {
		return nil, err
	}
	for _, name := range []string{"HEAD", "info/exclude", "info/attributes"} {
		data, err := os.ReadFile(paths[name])
		if name != "HEAD" && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0600); err != nil {
			return nil, err
		}
	}
	objects := filepath.ToSlash(paths["objects"])
	if strings.ContainsAny(objects, "\r\n") {
		return nil, errors.New("git: repository object path contains a newline")
	}
	if err := os.WriteFile(filepath.Join(tmp, "objects", "info", "alternates"), []byte(objects+"\n"), 0600); err != nil {
		return nil, err
	}
	p.env = []string{
		"GIT_DIR=" + tmp, "GIT_WORK_TREE=" + root,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=",
	}
	for _, entry := range bytes.Split(config, []byte{0}) {
		key, value, ok := strings.Cut(string(entry), "\n")
		if !ok || !reviewConfigKey(key) {
			continue
		}
		if _, _, err := p.run(ctx, root, nil, "config", "--add", key, value); err != nil {
			return nil, err
		}
	}
	// Plumbing preserves staged ignored files, split indexes, gitlinks and modes.
	if _, _, err := p.run(ctx, root, bytes.NewReader(index), "update-index", "-z", "--index-info"); err != nil {
		return nil, err
	}
	for _, entry := range bytes.Split(index, []byte{0}) {
		meta, path, ok := strings.Cut(string(entry), "\t")
		if ok && strings.HasPrefix(meta, "160000 ") && strings.HasSuffix(meta, " 0") {
			p.submodules = append(p.submodules, path)
		}
	}
	return p, nil
}

func reviewConfigKey(key string) bool {
	key = strings.ToLower(key)
	switch key {
	case "core.autocrlf", "core.eol", "core.safecrlf", "core.filemode", "core.symlinks", "core.ignorecase", "core.excludesfile", "core.attributesfile":
		return true
	}
	// Status needs upstream mapping, but never remote URLs, credential helpers,
	// aliases, includes, hooks, filters or command-valued diff drivers.
	return strings.HasPrefix(key, "branch.") && (strings.HasSuffix(key, ".remote") || strings.HasSuffix(key, ".merge")) ||
		strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".fetch") ||
		strings.HasPrefix(key, "diff.") && strings.HasSuffix(key, ".binary")
}

func (p *reviewRepo) close() { os.RemoveAll(p.dir) }

func (p *reviewRepo) run(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, bool, error) {
	return runInput(ctx, DefaultTimeout, dir, p.env, stdin, args...)
}

// Ask each initialized submodule through the same isolated boundary instead of
// letting Git spawn status against a nested repository's executable config.
func (p *reviewRepo) dirtySubmodules(ctx context.Context, depth int) ([]string, error) {
	var dirty []string
	for _, path := range p.submodules {
		dir := filepath.Join(p.root, filepath.FromSlash(path))
		if _, err := os.Lstat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		status, err := getStatus(ctx, dir, depth+1)
		if err != nil {
			return nil, err
		}
		if status.Dirty > 0 {
			dirty = append(dirty, path)
		}
	}
	return dirty, nil
}

func (p *reviewRepo) addDirtySubmoduleDiff(ctx context.Context, dir string, options DiffOptions, result *DiffResult) error {
	if options.Staged || options.To != "" {
		return nil
	}
	dirty, err := p.dirtySubmodules(ctx, 0)
	if err != nil {
		return err
	}
	for _, path := range dirty {
		if options.Path != "" {
			selected, err := filepath.Rel(p.root, filepath.Join(dir, options.Path))
			if err != nil {
				return err
			}
			selected = filepath.ToSlash(selected)
			if selected != "." && path != selected && !strings.HasPrefix(path, strings.TrimSuffix(selected, "/")+"/") {
				continue
			}
		}
		found := false
		for i := range result.Files {
			file := &result.Files[i]
			if file.Path != path {
				continue
			}
			found = true
			for h := range file.Hunks {
				for l := range file.Hunks[h].Lines {
					line := &file.Hunks[h].Lines[l]
					if line.Kind == "+" && strings.HasPrefix(line.Text, "Subproject commit ") {
						line.Text += "-dirty"
					}
				}
			}
			break
		}
		if found {
			continue
		}
		args := []string{"ls-files", "--stage", "-z", "--", ":(literal)" + path}
		if options.From != "" {
			args = []string{"ls-tree", "-z", options.From, "--", ":(literal)" + path}
		}
		out, truncated, err := p.run(ctx, p.root, nil, args...)
		if err != nil {
			return err
		}
		if truncated {
			return errors.New("git: submodule metadata exceeds review limit")
		}
		meta, _, _ := strings.Cut(string(out), "\t")
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[0] != "160000" {
			return errors.New("git: cannot review dirty submodule base")
		}
		old := fields[1]
		if options.From != "" {
			old = fields[2]
		}
		head, err := reviewMetadata(ctx, filepath.Join(p.root, path), "rev-parse", "--verify", "HEAD")
		if err != nil {
			return err
		}
		result.Files = append(result.Files, DiffFile{
			Path: path, Status: "modified", Additions: 1, Deletions: 1,
			Hunks: []DiffHunk{{
				Header: "@@ -1 +1 @@", OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
				Lines: []DiffLine{
					{Kind: "-", Text: "Subproject commit " + old, Old: 1},
					{Kind: "+", Text: "Subproject commit " + strings.TrimSpace(string(head)) + "-dirty", New: 1},
				},
			}},
		})
		result.Additions++
		result.Deletions++
	}
	return nil
}

// DiffWorktree reviews all changes against base, including untracked files and
// commits, without mutating the source index or executing repository commands.
func DiffWorktree(ctx context.Context, dir, base string) (DiffResult, error) {
	if !ValidRef(base) {
		return DiffResult{}, ErrInvalidRef
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	p, err := openReviewRepo(ctx, dir)
	if err != nil {
		return DiffResult{}, err
	}
	defer p.close()
	if _, _, err := p.run(ctx, p.root, nil, "add", "-A", "--", "."); err != nil {
		return DiffResult{}, err
	}
	out, truncated, err := p.run(ctx, p.root, nil, "diff", "--cached", "--no-color", "--no-ext-diff", "--no-textconv", "--ignore-submodules=dirty", "-M", "--src-prefix=a/", "--dst-prefix=b/", base, "--")
	if err != nil {
		return DiffResult{}, err
	}
	result := ParseDiff(string(out))
	result.Truncated = truncated
	return result, nil
}

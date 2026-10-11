package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// reviewRepo snapshots refs and the index into a disposable Git directory. Unlike
// a temporary index in the source repository, this cannot run source-configured
// clean/process filters. Only native normalization and attribute/ignore data are
// imported. Objects written while reviewing never touch the original repository.
type reviewRepo struct {
	dir        string
	root       string
	prefix     string // dir's path below root, slash-separated with a trailing slash ("" at root)
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

// openReviewRepo builds the disposable repository for dir. withIndex copies the
// source index (and any split-index base files) so git reuses its stat cache;
// ref-to-ref diffs pass false because they never read the index. Spawns: one
// rev-parse, then for-each-ref and config (plus ls-files for gitlinks) in
// parallel; everything else is plain file writes.
func openReviewRepo(ctx context.Context, dir string, withIndex bool) (_ *reviewRepo, err error) {
	// Git prints one value per argument, in order, so the answer is positional.
	meta, err := reviewMetadata(ctx, dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--show-prefix", "--show-object-format",
		"--git-path", "objects", "--git-path", "HEAD", "--git-path", "info/exclude", "--git-path", "info/attributes", "--git-path", "index")
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && strings.Contains(ge.Stderr, "not a git repository") {
			return nil, ErrNotRepo
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(meta), "\r\n"), "\n")
	if len(lines) != 8 {
		return nil, errors.New("git: repository path contains a newline or rev-parse output is malformed")
	}
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	root, prefix, format := lines[0], lines[1], lines[2]
	objectsPath, headPath, excludePath, attributesPath, indexPath := lines[3], lines[4], lines[5], lines[6], lines[7]
	if root == "" {
		return nil, errors.New("git: cannot review a repository without a work tree")
	}
	switch format {
	case "sha1", "sha256":
	default:
		return nil, errors.New("git: unsupported repository object format")
	}

	var refs, config, index []byte
	var refsErr, configErr, indexErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		refs, refsErr = reviewMetadata(ctx, root, "for-each-ref", "--format=%(objectname) %(refname)")
	}()
	go func() {
		defer wg.Done()
		config, configErr = reviewMetadata(ctx, root, "config", "--null", "--list")
	}()
	if withIndex {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Only used to find gitlinks; the index itself is copied as a file.
			index, indexErr = reviewMetadata(ctx, root, "ls-files", "--stage", "-z")
		}()
	}
	wg.Wait()
	for _, e := range []error{refsErr, configErr, indexErr} {
		if e != nil {
			return nil, e
		}
	}

	tmp, err := os.MkdirTemp("", "orole-review-*")
	if err != nil {
		return nil, err
	}
	p := &reviewRepo{dir: tmp, root: root, prefix: prefix}
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
	var gitConfig strings.Builder
	if format == "sha256" {
		gitConfig.WriteString("[core]\n\trepositoryformatversion = 1\n\tbare = false\n[extensions]\n\tobjectformat = sha256\n")
	} else {
		gitConfig.WriteString("[core]\n\trepositoryformatversion = 0\n\tbare = false\n")
	}
	writeReviewConfig(&gitConfig, config)
	if err := os.WriteFile(filepath.Join(tmp, "config"), []byte(gitConfig.String()), 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "packed-refs"), refs, 0600); err != nil {
		return nil, err
	}
	for name, src := range map[string]string{"HEAD": headPath, "info/exclude": excludePath, "info/attributes": attributesPath} {
		data, err := os.ReadFile(src)
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
	objects := filepath.ToSlash(objectsPath)
	if err := os.WriteFile(filepath.Join(tmp, "objects", "info", "alternates"), []byte(objects+"\n"), 0600); err != nil {
		return nil, err
	}
	// A copy, never the real file: `git diff` refreshes and rewrites the index it
	// reads even with GIT_OPTIONAL_LOCKS=0, and DiffWorktree stages into it.
	indexFile := filepath.Join(tmp, "index")
	if withIndex {
		if err := copyReviewIndex(indexPath, tmp); err != nil {
			return nil, err
		}
	}
	p.env = []string{
		"GIT_DIR=" + tmp, "GIT_WORK_TREE=" + root, "GIT_INDEX_FILE=" + indexFile,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=",
	}
	for rest := index; len(rest) > 0; {
		var entry []byte
		if i := bytes.IndexByte(rest, 0); i >= 0 {
			entry, rest = rest[:i], rest[i+1:]
		} else {
			entry, rest = rest, nil
		}
		if bytes.HasPrefix(entry, []byte("160000 ")) {
			if meta, path, ok := strings.Cut(string(entry), "\t"); ok && strings.HasSuffix(meta, " 0") {
				p.submodules = append(p.submodules, path)
			}
		}
	}
	return p, nil
}

// copyReviewIndex copies the index and the shared base files a split index links
// to into the disposable git dir. A missing index (unborn repository) is empty.
func copyReviewIndex(indexPath, tmp string) error {
	dst := filepath.Join(tmp, "index")
	if err := copyFile(indexPath, dst); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else if err := copyIndexTimes(indexPath, dst); err != nil {
		return err
	}
	shared, err := filepath.Glob(filepath.Join(filepath.Dir(indexPath), "sharedindex.*"))
	if err != nil {
		return err
	}
	for _, src := range shared {
		if err := copyFile(src, filepath.Join(tmp, filepath.Base(src))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

// copyIndexTimes gives the copied index the source's mtime. Git treats an entry
// whose mtime is not older than the index file's as racily clean and compares
// content; a fresh copy mtime would silently turn those entries into stat-clean.
func copyIndexTimes(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// writeReviewConfig appends the allow-listed entries of `git config --null --list`
// output to a git config file. Values are always quoted and escaped, so nothing in
// a source value can start a new section or key.
func writeReviewConfig(b *strings.Builder, list []byte) {
	for _, entry := range bytes.Split(list, []byte{0}) {
		key, value, ok := strings.Cut(string(entry), "\n")
		if !ok || !reviewConfigKey(key) {
			continue
		}
		first, last := strings.IndexByte(key, '.'), strings.LastIndexByte(key, '.')
		if first <= 0 || last == len(key)-1 {
			continue
		}
		section, name := key[:first], key[last+1:]
		if first == last {
			b.WriteString("[" + section + "]\n")
		} else {
			sub := key[first+1 : last]
			if strings.ContainsAny(sub, "\n\r") {
				continue
			}
			b.WriteString("[" + section + " \"" + subsectionEscaper.Replace(sub) + "\"]\n")
		}
		b.WriteString("\t" + name + " = \"" + configEscaper.Replace(value) + "\"\n")
	}
}

// Subsection names only escape backslash and quote; other escapes are not interpreted there.
var subsectionEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

var configEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\b", `\b`)

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
// Only submodules under the slash-terminated repository-relative prefix are asked.
func (p *reviewRepo) dirtySubmodules(ctx context.Context, depth int, under string) ([]string, error) {
	var dirty []string
	for _, path := range p.submodules {
		if !strings.HasPrefix(path, under) {
			continue
		}
		dir := filepath.Join(p.root, filepath.FromSlash(path))
		if _, err := os.Lstat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		status, _, err := getStatus(ctx, dir, depth+1, false)
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
	dirty, err := p.dirtySubmodules(ctx, 0, "")
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
	p, err := openReviewRepo(ctx, dir, true)
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

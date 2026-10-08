package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Local and git may independently approve the same physical path. Sharing the
// mutation lock prevents lost updates across those providers; roots and access
// checks remain strictly separate.
var filesystemMutations sync.Mutex

// RootSource supplies the approved roots of one provider. Implementations must
// return only roots approved for that provider.
type RootSource interface {
	Roots(ctx context.Context) ([]Root, error)
}

// RootFunc adapts a function to RootSource.
type RootFunc func(ctx context.Context) ([]Root, error)

// Roots implements RootSource.
func (f RootFunc) Roots(ctx context.Context) ([]Root, error) { return f(ctx) }

// StaticRoots is a fixed root list (tests, config).
func StaticRoots(provider string, paths ...string) RootSource {
	return RootFunc(func(context.Context) ([]Root, error) {
		out := make([]Root, len(paths))
		for i, p := range paths {
			out[i] = Root{ID: "static-" + strconv.Itoa(i), Provider: provider, Path: p, Label: filepath.Base(p)}
		}
		return out, nil
	})
}

// Local is a provider over the local filesystem restricted to approved roots.
// The "git" provider is a Local with a different root source and hooks.
type Local struct {
	id  string
	src RootSource
	// guard, when set, may veto mutations of a cleaned absolute path.
	guard func(clean string) error
	// annotate, when set, decorates listings of a directory.
	annotate func(ctx context.Context, dirReal string, entries []Entry)
	// rootCheck, when set, validates that a root is acceptable for this provider.
	rootCheck func(real string) error
}

// NewLocal returns the "local" provider over src.
func NewLocal(src RootSource) *Local { return &Local{id: "local", src: src} }

// ID implements Provider.
func (l *Local) ID() string { return l.id }

type resolved struct {
	display  string // cleaned lexical path as the caller named it
	real     string // fully resolved location (final component unresolved when !follow)
	root     Root
	rootReal string
	isRoot   bool
}

// CanonicalRoot validates a directory for use as a root and returns its
// resolved path. It is used when the user approves a root.
func CanonicalRoot(p string) (string, error) {
	clean, err := validateRaw(p)
	if err != nil {
		return "", err
	}
	real, err := realPath(clean)
	if err != nil {
		return "", mapErr(err, clean)
	}
	fi, err := os.Stat(real)
	if err != nil {
		return "", mapErr(err, clean)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrNotDir, clean)
	}
	return real, nil
}

// Roots implements Provider: roots with their current health.
func (l *Local) Roots(ctx context.Context) ([]Root, error) {
	roots, err := l.src.Roots(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Root, 0, len(roots))
	for _, r := range roots {
		r.Provider = l.id
		if _, err := l.rootReal(r); err != nil {
			r.Missing, r.Problem = true, err.Error()
		}
		out = append(out, r)
	}
	return out, nil
}

// rootReal verifies a root still resolves to the location that was approved.
func (l *Local) rootReal(r Root) (string, error) {
	real, err := realPath(r.Path)
	if err != nil {
		return "", fmt.Errorf("%w: root %s is not accessible: %v", ErrDenied, r.Path, err)
	}
	if !samePath(real, r.Path) {
		return "", fmt.Errorf("%w: root %s now resolves to %s; re-approve it", ErrDenied, r.Path, real)
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%w: root %s is not a directory", ErrDenied, r.Path)
	}
	if l.rootCheck != nil {
		if err := l.rootCheck(real); err != nil {
			return "", err
		}
	}
	return real, nil
}

// resolve applies the path policy. With follow, the final component is
// resolved too (reading through a link); without it the final component is
// kept as-is so links can be renamed or removed themselves.
func (l *Local) resolve(ctx context.Context, p string, follow bool) (resolved, error) {
	clean, err := validateRaw(p)
	if err != nil {
		return resolved{}, err
	}
	roots, err := l.src.Roots(ctx)
	if err != nil {
		return resolved{}, err
	}
	var best Root
	var real string
	found := false
	for _, r := range roots {
		if within(r.Path, clean) && (!found || len(r.Path) > len(best.Path)) {
			best, found = r, true
		}
	}
	if !found && len(roots) > 0 {
		// Approval stores a canonical path, while callers may still use an
		// ancestor junction or an 8.3 name (including Windows' temp directory).
		// Resolve that alias before looking up a root, never instead of checking
		// containment or validating that the approved root itself is unchanged.
		real, err = resolvePath(clean, follow)
		if err == nil {
			for _, r := range roots {
				if within(r.Path, real) && (!found || len(r.Path) > len(best.Path)) {
					best, found = r, true
				}
			}
		}
	}
	if !found {
		return resolved{}, ErrRootNotApproved
	}
	rootReal, err := l.rootReal(best)
	if err != nil {
		return resolved{}, err
	}
	res := resolved{display: clean, real: real, root: best, rootReal: rootReal}
	if samePath(clean, best.Path) {
		res.real, res.isRoot = rootReal, true
		return res, nil
	}
	if res.real == "" {
		res.real, err = resolvePath(clean, follow)
	}
	if err != nil {
		return resolved{}, mapErr(err, clean)
	}
	if !within(rootReal, res.real) {
		return resolved{}, fmt.Errorf("%w: %s resolves outside its root (symlink or junction)", ErrDenied, clean)
	}
	res.isRoot = samePath(res.real, rootReal)
	return res, nil
}

func resolvePath(clean string, follow bool) (string, error) {
	if follow {
		return evalExisting(clean)
	}
	parent, err := realPath(filepath.Dir(clean))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(clean)), nil
}

func evalExisting(p string) (string, error) {
	real, err := realPath(p)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", err
	}
	pr, perr := evalExisting(parent)
	if perr != nil {
		return "", perr
	}
	return filepath.Join(pr, filepath.Base(p)), nil
}

func mapErr(err error, path string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrDenied), errors.Is(err, ErrInvalidPath), errors.Is(err, ErrExists):
		return err
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s", ErrNotFound, path)
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%w: %s", ErrExists, path)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %s: %v", ErrDenied, path, err)
	}
	return err
}

func (l *Local) lock(_ string) func() {
	filesystemMutations.Lock()
	return filesystemMutations.Unlock
}

func isLinkMode(m fs.FileMode) bool { return m&(fs.ModeSymlink|fs.ModeIrregular) != 0 }

func entryOf(display string, fi fs.FileInfo, link bool, targetDir *bool) Entry {
	e := Entry{Name: fi.Name(), Path: display, IsDir: fi.IsDir(), Size: fi.Size(), ModTime: fi.ModTime(), Link: link,
		ReadOnly: !fi.IsDir() && fi.Mode().Perm()&0o200 == 0}
	if link && targetDir != nil {
		e.IsDir = *targetDir
	}
	e.Hidden = strings.HasPrefix(fi.Name(), ".") || platformHidden(fi)
	return e
}

// List implements Provider.
func (l *Local) List(ctx context.Context, path string) ([]Entry, error) {
	r, err := l.resolve(ctx, path, true)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(r.real)
	if err != nil {
		return nil, mapErr(err, r.display)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotDir, r.display)
	}
	des, err := os.ReadDir(r.real)
	if err != nil {
		return nil, mapErr(err, r.display)
	}
	if len(des) > MaxListEntries {
		return nil, fmt.Errorf("%w: directory has %d entries (limit %d)", ErrTooLarge, len(des), MaxListEntries)
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		info, err := de.Info()
		if err != nil {
			continue // vanished between ReadDir and Info
		}
		display := filepath.Join(r.display, de.Name())
		link := isLinkMode(info.Mode())
		var td *bool
		if link {
			// Follow only to learn whether it is a directory; the policy is applied
			// again when the link is actually opened.
			b := false
			if t, err := os.Stat(filepath.Join(r.real, de.Name())); err == nil {
				b = t.IsDir()
			}
			td = &b
		}
		out = append(out, entryOf(display, info, link, td))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if l.annotate != nil {
		l.annotate(ctx, r.real, out)
	}
	return out, nil
}

// Stat implements Provider.
func (l *Local) Stat(ctx context.Context, path string) (Info, error) {
	r, err := l.resolve(ctx, path, true)
	if err != nil {
		return Info{}, err
	}
	return l.statResolved(r)
}

func (l *Local) statResolved(r resolved) (Info, error) {
	fi, err := os.Stat(r.real)
	if err != nil {
		return Info{}, mapErr(err, r.display)
	}
	link := false
	if lfi, err := os.Lstat(r.display); err == nil {
		link = isLinkMode(lfi.Mode())
	}
	e := entryOf(r.display, fi, link, nil)
	e.Name = filepath.Base(r.display)
	info := Info{Entry: e, Root: r.root.ID}
	if !fi.IsDir() {
		info.Etag = computeEtag(r.real, fi)
	}
	return info, nil
}

// Read implements Provider.
func (l *Local) Read(ctx context.Context, path string, opt ReadOptions) (ReadResult, error) {
	r, err := l.resolve(ctx, path, true)
	if err != nil {
		return ReadResult{}, err
	}
	f, err := os.Open(r.real)
	if err != nil {
		return ReadResult{}, mapErr(err, r.display)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ReadResult{}, mapErr(err, r.display)
	}
	if fi.IsDir() {
		return ReadResult{}, fmt.Errorf("%w: %s", ErrIsDir, r.display)
	}
	if !fi.Mode().IsRegular() {
		return ReadResult{}, fmt.Errorf("%w: %s is not a regular file", ErrUnsupportedOp, r.display)
	}
	max := opt.MaxBytes
	if max <= 0 || max > MaxReadBytes {
		max = MaxReadBytes
	}
	size := fi.Size()
	if opt.Offset < 0 || opt.Length < 0 {
		return ReadResult{}, fmt.Errorf("%w: negative range", ErrInvalidPath)
	}
	want := opt.Length
	whole := want == 0 && opt.Offset == 0
	if want == 0 {
		want = size - opt.Offset
		if want < 0 {
			want = 0
		}
		if want > max {
			return ReadResult{}, fmt.Errorf("%w: %s is %d bytes (limit %d); read a range instead", ErrTooLarge, r.display, size, max)
		}
	}
	if want > max {
		want = max
	}
	if opt.Offset > 0 {
		if _, err := f.Seek(opt.Offset, io.SeekStart); err != nil {
			return ReadResult{}, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, want))
	if err != nil {
		return ReadResult{}, err
	}
	res := ReadResult{Data: data, Offset: opt.Offset, Size: size, Truncated: opt.Offset+int64(len(data)) < size}
	e := entryOf(r.display, fi, false, nil)
	e.Name = filepath.Base(r.display)
	res.Info = Info{Entry: e, Root: r.root.ID}
	if whole && int64(len(data)) == size {
		res.Info.Etag = etagFor(fi.ModTime().UnixNano(), size, sha256.Sum256(data))
	} else {
		res.Info.Etag = computeEtag(r.real, fi)
	}
	return res, nil
}

func etagFor(mtimeNano, size int64, sum [32]byte) string {
	return "v1:" + strconv.FormatInt(mtimeNano, 10) + ":" + strconv.FormatInt(size, 10) + ":" + hex.EncodeToString(sum[:])
}

func computeEtag(real string, fi fs.FileInfo) string {
	prefix := "v1:" + strconv.FormatInt(fi.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(fi.Size(), 10) + ":"
	if fi.Size() > MaxHashBytes {
		return prefix
	}
	f, err := os.Open(real)
	if err != nil {
		return prefix
	}
	defer f.Close()
	h := sha256.New()
	if n, err := io.Copy(h, io.LimitReader(f, MaxHashBytes+1)); err != nil || n != fi.Size() {
		return prefix
	}
	return prefix + hex.EncodeToString(h.Sum(nil))
}

type etag struct {
	mtime, size int64
	sum         string
}

func parseEtag(s string) (etag, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 4 || parts[0] != "v1" {
		return etag{}, false
	}
	m, err1 := strconv.ParseInt(parts[1], 10, 64)
	sz, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil {
		return etag{}, false
	}
	return etag{m, sz, parts[3]}, true
}

// stale reports whether the file described by cur differs from the version
// expected. A changed size is always a conflict; with hashes on both sides
// equal content is not (a touch does not conflict); otherwise mtimes decide.
func (want etag) stale(cur etag) bool {
	if want.size != cur.size {
		return true
	}
	if want.sum != "" && cur.sum != "" {
		return want.sum != cur.sum
	}
	return want.mtime != cur.mtime
}

// Write implements Provider.
func (l *Local) Write(ctx context.Context, path string, data []byte, opt WriteOptions) (Info, error) {
	if int64(len(data)) > MaxReadBytes {
		return Info{}, fmt.Errorf("%w: %d bytes exceeds the %d byte write limit", ErrTooLarge, len(data), MaxReadBytes)
	}
	// Resolution opens native handles too; keep concurrent writers out for the
	// entire resolve/check/replace transaction, not just the final ETag check.
	unlock := l.lock(path)
	defer unlock()
	r, err := l.resolve(ctx, path, true)
	if err != nil {
		return Info{}, err
	}
	if r.isRoot {
		return Info{}, fmt.Errorf("%w: %s is a root directory", ErrIsDir, r.display)
	}
	if err := l.guardMutation(r.display, r.real); err != nil {
		return Info{}, err
	}

	fi, err := os.Stat(r.real)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Info{}, mapErr(err, r.display)
	}
	if exists {
		if fi.IsDir() {
			return Info{}, fmt.Errorf("%w: %s", ErrIsDir, r.display)
		}
		if !fi.Mode().IsRegular() {
			return Info{}, fmt.Errorf("%w: target is not a regular file", ErrUnsupportedOp)
		}
		if opt.Create {
			return Info{}, fmt.Errorf("%w: %s", ErrExists, r.display)
		}
		if fi.Mode().Perm()&0o200 == 0 {
			return Info{}, fmt.Errorf("%w: %s has the read-only attribute", ErrReadOnly, r.display)
		}
		if !opt.Force {
			want, ok := parseEtag(opt.IfMatch)
			if !ok {
				return Info{}, ErrNeedsEtag
			}
			cur, _ := parseEtag(computeEtag(r.real, fi))
			if want.stale(cur) {
				ci, _ := l.statResolved(r)
				return Info{}, &ConflictError{Path: r.display, Current: ci}
			}
		}
	} else {
		if opt.IfMatch != "" && !opt.Force {
			return Info{}, &ConflictError{Path: r.display, Deleted: true}
		}
		if !opt.Create && !opt.Force {
			return Info{}, fmt.Errorf("%w: %s (use create to make a new file)", ErrNotFound, r.display)
		}
		if st, err := os.Stat(filepath.Dir(r.real)); err != nil || !st.IsDir() {
			return Info{}, fmt.Errorf("%w: parent of %s", ErrNotFound, r.display)
		}
	}
	if err := atomicWrite(r.real, data, fi); err != nil {
		return Info{}, mapErr(err, r.display)
	}
	return l.statResolved(r)
}

func atomicWrite(real string, data []byte, old fs.FileInfo) error {
	dir := filepath.Dir(real)
	tmp, err := os.CreateTemp(dir, ".orole-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if old != nil {
		if err := os.Chmod(name, old.Mode().Perm()); err != nil {
			os.Remove(name)
			return err
		}
	}
	if err := os.Rename(name, real); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func (l *Local) guardMutation(display, real string) error {
	if l.guard == nil {
		return nil
	}
	if err := l.guard(display); err != nil {
		return err
	}
	return l.guard(real)
}

// Mkdir implements Provider.
func (l *Local) Mkdir(ctx context.Context, path string) (Info, error) {
	r, err := l.resolve(ctx, path, false)
	if err != nil {
		return Info{}, err
	}
	if r.isRoot {
		return Info{}, fmt.Errorf("%w: %s", ErrExists, r.display)
	}
	if err := l.guardMutation(r.display, r.real); err != nil {
		return Info{}, err
	}
	if err := os.Mkdir(r.real, 0o755); err != nil {
		return Info{}, mapErr(err, r.display)
	}
	return l.statResolved(r)
}

// Rename implements Provider.
func (l *Local) Rename(ctx context.Context, from, to string) (Info, error) {
	unlock := l.lock(from)
	defer unlock()
	src, err := l.resolve(ctx, from, false)
	if err != nil {
		return Info{}, err
	}
	dst, err := l.resolve(ctx, to, false)
	if err != nil {
		return Info{}, err
	}
	if src.isRoot || dst.isRoot {
		return Info{}, fmt.Errorf("%w: a root directory cannot be renamed or replaced", ErrDenied)
	}
	for _, r := range []resolved{src, dst} {
		if err := l.guardMutation(r.display, r.real); err != nil {
			return Info{}, err
		}
	}
	sfi, err := os.Lstat(src.real)
	if err != nil {
		return Info{}, mapErr(err, src.display)
	}
	if sfi.IsDir() && within(src.real, dst.real) && !samePath(src.real, dst.real) {
		return Info{}, fmt.Errorf("%w: cannot move %s into itself", ErrInvalidPath, src.display)
	}
	if dfi, err := os.Lstat(dst.real); err == nil {
		// Only a case-only rename may land on an existing entry: itself.
		if !os.SameFile(sfi, dfi) {
			return Info{}, fmt.Errorf("%w: %s", ErrExists, dst.display)
		}
	}
	if err := os.Rename(src.real, dst.real); err != nil {
		return Info{}, mapErr(err, src.display)
	}
	if isLinkMode(sfi.Mode()) {
		info, err := os.Lstat(dst.real)
		if err != nil {
			return Info{}, mapErr(err, dst.display)
		}
		return Info{Entry: entryOf(dst.display, info, true, nil), Root: dst.root.ID}, nil
	}
	return l.statResolved(dst)
}

// Delete implements Provider.
func (l *Local) Delete(ctx context.Context, path string, opt DeleteOptions) (DeleteResult, error) {
	unlock := l.lock(path)
	defer unlock()
	r, err := l.resolve(ctx, path, false)
	if err != nil {
		return DeleteResult{}, err
	}
	if r.isRoot {
		return DeleteResult{}, fmt.Errorf("%w: a root directory cannot be deleted; remove the root instead", ErrDenied)
	}
	if err := l.guardMutation(r.display, r.real); err != nil {
		return DeleteResult{}, err
	}
	fi, err := os.Lstat(r.real)
	if err != nil {
		return DeleteResult{}, mapErr(err, r.display)
	}
	if opt.Permanent && isLinkMode(fi.Mode()) {
		// Remove only the link, never what it points at.
		if err := os.Remove(r.real); err != nil {
			return DeleteResult{}, mapErr(err, r.display)
		}
		return DeleteResult{}, nil
	}
	if !opt.Permanent {
		if err := recycle(r.real); err != nil {
			return DeleteResult{}, err
		}
		return DeleteResult{Recycled: true}, nil
	}
	if fi.IsDir() {
		err = os.RemoveAll(r.real)
	} else {
		err = os.Remove(r.real)
	}
	if err != nil {
		return DeleteResult{}, mapErr(err, r.display)
	}
	return DeleteResult{}, nil
}

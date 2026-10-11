package machines

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

type FileRequest struct {
	MachineID string `json:"machineId"`
	Path      string `json:"path"`
}
type FileReadRequest struct {
	MachineID string            `json:"machineId"`
	Path      string            `json:"path"`
	Options   files.ReadOptions `json:"options"`
}
type FileWriteRequest struct {
	MachineID string             `json:"machineId"`
	Path      string             `json:"path"`
	Data      []byte             `json:"data"`
	Options   files.WriteOptions `json:"options"`
}

func remotePath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.ContainsAny(p, "\x00\r\n") || strings.HasPrefix(p, "//") {
		return "", files.ErrInvalidPath
	}
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		p = "/" + p
	}
	if !strings.HasPrefix(p, "/") {
		return "", files.ErrInvalidPath
	}
	return path.Clean(p), nil
}
func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
func (m *Manager) canonicalRoots(ctx context.Context, v Machine, roots []string) ([]string, error) {
	if len(roots) > 32 {
		return nil, ErrInvalid
	}
	total := 0
	for _, root := range roots {
		total += len(root)
		if !approvalText(root) || total > maxApprovalDetail {
			return nil, fmt.Errorf("%w: roots cannot be shown completely in approval", ErrInvalid)
		}
	}
	out := []string{}
	if len(roots) == 0 {
		return out, nil
	}
	if v.ID == "local" {
		for _, r := range roots {
			r, err := files.CanonicalRoot(r)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, nil
	}
	ctx, cancel := operationContext(ctx, 20)
	defer cancel()
	lease, err := m.pool.acquire(ctx, v)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	fs := lease.FS
	for _, r := range roots {
		r, err = remotePath(r)
		if err != nil {
			return nil, err
		}
		r, err = fs.RealPath(r)
		if err != nil {
			return nil, err
		}
		r, err = remotePath(r)
		if err != nil {
			return nil, err
		}
		info, err := fs.Stat(r)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, files.ErrNotDir
		}
		out = append(out, r)
	}
	return out, nil
}
func localProvider(v Machine) *files.Local {
	return files.NewLocal(files.RootFunc(func(context.Context) ([]files.Root, error) {
		out := []files.Root{}
		for i, p := range v.Roots {
			out = append(out, files.Root{ID: fmt.Sprint(i), Provider: "machine:" + v.ID, Path: p, Label: filepath.Base(p)})
		}
		return out, nil
	}))
}
func (m *Manager) fileSession(ctx context.Context, actor string, p FileRequest, write bool) (op *accessOperation, fs *sftp.Client, close func(), err error) {
	op, err = m.beginAccess(ctx, p.MachineID)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		if err != nil {
			err = op.result(err)
			op.finish()
		}
	}()
	v := op.machine
	effect := policy.EffectRead
	action := "files.read"
	if write {
		if !v.AllowWrite {
			return op, nil, nil, ErrDenied
		}
		effect = policy.EffectWrite
		action = "files.write"
	}
	if !approvalText(p.Path) {
		return op, nil, nil, ErrInvalid
	}
	identity, err := machineIdentity(v)
	if err != nil {
		return op, nil, nil, err
	}
	detail, err := approvalDetail(struct {
		Target approvalIdentity `json:"target"`
		Path   string           `json:"path"`
	}{identity, p.Path})
	if err != nil {
		return op, nil, nil, err
	}
	if err = m.authorize(op.ctx, actor, action, v.ID, effect, detail); err != nil {
		return op, nil, nil, err
	}
	if err = context.Cause(op.ctx); err != nil {
		return op, nil, nil, err
	}
	if v.ID == "local" {
		return op, nil, op.finish, nil
	}
	ctx, cancel := operationContext(op.ctx, 30)
	lease, err := m.pool.acquire(ctx, v)
	if err != nil {
		cancel()
		return op, nil, nil, err
	}
	return op, lease.FS, func() { lease.Release(); cancel(); op.finish() }, nil
}

// Remote servers are trusted endpoints, not sandboxes. Refuse symlinks and check
// canonical paths to prevent accidental escapes; account ACLs remain authoritative.
//
// Every server query below is independent of the others' results, so they are
// issued together over the shared SFTP connection and cost one round trip of
// latency instead of one per path component. The decisions are then taken in
// the original order, so the outcome (including which error wins) is unchanged.
func checkedRemote(fs *sftp.Client, v Machine, input string, create bool) (string, error) {
	p, err := remotePath(input)
	if err != nil {
		return "", err
	}
	root := ""
	for _, r := range v.Roots {
		if within(r, p) && len(r) > len(root) {
			root = r
		}
	}
	if root == "" {
		return "", files.ErrRootNotApproved
	}
	parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(p, root), "/"), "/")
	var (
		rootReal, targetReal, parentReal string
		rootErr, targetErr, parentErr    error
		statErr                          error
		lstat                            = make([]os.FileInfo, len(parts))
		lstatErr                         = make([]error, len(parts))
		wg                               sync.WaitGroup
		slots                            = make(chan struct{}, 16)
	)
	run := func(f func()) {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			f()
		}()
	}
	run(func() { rootReal, rootErr = fs.RealPath(root) })
	current := root
	for i, part := range parts {
		if part == "" {
			continue
		}
		current = path.Join(current, part)
		at := current
		run(func() { lstat[i], lstatErr[i] = fs.Lstat(at) })
	}
	run(func() { targetReal, targetErr = fs.RealPath(p) })
	if create {
		run(func() { _, statErr = fs.Stat(p) })
		run(func() { parentReal, parentErr = fs.RealPath(path.Dir(p)) })
	}
	wg.Wait()
	if rootErr != nil {
		return "", rootErr
	}
	rootReal, err = remotePath(rootReal)
	if err != nil || rootReal != root {
		return "", files.ErrRootNotApproved
	}
	for i, part := range parts {
		if part == "" {
			continue
		}
		if create && i == len(parts)-1 && os.IsNotExist(lstatErr[i]) {
			break
		}
		if lstatErr[i] != nil {
			return "", lstatErr[i]
		}
		if lstat[i].Mode()&os.ModeSymlink != 0 {
			return "", files.ErrRootNotApproved
		}
	}
	real, realErr := targetReal, targetErr
	if create && os.IsNotExist(statErr) {
		real, realErr = parentReal, parentErr
	}
	if realErr != nil {
		return "", realErr
	}
	real, err = remotePath(real)
	if err != nil || !within(root, real) {
		return "", files.ErrRootNotApproved
	}
	return p, nil
}
func entry(p string, st os.FileInfo) files.Entry {
	return files.Entry{Name: st.Name(), Path: p, IsDir: st.IsDir(), Size: st.Size(), ModTime: st.ModTime(), Link: st.Mode()&os.ModeSymlink != 0, Hidden: strings.HasPrefix(st.Name(), "."), ReadOnly: st.Mode().Perm()&0200 == 0}
}

// remoteEtag is "v1:<mtime unix nanos>:<size>:" with an empty hash, the form
// files.Info documents for versions identified by mtime and size alone. It comes
// from attributes the server already returned: no file content is transferred.
// SFTP v3 carries whole-second mtimes, so a same-size rewrite within the same
// second as the previous version is not detected.
func remoteEtag(st os.FileInfo) string {
	return fmt.Sprintf("v1:%d:%d:", st.ModTime().UnixNano(), st.Size())
}
func infoFromStat(p string, st os.FileInfo) files.Info {
	out := files.Info{Entry: entry(p, st)}
	if !st.IsDir() {
		out.Etag = remoteEtag(st)
	}
	return out
}
func remoteInfo(fs *sftp.Client, p string) (files.Info, error) {
	st, err := fs.Stat(p)
	if err != nil {
		return files.Info{}, err
	}
	return infoFromStat(p, st), nil
}
func (m *Manager) ListFiles(ctx context.Context, actor string, p FileRequest) ([]files.Entry, error) {
	op, fs, close, err := m.fileSession(ctx, actor, p, false)
	if err != nil {
		return nil, err
	}
	defer close()
	unlock, err := op.guard()
	if err != nil {
		return nil, err
	}
	defer unlock()
	v, ctx := op.machine, op.ctx
	if fs == nil {
		return localProvider(v).List(ctx, p.Path)
	}
	target, err := checkedRemote(fs, v, p.Path, false)
	if err != nil {
		return nil, err
	}
	list, err := fs.ReadDir(target)
	if err != nil {
		return nil, err
	}
	if len(list) > files.MaxListEntries {
		return nil, files.ErrTooLarge
	}
	out := make([]files.Entry, 0, len(list))
	for _, st := range list {
		out = append(out, entry(path.Join(target, st.Name()), st))
	}
	return out, nil
}
func (m *Manager) StatFile(ctx context.Context, actor string, p FileRequest) (files.Info, error) {
	op, fs, close, err := m.fileSession(ctx, actor, p, false)
	if err != nil {
		return files.Info{}, err
	}
	defer close()
	unlock, err := op.guard()
	if err != nil {
		return files.Info{}, err
	}
	defer unlock()
	v, ctx := op.machine, op.ctx
	if fs == nil {
		return localProvider(v).Stat(ctx, p.Path)
	}
	target, err := checkedRemote(fs, v, p.Path, false)
	if err != nil {
		return files.Info{}, err
	}
	return remoteInfo(fs, target)
}
func (m *Manager) ReadFile(ctx context.Context, actor string, p FileReadRequest) (files.ReadResult, error) {
	op, fs, close, err := m.fileSession(ctx, actor, FileRequest{p.MachineID, p.Path}, false)
	if err != nil {
		return files.ReadResult{}, err
	}
	defer close()
	unlock, err := op.guard()
	if err != nil {
		return files.ReadResult{}, err
	}
	defer unlock()
	v, ctx := op.machine, op.ctx
	if fs == nil {
		return localProvider(v).Read(ctx, p.Path, p.Options)
	}
	target, err := checkedRemote(fs, v, p.Path, false)
	if err != nil {
		return files.ReadResult{}, err
	}
	info, err := remoteInfo(fs, target)
	if err != nil {
		return files.ReadResult{}, err
	}
	if info.IsDir {
		return files.ReadResult{}, files.ErrIsDir
	}
	opt := p.Options
	if opt.Offset < 0 || opt.Length < 0 || opt.MaxBytes < 0 {
		return files.ReadResult{}, files.ErrInvalidPath
	}
	limit := int64(files.MaxReadBytes)
	if opt.MaxBytes > 0 && opt.MaxBytes < limit {
		limit = opt.MaxBytes
	}
	length := info.Size - opt.Offset
	if length < 0 {
		length = 0
	}
	if opt.Length > 0 && opt.Length < length {
		length = opt.Length
	}
	if length > limit {
		return files.ReadResult{}, files.ErrTooLarge
	}
	f, err := fs.Open(target)
	if err != nil {
		return files.ReadResult{}, err
	}
	defer f.Close()
	if _, err = f.Seek(opt.Offset, io.SeekStart); err != nil {
		return files.ReadResult{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, length))
	if err != nil {
		return files.ReadResult{}, err
	}
	// The handle's attributes (one fstat) show whether the file changed while it
	// was being read; the content is never re-read to find out.
	st, err := f.Stat()
	if err != nil {
		return files.ReadResult{}, err
	}
	if after := infoFromStat(target, st); after.Etag != info.Etag {
		return files.ReadResult{}, &files.ConflictError{Path: p.Path, Current: after}
	}
	return files.ReadResult{Data: data, Offset: opt.Offset, Size: info.Size, Truncated: opt.Offset > 0 || int64(len(data)) < info.Size, Info: info}, nil
}
func (m *Manager) WriteFile(ctx context.Context, actor string, p FileWriteRequest) (files.Info, error) {
	if len(p.Data) > files.MaxReadBytes {
		return files.Info{}, files.ErrTooLarge
	}
	op, fs, close, err := m.fileSession(ctx, actor, FileRequest{p.MachineID, p.Path}, true)
	if err != nil {
		return files.Info{}, err
	}
	defer close()
	select {
	case m.writes <- struct{}{}:
		defer func() { <-m.writes }()
	case <-op.ctx.Done():
		return files.Info{}, op.result(op.ctx.Err())
	}
	unlock, err := op.guard()
	if err != nil {
		return files.Info{}, err
	}
	defer unlock()
	v, ctx := op.machine, op.ctx
	if fs == nil {
		return localProvider(v).Write(ctx, p.Path, p.Data, p.Options)
	}
	target, err := checkedRemote(fs, v, p.Path, true)
	if err != nil {
		return files.Info{}, err
	}
	before, err := remoteInfo(fs, target)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return files.Info{}, err
	}
	if exists && before.IsDir {
		return files.Info{}, files.ErrIsDir
	}
	if exists && p.Options.Create {
		return files.Info{}, files.ErrExists
	}
	if exists && !p.Options.Force {
		if p.Options.IfMatch == "" {
			return files.Info{}, files.ErrNeedsEtag
		}
		if p.Options.IfMatch != before.Etag {
			return files.Info{}, &files.ConflictError{Path: p.Path, Current: before}
		}
	}
	if !exists && p.Options.IfMatch != "" && !p.Options.Force {
		return files.Info{}, &files.ConflictError{Path: p.Path, Deleted: true}
	}
	temp := path.Join(path.Dir(target), ".orole-"+uuid.NewString())
	f, err := fs.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return files.Info{}, err
	}
	defer fs.Remove(temp)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return files.Info{}, err
	}
	if _, err = f.Write(p.Data); err != nil {
		f.Close()
		return files.Info{}, err
	}
	if err = f.Close(); err != nil {
		return files.Info{}, err
	}
	if exists {
		// One stat serves both the mode to preserve and the final conflict check.
		st, e := fs.Stat(target)
		if e != nil {
			return files.Info{}, e
		}
		if err = fs.Chmod(temp, st.Mode().Perm()); err != nil {
			return files.Info{}, err
		}
		if current := infoFromStat(target, st); current.Etag != before.Etag {
			return files.Info{}, &files.ConflictError{Path: p.Path, Current: current}
		}
	}
	// Never use delete-then-rename as a fallback: it could lose the original.
	if exists {
		err = fs.PosixRename(temp, target)
	} else {
		err = fs.Rename(temp, target)
	}
	if err != nil {
		return files.Info{}, fmt.Errorf("atomic remote write failed (server must support atomic rename): %w", err)
	}
	return remoteInfo(fs, target)
}
func mapFileError(err error) error {
	if errors.Is(err, ErrDenied) || errors.Is(err, ErrAccessChanged) || errors.Is(err, policy.ErrDenied) {
		return fmt.Errorf("%w: %v", files.ErrDenied, err)
	}
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: %v", files.ErrNotFound, err)
	}
	return err
}

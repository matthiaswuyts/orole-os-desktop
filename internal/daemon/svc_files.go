package daemon

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/machines"
)

// File IPC error codes beyond ipc.Code*.
const (
	codeExists       = "exists"
	codeTooLarge     = "too_large"
	codeReadOnly     = "read_only"
	codeNoRecycle    = "no_recycle_bin"
	codeNotConnected = "not_connected"
)

func mapFileErr(err error) error {
	var ie *ipc.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ie):
		return err
	case errors.Is(err, files.ErrNotFound):
		return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	case errors.Is(err, files.ErrReadOnly):
		return &ipc.Error{Code: codeReadOnly, Message: err.Error()}
	case errors.Is(err, files.ErrDenied):
		return &ipc.Error{Code: ipc.CodeDenied, Message: err.Error()}
	case errors.Is(err, files.ErrExists):
		return &ipc.Error{Code: codeExists, Message: err.Error()}
	case errors.Is(err, files.ErrTooLarge):
		return &ipc.Error{Code: codeTooLarge, Message: err.Error()}
	case errors.Is(err, files.ErrNoRecycleBin):
		return &ipc.Error{Code: codeNoRecycle, Message: err.Error()}
	case errors.Is(err, files.ErrNotConnected):
		return &ipc.Error{Code: codeNotConnected, Message: err.Error()}
	case errors.Is(err, files.ErrInvalidPath), errors.Is(err, files.ErrNotDir), errors.Is(err, files.ErrIsDir),
		errors.Is(err, files.ErrNeedsEtag), errors.Is(err, files.ErrUnsupportedOp):
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	}
	return err
}

// FileProvider describes a provider tab for the UI.
type FileProvider struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Connected bool   `json:"connected"`
	// ManagedRoots is true when the user approves roots here (local); git roots
	// come from the project registry, remote and drive from their own setup.
	ManagedRoots bool   `json:"managedRoots"`
	Note         string `json:"note,omitempty"`
}

type fileTarget struct {
	Provider string `json:"provider"`
	Path     string `json:"path"`
}

// FileWriteResult is files.write's answer: a conflict is data, not an error, so
// the editor can show the current version without parsing messages.
type FileWriteResult struct {
	Info     *files.Info `json:"info,omitempty"`
	Conflict bool        `json:"conflict"`
	// Deleted: the file was removed since it was read.
	Deleted bool `json:"deleted,omitempty"`
	// Current describes the on-disk version when Conflict.
	Current *files.Info `json:"current,omitempty"`
}

// FilesChanged is published after any mutation so views can refresh.
type FilesChanged struct {
	Provider string `json:"provider"`
	Dir      string `json:"dir"`
}

// projectRoots is the git provider's root source: registered local projects.
// The registry (internal/projects) is the permission authority for this
// provider; nothing the user approves for the local provider reaches it.
func (d *Daemon) projectRoots(ctx context.Context) ([]files.Root, error) {
	if d.Projects == nil {
		return nil, nil
	}
	ps, err := d.Projects.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]files.Root, 0, len(ps))
	for _, p := range ps {
		if p.MachineID != "" && p.MachineID != "local" {
			continue
		}
		// Registration pins the fully resolved native path. Never resolve it
		// into new authority here: Local.rootReal must detect retargeted roots.
		// Legacy rows retain their stored authority and fail closed if it no
		// longer matches; only explicit registration can approve a new target.
		out = append(out, files.Root{ID: p.ID, Provider: "git", Path: p.Path, Label: p.Name})
	}
	return out, nil
}

func (d *Daemon) registerFiles() {
	localStore := files.RootStore{DB: d.DB.DB, Provider: "local"}
	local := files.NewLocal(localStore)
	git := files.NewGit(files.RootFunc(d.projectRoots))
	reg := files.NewRegistry(local, git)
	reg.Register(machines.NewProvider(d.Machines))

	provider := func(id string) (files.Provider, error) {
		if id == "" {
			id = "local"
		}
		p, err := reg.Get(id)
		return p, mapFileErr(err)
	}
	changed := func(p files.Provider, path string) {
		d.IPC.Publish("files.changed", FilesChanged{Provider: p.ID(), Dir: filepath.Dir(path)})
	}
	record := func(action string, p files.Provider, target string, err error) {
		if err != nil {
			d.audit(action, p.ID()+":"+target, "error", err.Error())
			return
		}
		d.audit(action, p.ID()+":"+target, "allowed", "")
	}

	d.registerNotifications()
	d.registerSearch(reg)

	handle(d, "files.providers", func(ctx context.Context, _ none) ([]FileProvider, error) {
		_, remoteError := reg.Get("remote")
		_, driveError := reg.Get("drive")
		return []FileProvider{
			{ID: "local", Label: "Local", Connected: true, ManagedRoots: true},
			{ID: "git", Label: "Projects", Connected: true, Note: "Roots are your registered projects."},
			{ID: "remote", Label: "Remote", Connected: remoteError == nil, Note: "Configure remote machines and their approved roots in Machines."},
			{ID: "drive", Label: "Drive", Connected: driveError == nil, Note: "Configure Orole Drive through its own integration."},
		}, nil
	})

	handle(d, "files.roots.list", func(ctx context.Context, p struct {
		Provider string `json:"provider"`
	}) ([]files.Root, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return nil, err
		}
		rs, err := pr.Roots(ctx)
		return rs, mapFileErr(err)
	})
	handle(d, "files.roots.add", func(ctx context.Context, p struct {
		Provider string `json:"provider"`
		Path     string `json:"path"`
		Label    string `json:"label"`
	}) (files.Root, error) {
		if p.Provider != "" && p.Provider != "local" {
			return files.Root{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: "roots of provider " + p.Provider + " are not managed here"}
		}
		r, err := localStore.Add(ctx, p.Path, p.Label)
		d.audit("files.root.add", p.Path, outcome(err), r.Path)
		if err == nil {
			d.IPC.Publish("files.roots.changed", map[string]string{"provider": "local"})
		}
		return r, mapFileErr(err)
	})
	handle(d, "files.roots.remove", func(ctx context.Context, p struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
	}) (none, error) {
		if p.Provider != "" && p.Provider != "local" {
			return none{}, &ipc.Error{Code: ipc.CodeBadRequest, Message: "roots of provider " + p.Provider + " are not managed here"}
		}
		r, err := localStore.Remove(ctx, p.ID)
		d.audit("files.root.remove", p.ID, outcome(err), r.Path)
		if err == nil {
			d.IPC.Publish("files.roots.changed", map[string]string{"provider": "local"})
		}
		return none{}, mapFileErr(err)
	})

	handle(d, "files.list", func(ctx context.Context, p fileTarget) ([]files.Entry, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return nil, err
		}
		es, err := pr.List(ctx, p.Path)
		return es, mapFileErr(err)
	})
	handle(d, "files.stat", func(ctx context.Context, p fileTarget) (files.Info, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return files.Info{}, err
		}
		i, err := pr.Stat(ctx, p.Path)
		return i, mapFileErr(err)
	})
	handle(d, "files.read", func(ctx context.Context, p struct {
		fileTarget
		files.ReadOptions
	}) (files.ReadResult, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return files.ReadResult{}, err
		}
		r, err := pr.Read(ctx, p.Path, p.ReadOptions)
		return r, mapFileErr(err)
	})
	handle(d, "files.write", func(ctx context.Context, p struct {
		fileTarget
		Data []byte `json:"data"`
		files.WriteOptions
	}) (FileWriteResult, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return FileWriteResult{}, err
		}
		info, err := pr.Write(ctx, p.Path, p.Data, p.WriteOptions)
		var ce *files.ConflictError
		if errors.As(err, &ce) {
			d.audit("files.write", pr.ID()+":"+p.Path, "denied", "conflict")
			res := FileWriteResult{Conflict: true, Deleted: ce.Deleted}
			if !ce.Deleted {
				cur := ce.Current
				res.Current = &cur
			}
			return res, nil
		}
		record("files.write", pr, p.Path, err)
		if err != nil {
			return FileWriteResult{}, mapFileErr(err)
		}
		changed(pr, p.Path)
		return FileWriteResult{Info: &info}, nil
	})
	handle(d, "files.mkdir", func(ctx context.Context, p fileTarget) (files.Info, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return files.Info{}, err
		}
		i, err := pr.Mkdir(ctx, p.Path)
		record("files.mkdir", pr, p.Path, err)
		if err == nil {
			changed(pr, p.Path)
		}
		return i, mapFileErr(err)
	})
	handle(d, "files.rename", func(ctx context.Context, p struct {
		Provider string `json:"provider"`
		From     string `json:"from"`
		To       string `json:"to"`
	}) (files.Info, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return files.Info{}, err
		}
		i, err := pr.Rename(ctx, p.From, p.To)
		record("files.rename", pr, p.From+" -> "+p.To, err)
		if err == nil {
			changed(pr, p.From)
			changed(pr, p.To)
		}
		return i, mapFileErr(err)
	})
	handle(d, "files.delete", func(ctx context.Context, p struct {
		fileTarget
		files.DeleteOptions
	}) (files.DeleteResult, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return files.DeleteResult{}, err
		}
		res, err := pr.Delete(ctx, p.Path, p.DeleteOptions)
		action := "files.delete.recycle"
		if p.Permanent {
			action = "files.delete.permanent"
		}
		record(action, pr, p.Path, err)
		if err == nil {
			changed(pr, p.Path)
		}
		return res, mapFileErr(err)
	})
	handle(d, "files.search", func(ctx context.Context, p struct {
		fileTarget
		Query string `json:"query"`
		files.SearchOptions
	}) ([]files.Entry, error) {
		pr, err := provider(p.Provider)
		if err != nil {
			return nil, err
		}
		if p.Path != "" {
			es, err := pr.Search(ctx, p.Path, p.Query, p.SearchOptions)
			return es, mapFileErr(err)
		}
		return searchAllRoots(ctx, pr, p.Query, p.SearchOptions)
	})
}

func outcome(err error) string {
	if err != nil {
		return "error"
	}
	return "allowed"
}

// searchAllRoots searches every healthy root of a provider and keeps the limit overall.
func searchAllRoots(ctx context.Context, pr files.Provider, q string, opt files.SearchOptions) ([]files.Entry, error) {
	roots, err := pr.Roots(ctx)
	if err != nil {
		return nil, mapFileErr(err)
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = files.DefaultSearchResults
	}
	if limit > files.MaxSearchResults {
		limit = files.MaxSearchResults
	}
	out := []files.Entry{}
	seen := make(map[string]struct{})
	for _, r := range roots {
		if r.Missing {
			continue
		}
		es, err := pr.Search(ctx, r.Path, q, opt)
		if err != nil {
			return out, mapFileErr(err)
		}
		for _, entry := range es {
			if _, duplicate := seen[entry.Path]; duplicate {
				continue
			}
			seen[entry.Path] = struct{}{}
			out = append(out, entry)
		}
		if len(out) >= limit*2 || ctx.Err() != nil {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

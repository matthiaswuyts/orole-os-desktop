package machines

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/files"
)

// Provider exposes all paired remote roots in a single slash-separated namespace.
// Unsupported mutations fail explicitly; nothing ever falls back to local disk.
type Provider struct{ manager *Manager }

func NewProvider(manager *Manager) *Provider { return &Provider{manager: manager} }
func (*Provider) ID() string                 { return "remote" }
func namespaced(id, p string) string         { return "/" + id + "/" + strings.TrimPrefix(p, "/") }
func splitRemote(p string) (FileRequest, error) {
	if !strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return FileRequest{}, files.ErrInvalidPath
	}
	parts := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 2)
	if len(parts) != 2 {
		return FileRequest{}, files.ErrInvalidPath
	}
	if _, err := uuid.Parse(parts[0]); err != nil {
		return FileRequest{}, files.ErrInvalidPath
	}
	remote, err := remotePath("/" + parts[1])
	if err != nil {
		return FileRequest{}, err
	}
	return FileRequest{MachineID: parts[0], Path: remote}, nil
}
func (p *Provider) Roots(ctx context.Context) ([]files.Root, error) {
	if p.manager == nil {
		return nil, files.ErrNotConnected
	}
	machines, err := p.manager.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []files.Root{}
	for _, m := range machines {
		if m.Kind != "remote" || m.Transport != "ssh" {
			continue
		}
		for _, r := range m.Roots {
			out = append(out, files.Root{ID: m.ID + ":" + r, Provider: "remote", Path: namespaced(m.ID, r), Label: m.Name + " · " + r})
		}
	}
	return out, nil
}
func providerInfo(id string, info files.Info) files.Info {
	info.Path = namespaced(id, info.Path)
	return info
}
func providerError(id string, err error) error {
	var conflict *files.ConflictError
	if errors.As(err, &conflict) {
		copy := *conflict
		copy.Path = namespaced(id, copy.Path)
		if !copy.Deleted {
			copy.Current = providerInfo(id, copy.Current)
		}
		return &copy
	}
	return mapFileError(err)
}
func (p *Provider) List(ctx context.Context, name string) ([]files.Entry, error) {
	req, err := splitRemote(name)
	if err != nil {
		return nil, err
	}
	out, err := p.manager.ListFiles(ctx, "user", req)
	for i := range out {
		out[i].Path = namespaced(req.MachineID, out[i].Path)
	}
	return out, mapFileError(err)
}
func (p *Provider) Stat(ctx context.Context, name string) (files.Info, error) {
	req, err := splitRemote(name)
	if err != nil {
		return files.Info{}, err
	}
	out, err := p.manager.StatFile(ctx, "user", req)
	return providerInfo(req.MachineID, out), providerError(req.MachineID, err)
}
func (p *Provider) Read(ctx context.Context, name string, opt files.ReadOptions) (files.ReadResult, error) {
	req, err := splitRemote(name)
	if err != nil {
		return files.ReadResult{}, err
	}
	out, err := p.manager.ReadFile(ctx, "user", FileReadRequest{req.MachineID, req.Path, opt})
	out.Info = providerInfo(req.MachineID, out.Info)
	return out, providerError(req.MachineID, err)
}
func (p *Provider) Write(ctx context.Context, name string, data []byte, opt files.WriteOptions) (files.Info, error) {
	req, err := splitRemote(name)
	if err != nil {
		return files.Info{}, err
	}
	out, err := p.manager.WriteFile(ctx, "user", FileWriteRequest{req.MachineID, req.Path, data, opt})
	return providerInfo(req.MachineID, out), providerError(req.MachineID, err)
}
func (*Provider) Mkdir(context.Context, string) (files.Info, error) {
	return files.Info{}, files.ErrUnsupportedOp
}
func (*Provider) Rename(context.Context, string, string) (files.Info, error) {
	return files.Info{}, files.ErrUnsupportedOp
}
func (*Provider) Delete(context.Context, string, files.DeleteOptions) (files.DeleteResult, error) {
	return files.DeleteResult{}, files.ErrUnsupportedOp
}
func (p *Provider) Search(ctx context.Context, start, query string, opt files.SearchOptions) ([]files.Entry, error) {
	if _, err := splitRemote(start); err != nil {
		return nil, err
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = files.DefaultSearchResults
	}
	if limit > files.MaxSearchResults {
		limit = files.MaxSearchResults
	}
	query = strings.ToLower(query)
	queue := []string{start}
	out := []files.Entry{}
	visited := 0
	for len(queue) > 0 && len(out) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir := queue[0]
		queue = queue[1:]
		entries, err := p.List(ctx, dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			visited++
			if visited > files.MaxSearchVisited {
				return out, nil
			}
			if !opt.IncludeHidden && (e.Hidden || e.Name == "node_modules") {
				continue
			}
			if strings.Contains(strings.ToLower(e.Name), query) {
				out = append(out, e)
				if len(out) == limit {
					break
				}
			}
			if e.IsDir && !e.Link && path.Clean(e.Path) != path.Clean(dir) {
				queue = append(queue, e.Path)
			}
		}
	}
	return out, nil
}

var _ files.Provider = (*Provider)(nil)

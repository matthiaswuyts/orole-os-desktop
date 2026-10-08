package app

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/search"
)

// Files forwards provider-scoped file operations to the daemon.
type Files struct{ L *Link }

func (s Files) Providers(ctx context.Context) ([]daemon.FileProvider, error) {
	var out []daemon.FileProvider
	err := s.L.call(ctx, "files.providers", nil, &out)
	return out, err
}
func (s Files) Roots(ctx context.Context, provider string) ([]files.Root, error) {
	var out []files.Root
	err := s.L.call(ctx, "files.roots.list", map[string]string{"provider": provider}, &out)
	return out, err
}

// AddRoot obtains approval through the native folder picker, never an arbitrary frontend path.
func (s Files) AddRoot(ctx context.Context) (*files.Root, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Allow Orole OS to access a folder", Directory: true})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	var out files.Root
	err = s.L.call(ctx, "files.roots.add", map[string]string{"provider": "local", "path": paths[0]}, &out)
	return &out, err
}
func (s Files) RemoveRoot(ctx context.Context, id string) error {
	return s.L.call(ctx, "files.roots.remove", map[string]string{"provider": "local", "id": id}, nil)
}
func (s Files) List(ctx context.Context, provider, path string) ([]files.Entry, error) {
	var out []files.Entry
	err := s.L.call(ctx, "files.list", fileArgs(provider, path), &out)
	return out, err
}
func (s Files) Stat(ctx context.Context, provider, path string) (files.Info, error) {
	var out files.Info
	err := s.L.call(ctx, "files.stat", fileArgs(provider, path), &out)
	return out, err
}

// FileRead carries binary-safe base64 content; the editor checks UTF-8 before editing.
type FileRead struct {
	Data      string     `json:"data"`
	Offset    int64      `json:"offset"`
	Size      int64      `json:"size"`
	Truncated bool       `json:"truncated"`
	Info      files.Info `json:"info"`
}

func (s Files) Read(ctx context.Context, provider, path string, offset, length int64) (FileRead, error) {
	var out FileRead
	p := fileArgs(provider, path)
	p["offset"] = offset
	p["length"] = length
	err := s.L.call(ctx, "files.read", p, &out)
	return out, err
}
func (s Files) Write(ctx context.Context, provider, path, data, ifMatch string, create, force bool) (daemon.FileWriteResult, error) {
	var out daemon.FileWriteResult
	if len(data) > base64.StdEncoding.EncodedLen(files.MaxReadBytes) {
		return out, fmt.Errorf("file exceeds write size limit")
	}
	bytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return out, err
	}
	p := fileArgs(provider, path)
	p["data"] = bytes
	p["ifMatch"] = ifMatch
	p["create"] = create
	p["force"] = force
	err = s.L.call(ctx, "files.write", p, &out)
	return out, err
}
func (s Files) Mkdir(ctx context.Context, provider, path string) (files.Info, error) {
	var out files.Info
	err := s.L.call(ctx, "files.mkdir", fileArgs(provider, path), &out)
	return out, err
}
func (s Files) Rename(ctx context.Context, provider, from, to string) (files.Info, error) {
	var out files.Info
	err := s.L.call(ctx, "files.rename", map[string]string{"provider": provider, "from": from, "to": to}, &out)
	return out, err
}
func (s Files) Delete(ctx context.Context, provider, path string, permanent bool) (files.DeleteResult, error) {
	var out files.DeleteResult
	p := fileArgs(provider, path)
	p["permanent"] = permanent
	err := s.L.call(ctx, "files.delete", p, &out)
	return out, err
}
func (s Files) Search(ctx context.Context, provider, path, query string, limit int) ([]files.Entry, error) {
	var out []files.Entry
	p := fileArgs(provider, path)
	p["query"] = query
	p["limit"] = limit
	err := s.L.call(ctx, "files.search", p, &out)
	return out, err
}
func fileArgs(provider, path string) map[string]any {
	return map[string]any{"provider": provider, "path": path}
}

// Search searches only the daemon-owned local providers.
type Search struct{ L *Link }

func (s Search) Query(ctx context.Context, query string, providers []string, limit int) (search.Response, error) {
	var out search.Response
	err := s.L.call(ctx, "search.query", map[string]any{"query": query, "providers": providers, "limit": limit}, &out)
	return out, err
}

// Package files is Orole OS's file access layer.
//
// Every file operation goes through a Provider. A provider owns its own
// permission model and its own set of roots; permissions are never merged
// across providers (access to a local root grants nothing in the git provider
// and vice versa). The daemon dispatches by provider id and refuses unknown or
// unconnected ones, see Registry.
//
// Implemented providers:
//
//   - "local": the local filesystem, restricted to user-approved roots stored in
//     the daemon database (table file_roots). See Local and the path policy in
//     policy.go.
//   - "git": the local filesystem restricted to project (git repository) roots.
//     Same path policy, additionally read-only inside .git and listings carry
//     git status. See NewGit.
//
// Declared but NOT implemented here (other slices own them; Registry answers
// ErrNotConnected for these ids until they register a Provider):
//
//   - "remote": files on another Orole machine, reached through the machine
//     transport. A remote Provider must enforce the same contract below on the
//     remote side (roots, size limits, etag conflict checks, recycle/permanent
//     delete) and must never fall back to the local filesystem.
//   - "drive": Orole Drive. Paths are drive-relative and slash separated;
//     Etag is the Drive revision id; Delete moves to the Drive trash.
//
// Provider contract: every path argument is a path in the provider's own
// namespace (an absolute path for local/git); all inputs are untrusted; Write is
// optimistic (etag conflict, never silently overwrites); Read and Write are size
// limited; Delete never removes permanently unless asked explicitly.
package files

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Limits shared by providers.
const (
	// MaxReadBytes bounds a single Read result and a whole-file Write. IPC
	// frames are limited to 4 MiB including base64 expansion.
	MaxReadBytes = 2 << 20
	// MaxHashBytes bounds how much of a file Stat will hash for the etag.
	MaxHashBytes = 16 << 20
	// MaxListEntries bounds one directory listing.
	MaxListEntries = 20000
	// DefaultSearchResults / MaxSearchResults bound name search results.
	DefaultSearchResults = 100
	MaxSearchResults     = 500
	// MaxSearchVisited bounds directory entries examined by one search.
	MaxSearchVisited = 200000
)

// Errors returned by providers. Wrap with %w; test with errors.Is / errors.As.
var (
	ErrNotFound        = errors.New("not found")
	ErrExists          = errors.New("already exists")
	ErrNotDir          = errors.New("not a directory")
	ErrIsDir           = errors.New("is a directory")
	ErrTooLarge        = errors.New("too large")
	ErrDenied          = errors.New("denied")
	ErrInvalidPath     = errors.New("invalid path")
	ErrReadOnly        = errors.New("read-only")
	ErrNotConnected    = errors.New("provider not connected")
	ErrNoRecycleBin    = errors.New("recycle bin unavailable")
	ErrNeedsEtag       = errors.New("write needs the etag of the version being replaced")
	ErrUnsupportedOp   = errors.New("operation not supported")
	ErrRootNotApproved = fmt.Errorf("%w: path is outside the approved roots", ErrDenied)
)

// ConflictError is returned by Write when the file changed (or vanished) since
// the version the caller edited.
type ConflictError struct {
	Path string
	// Current describes the file as it is now; zero when Deleted.
	Current Info
	Deleted bool
}

func (e *ConflictError) Error() string {
	if e.Deleted {
		return "conflict: " + e.Path + " was deleted since it was read"
	}
	return "conflict: " + e.Path + " changed since it was read"
}

// Root is an approved top-level directory of a provider.
type Root struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Path     string `json:"path"`
	Label    string `json:"label"`
	// Missing is set when the root no longer resolves to the approved location.
	Missing bool   `json:"missing,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// Entry is one directory listing row.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	// Link is set for symlinks and junctions. They are listed but never
	// followed by Search, and Delete removes only the link.
	Link bool `json:"link,omitempty"`
	// ReadOnly reflects the read-only attribute.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Hidden reflects the hidden attribute or a leading dot.
	Hidden bool `json:"hidden,omitempty"`
	// Git is the porcelain XY status for the git provider ("" = clean/unknown).
	Git string `json:"git,omitempty"`
}

// Info is Stat output: an Entry plus the etag used for optimistic writes.
type Info struct {
	Entry
	// Etag is "v1:<mtime unix nanos>:<size>:<sha256 hex>"; the hash part is empty
	// for files above MaxHashBytes and for remote machine files (then mtime+size
	// alone are compared).
	Etag string `json:"etag,omitempty"`
	// Root is the id of the root that contains the path.
	Root string `json:"root,omitempty"`
}

// ReadOptions selects a byte range. Length 0 reads the whole file, which must
// then fit in MaxBytes (default MaxReadBytes), otherwise ErrTooLarge.
type ReadOptions struct {
	Offset   int64 `json:"offset,omitempty"`
	Length   int64 `json:"length,omitempty"`
	MaxBytes int64 `json:"maxBytes,omitempty"`
}

// ReadResult is file content. Etag describes the whole file as of the read.
type ReadResult struct {
	Data      []byte `json:"data"`
	Offset    int64  `json:"offset"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Info      Info   `json:"info"`
}

// WriteOptions control Write.
type WriteOptions struct {
	// IfMatch is the Etag of the version being replaced. Required when the file
	// exists unless Force is set.
	IfMatch string `json:"ifMatch,omitempty"`
	// Create requires that the file does not exist yet.
	Create bool `json:"create,omitempty"`
	// Force overwrites regardless of etag (the user chose "overwrite" after a
	// conflict). It still never creates paths outside the roots.
	Force bool `json:"force,omitempty"`
}

// DeleteOptions control Delete.
type DeleteOptions struct {
	// Permanent removes instead of recycling. Callers must have obtained explicit
	// user confirmation.
	Permanent bool `json:"permanent,omitempty"`
}

// DeleteResult reports how an item was removed.
type DeleteResult struct {
	Recycled bool `json:"recycled"`
}

// SearchOptions bound a name search.
type SearchOptions struct {
	Limit int `json:"limit,omitempty"`
	// IncludeHidden also searches hidden entries and ignored directories
	// (.git, node_modules).
	IncludeHidden bool `json:"includeHidden,omitempty"`
}

// Provider is a file source with its own permission model.
type Provider interface {
	// ID is the stable provider id ("local", "git", "remote", "drive").
	ID() string
	// Roots lists the approved roots of this provider.
	Roots(ctx context.Context) ([]Root, error)
	// List returns the entries of a directory (not recursive).
	List(ctx context.Context, path string) ([]Entry, error)
	// Stat describes a file or directory, including its etag.
	Stat(ctx context.Context, path string) (Info, error)
	// Read returns file bytes subject to size limits.
	Read(ctx context.Context, path string, opt ReadOptions) (ReadResult, error)
	// Write replaces (or creates) a file atomically. A stale etag returns a
	// *ConflictError.
	Write(ctx context.Context, path string, data []byte, opt WriteOptions) (Info, error)
	// Mkdir creates a single directory whose parent exists.
	Mkdir(ctx context.Context, path string) (Info, error)
	// Rename moves a file or directory without overwriting.
	Rename(ctx context.Context, from, to string) (Info, error)
	// Delete moves to the Recycle Bin, or removes permanently when asked.
	Delete(ctx context.Context, path string, opt DeleteOptions) (DeleteResult, error)
	// Search finds entries by name under a path, with result and effort limits.
	Search(ctx context.Context, path, query string, opt SearchOptions) ([]Entry, error)
}

// Registry maps provider ids to providers. Ids without a registered provider
// answer ErrNotConnected, so a UI tab for "remote" or "drive" shows its setup
// state instead of falling back to another provider.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]Provider
}

// NewRegistry registers providers by their ID.
func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byID: map[string]Provider{}}
	for _, p := range ps {
		r.byID[p.ID()] = p
	}
	return r
}

// Register installs a provider without sharing another provider's roots.
func (r *Registry) Register(provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[provider.ID()] = provider
}

// Get returns the provider or an error wrapping ErrNotConnected.
func (r *Registry) Get(id string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.byID[id]; ok {
		return p, nil
	}
	switch id {
	case "remote", "drive":
		return nil, fmt.Errorf("%w: the %s provider is not connected in this build", ErrNotConnected, id)
	}
	return nil, fmt.Errorf("%w: unknown provider %q", ErrInvalidPath, id)
}

// IDs lists the registered provider ids.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byID))
	for id := range r.byID {
		out = append(out, id)
	}
	return out
}

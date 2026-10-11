// Package projects is the project registry and worktree manager.
//
// # Contract for other slices (agents, review, ...)
//
// Import this package and depend on [Service], whose Worktrees field satisfies [Worktrees]:
//
//	wt, err := svc.Worktrees.Create(ctx, projectID, "fix-login", "main") // new branch orole/fix-login
//	wt, err  = svc.Worktrees.Get(ctx, wt.ID)                            // path, branch, live git state
//
// Create makes `<projectParent>/.orole-worktrees/<project>/<slug>` (or the project's configured
// worktree root) on a new branch `orole/<slug>` started at baseRef ("" = HEAD). Names, refs and
// paths are validated; a path outside the worktree root or a ref that looks like a git option is
// rejected. Errors wrap [ErrNotFound], [ErrInvalid] or [ErrConflict].
//
// Git is the source of truth for which worktrees exist; the SQLite `worktrees` table only keeps
// stable ids and Orole metadata and is reconciled whenever worktrees are listed.
package projects

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/git"
)

// Errors returned (wrapped) by this package.
var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid request")
	ErrConflict = errors.New("conflict")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Project is a registered project: a git repository, a plain folder, or just a name (no folder).
type Project struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MachineID    string `json:"machineId"`
	Path         string `json:"path"`
	RemoteURL    string `json:"remoteUrl"`
	LastOpened   int64  `json:"lastOpened"`
	CreatedAt    int64  `json:"createdAt"`
	WorktreeRoot string `json:"worktreeRoot"` // effective root (configured or default)
	// WorktreeRootCustom is true when the root was configured rather than the default.
	WorktreeRootCustom bool `json:"worktreeRootCustom"`
	// Missing is true when the project has a folder and it no longer exists.
	Missing bool `json:"missing"`
	// IsGit is true when the project folder is a git work tree (worktrees and git state apply).
	IsGit bool `json:"isGit"`
}

// Worktree is a git worktree of a project merged with live git state.
type Worktree struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	Head      string `json:"head"`
	BaseRef   string `json:"baseRef"`
	IsMain    bool   `json:"isMain"`
	Detached  bool   `json:"detached"`
	Locked    bool   `json:"locked"`
	Prunable  bool   `json:"prunable"`
	CreatedAt int64  `json:"createdAt"`
	Upstream  string `json:"upstream"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Staged    int    `json:"staged"`
	Unstaged  int    `json:"unstaged"`
	Untracked int    `json:"untracked"`
	Dirty     int    `json:"dirty"`
	// StatusKnown is true when Upstream, Ahead, Behind and the change counts were read from git.
	// List leaves it false: reading them runs `git status` per worktree, which is slow in large repos.
	StatusKnown bool `json:"statusKnown"`
	// Error is set when git state could not be read for this worktree.
	Error string `json:"error,omitempty"`
}

// Worktrees is the interface other slices import to create and look up worktrees.
type Worktrees interface {
	// Create adds a worktree on a new branch orole/<slug(name)> started at baseRef ("" = HEAD).
	Create(ctx context.Context, projectID, name, baseRef string) (Worktree, error)
	// Get returns one worktree with live git state (one git list plus that worktree's status).
	Get(ctx context.Context, id string) (Worktree, error)
	// Path returns a registered worktree's directory from the database only: no git process, no status.
	Path(ctx context.Context, id string) (string, error)
	// List returns the worktrees of one project without reading per-worktree git status (fast); the main checkout comes first.
	List(ctx context.Context, projectID string) ([]Worktree, error)
	// ListStatus is List plus live git status (ahead/behind, dirty counts) for every worktree.
	ListStatus(ctx context.Context, projectID string) ([]Worktree, error)
	// Remove deletes a linked worktree (never the main checkout). force discards local changes.
	Remove(ctx context.Context, id string, force bool) error
}

// Service is the project registry plus its worktree manager.
type Service struct {
	db        *database.DB
	Worktrees Worktrees
	wm        *worktreeManager
}

// New builds the service on an opened database.
func New(db *database.DB) *Service {
	s := &Service{db: db}
	s.wm = &worktreeManager{s: s}
	s.Worktrees = s.wm
	return s
}

const machineLocal = "local"

const projectCols = `id,name,machine_id,path,remote_url,last_opened,created_at,worktree_root`

type scanner interface{ Scan(...any) error }

func scanProject(r scanner) (Project, error) {
	var p Project
	var custom string
	if err := r.Scan(&p.ID, &p.Name, &p.MachineID, &p.Path, &p.RemoteURL, &p.LastOpened, &p.CreatedAt, &custom); err != nil {
		return p, err
	}
	p.WorktreeRoot, p.WorktreeRootCustom = custom, custom != ""
	if custom == "" {
		p.WorktreeRoot = defaultRoot(p)
	}
	if p.Path != "" {
		if st, err := os.Stat(p.Path); err != nil || !st.IsDir() {
			p.Missing = true
		} else if _, err := os.Lstat(filepath.Join(p.Path, ".git")); err == nil {
			p.IsGit = true
		}
	}
	return p, nil
}

func defaultRoot(p Project) string {
	if p.Path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p.Path), ".orole-worktrees", p.Name)
}

// Add registers the git repository containing path. A linked worktree resolves to its main
// checkout, whose fully resolved native path is pinned as the approved file root.
// Adding a project twice returns the existing one.
func (s *Service) Add(ctx context.Context, path string) (Project, error) {
	if strings.TrimSpace(path) == "" {
		return Project{}, invalid("path is required")
	}
	return s.register(ctx, "", path, true)
}

// Create registers a project by name with an optional folder. The folder need not be a git
// repository; one that is resolves to its main checkout like [Service.Add]. Without a folder,
// tasks use a daemon-managed workspace while the public Path stays empty (no git worktrees).
// A blank name defaults to the folder name. Registering an existing folder returns its project.
func (s *Service) Create(ctx context.Context, name, path string) (Project, error) {
	name, path = strings.TrimSpace(name), strings.TrimSpace(path)
	if name == "" && path == "" {
		return Project{}, invalid("a project name is required")
	}
	return s.register(ctx, name, path, false)
}

func (s *Service) register(ctx context.Context, name, path string, requireGit bool) (Project, error) {
	root, remote := "", ""
	if path != "" {
		canon, err := approvedProjectPath(path)
		if err != nil {
			return Project{}, invalid("%v", err)
		}
		if st, err := os.Stat(canon); err != nil || !st.IsDir() {
			return Project{}, invalid("%s is not a directory", path)
		}
		root = canon
		top, err := git.Toplevel(ctx, canon)
		switch {
		case errors.Is(err, git.ErrNotRepo):
			if requireGit {
				return Project{}, invalid("%s is not a git repository", path)
			}
		case err != nil:
			return Project{}, err
		default:
			list, err := git.WorktreeList(ctx, top)
			if err != nil || len(list) == 0 {
				return Project{}, fmt.Errorf("listing worktrees of %s: %w", top, err)
			}
			if list[0].Bare {
				return Project{}, invalid("bare repositories are not supported")
			}
			if root, err = approvedProjectPath(list[0].Path); err != nil {
				return Project{}, err
			}
			remote = git.RemoteURL(ctx, root)
		}
		existing, err := s.List(ctx)
		if err != nil {
			return Project{}, err
		}
		for _, p := range existing {
			if p.Path != "" && pathKey(p.Path) == pathKey(root) {
				_ = s.MarkOpened(ctx, p.ID)
				return s.Get(ctx, p.ID)
			}
		}
		if name == "" {
			name = filepath.Base(root)
		}
	}
	id, now := uuid.NewString(), time.Now().Unix()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,name,machine_id,path,remote_url,last_opened,created_at) VALUES(?,?,?,?,?,?,?)`,
		id, name, machineLocal, root, remote, now, now); err != nil {
		return Project{}, err
	}
	return s.Get(ctx, id)
}

// Get returns one project.
func (s *Service) Get(ctx context.Context, id string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, id)
	}
	return p, err
}

// List returns projects, most recently opened first.
func (s *Service) List(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectCols+` FROM projects ORDER BY last_opened DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Remove unregisters a project. Files on disk and git worktrees are left untouched.
func (s *Service) Remove(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: project %s", ErrNotFound, id)
	}
	return nil
}

// MarkOpened records that the user opened the project now.
func (s *Service) MarkOpened(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET last_opened=? WHERE id=?`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: project %s", ErrNotFound, id)
	}
	return nil
}

// SetWorktreeRoot configures where new worktrees of the project are created ("" = default).
// The root must be absolute and must not be inside the project's own checkout.
func (s *Service) SetWorktreeRoot(ctx context.Context, id, root string) (Project, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return Project{}, err
	}
	if root != "" {
		if !filepath.IsAbs(root) {
			return Project{}, invalid("worktree root must be an absolute path")
		}
		root = filepath.Clean(root)
		if filepath.Dir(root) == root {
			return Project{}, invalid("worktree root cannot be a drive root")
		}
		if p.Path == "" {
			return Project{}, invalid("project has no folder")
		}
		if inside(p.Path, root) {
			return Project{}, invalid("worktree root cannot be inside the project checkout")
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE projects SET worktree_root=? WHERE id=?`, root, id); err != nil {
		return Project{}, err
	}
	return s.Get(ctx, id)
}

// approvedProjectPath resolves native aliases only during explicit registration.
// Unlike canonical, approval must fail if resolution fails and must expand
// Windows junctions and short names as well as ordinary symlinks.
func approvedProjectPath(p string) (string, error) {
	abs, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		return "", err
	}
	return files.CanonicalRoot(abs)
}

// canonical returns an absolute, symlink-resolved, cleaned path.
func canonical(p string) (string, error) {
	abs, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	return filepath.Clean(abs), nil
}

// inside reports whether child is base or located under base.
func inside(base, child string) bool {
	rel, err := filepath.Rel(pathKey(base), pathKey(child))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func worktreeID(projectID, path string) string {
	sum := sha256.Sum256([]byte(projectID + "|" + pathKey(path)))
	return "wt_" + hex.EncodeToString(sum[:8])
}

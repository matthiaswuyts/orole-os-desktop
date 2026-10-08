package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/git"
)

// BranchPrefix is prepended to the slug of every worktree branch Orole creates.
const BranchPrefix = "orole/"

type worktreeManager struct {
	s  *Service
	mu sync.Mutex // serializes create/remove/reconcile
}

// Slug turns a free-form name into a branch/directory-safe slug ("" if nothing usable remains).
func Slug(name string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == '.' || r == '_' || r == '-' || r == ' ' || r == '/':
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

type wtRow struct {
	id, projectID, name, path, branch, baseRef string
	isMain                                     bool
	createdAt                                  int64
}

// reconcile syncs the worktrees table with git's list and returns the rows in git order.
func (m *worktreeManager) reconcile(ctx context.Context, p Project, list []git.WorktreeInfo) ([]wtRow, error) {
	existing := map[string]wtRow{}
	rows, err := m.s.db.QueryContext(ctx, `SELECT id,name,path,branch,base_ref,is_main,created_at FROM worktrees WHERE project_id=?`, p.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		r := wtRow{projectID: p.ID}
		var main int
		if err := rows.Scan(&r.id, &r.name, &r.path, &r.branch, &r.baseRef, &main, &r.createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		r.isMain = main == 1
		existing[r.id] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tx, err := m.s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := make([]wtRow, 0, len(list))
	seen := map[string]bool{}
	for i, w := range list {
		path, err := canonical(w.Path)
		if err != nil {
			continue
		}
		id := worktreeID(p.ID, path)
		seen[id] = true
		r, ok := existing[id]
		if !ok {
			r = wtRow{id: id, projectID: p.ID, path: path, name: filepath.Base(path), isMain: i == 0, createdAt: time.Now().Unix()}
			if r.isMain {
				r.name = p.Name
			}
		}
		r.branch = w.Branch
		if _, err := tx.ExecContext(ctx, `INSERT INTO worktrees(id,project_id,name,path,branch,base_ref,is_main,created_at) VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET branch=excluded.branch`, r.id, p.ID, r.name, r.path, r.branch, r.baseRef, boolInt(r.isMain), r.createdAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	for id := range existing {
		if !seen[id] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM worktrees WHERE id=?`, id); err != nil {
				return nil, err
			}
		}
	}
	return out, tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// build merges a row with git's entry and live status.
func build(ctx context.Context, r wtRow, w git.WorktreeInfo) Worktree {
	wt := Worktree{ID: r.id, ProjectID: r.projectID, Name: r.name, Path: r.path, Branch: w.Branch, Head: w.Head, BaseRef: r.baseRef,
		IsMain: r.isMain, Detached: w.Detached, Locked: w.Locked, Prunable: w.Prunable, CreatedAt: r.createdAt}
	if w.Prunable {
		wt.Error = "directory is missing (prunable)"
		return wt
	}
	st, err := git.GetStatus(ctx, r.path)
	if err != nil {
		wt.Error = err.Error()
		return wt
	}
	wt.Upstream, wt.Ahead, wt.Behind = st.Branch.Upstream, st.Branch.Ahead, st.Branch.Behind
	wt.Staged, wt.Unstaged, wt.Untracked, wt.Dirty = st.Staged, st.Unstaged, st.Untracked, st.Dirty
	return wt
}

func (m *worktreeManager) List(ctx context.Context, projectID string) ([]Worktree, error) {
	p, err := m.s.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p.Missing {
		return nil, fmt.Errorf("%w: project directory %s is missing", ErrNotFound, p.Path)
	}
	m.mu.Lock()
	list, err := git.WorktreeList(ctx, p.Path)
	var rows []wtRow
	if err == nil {
		rows, err = m.reconcile(ctx, p, list)
	}
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	byPath := map[string]git.WorktreeInfo{}
	for _, w := range list {
		if c, err := canonical(w.Path); err == nil {
			byPath[pathKey(c)] = w
		}
	}
	out := make([]Worktree, len(rows))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, r := range rows {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = build(ctx, r, byPath[pathKey(r.path)])
		}()
	}
	wg.Wait()
	return out, nil
}

func (m *worktreeManager) Get(ctx context.Context, id string) (Worktree, error) {
	var projectID string
	err := m.s.db.QueryRowContext(ctx, `SELECT project_id FROM worktrees WHERE id=?`, id).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return Worktree{}, fmt.Errorf("%w: worktree %s", ErrNotFound, id)
	}
	if err != nil {
		return Worktree{}, err
	}
	all, err := m.List(ctx, projectID)
	if err != nil {
		return Worktree{}, err
	}
	for _, w := range all {
		if w.ID == id {
			return w, nil
		}
	}
	return Worktree{}, fmt.Errorf("%w: worktree %s", ErrNotFound, id)
}

func (m *worktreeManager) Create(ctx context.Context, projectID, name, baseRef string) (Worktree, error) {
	p, err := m.s.Get(ctx, projectID)
	if err != nil {
		return Worktree{}, err
	}
	if p.Missing {
		return Worktree{}, fmt.Errorf("%w: project directory %s is missing", ErrNotFound, p.Path)
	}
	slug := Slug(name)
	if slug == "" {
		return Worktree{}, invalid("worktree name needs letters or digits")
	}
	if baseRef != "" && !git.ValidRev(baseRef) {
		return Worktree{}, invalid("invalid base ref %q", baseRef)
	}
	branch := BranchPrefix + slug
	if err := git.ValidBranchName(ctx, branch); err != nil {
		return Worktree{}, invalid("%v", err)
	}
	root := filepath.Clean(p.WorktreeRoot)
	path := filepath.Join(root, slug)
	if filepath.Dir(path) != root || !inside(root, path) || inside(p.Path, path) {
		return Worktree{}, invalid("worktree path %s is outside the worktree root", path)
	}

	id, err := m.createLocked(ctx, p, slug, branch, baseRef, root, path)
	if err != nil {
		return Worktree{}, err
	}
	return m.Get(ctx, id)
}

func (m *worktreeManager) createLocked(ctx context.Context, p Project, slug, branch, baseRef, root, path string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := git.ResolveCommit(ctx, p.Path, orDefault(baseRef, "HEAD")); err != nil {
		return "", invalid("%v", err)
	}
	if ok, err := git.BranchExists(ctx, p.Path, branch); err != nil {
		return "", err
	} else if ok {
		return "", fmt.Errorf("%w: branch %s already exists", ErrConflict, branch)
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("%w: %s already exists", ErrConflict, path)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	if err := git.WorktreeAdd(ctx, p.Path, path, branch, baseRef); err != nil {
		return "", err
	}
	canon, err := canonical(path)
	if err != nil {
		return "", err
	}
	id := worktreeID(p.ID, canon)
	_, err = m.s.db.ExecContext(ctx, `INSERT INTO worktrees(id,project_id,name,path,branch,base_ref,is_main,created_at) VALUES(?,?,?,?,?,?,0,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, branch=excluded.branch, base_ref=excluded.base_ref`,
		id, p.ID, slug, canon, branch, orDefault(baseRef, "HEAD"), time.Now().Unix())
	return id, err
}

func (m *worktreeManager) Remove(ctx context.Context, id string, force bool) error {
	wt, err := m.Get(ctx, id)
	if err != nil {
		return err
	}
	if wt.IsMain {
		return invalid("the main checkout cannot be removed; remove the project instead")
	}
	p, err := m.s.Get(ctx, wt.ProjectID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if wt.Prunable {
		if err := git.WorktreePrune(ctx, p.Path); err != nil {
			return err
		}
	} else if err := git.WorktreeRemove(ctx, p.Path, wt.Path, force); err != nil {
		return err
	}
	_, err = m.s.db.ExecContext(ctx, `DELETE FROM worktrees WHERE id=?`, id)
	return err
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

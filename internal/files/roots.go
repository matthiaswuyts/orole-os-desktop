package files

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RootStore persists a provider's approved roots in table file_roots. Each
// provider id gets its own rows: the store is also a RootSource for exactly
// that provider.
type RootStore struct {
	DB       *sql.DB
	Provider string
}

// Roots implements RootSource.
func (s RootStore) Roots(ctx context.Context) ([]Root, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, path, label FROM file_roots WHERE provider=? ORDER BY created_at, path`, s.Provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Root{}
	for rows.Next() {
		r := Root{Provider: s.Provider}
		if err := rows.Scan(&r.ID, &r.Path, &r.Label); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Add approves a directory. The path is canonicalised first, so a symlink or
// junction is approved as the place it really is. Adding an existing root
// returns it unchanged.
func (s RootStore) Add(ctx context.Context, path, label string) (Root, error) {
	real, err := CanonicalRoot(path)
	if err != nil {
		return Root{}, err
	}
	if label == "" {
		label = filepath.Base(real)
		if label == "" || label == string(filepath.Separator) || strings.HasSuffix(label, ":") {
			label = real
		}
	}
	if len(label) > 120 {
		label = label[:120]
	}
	var existing Root
	err = s.DB.QueryRowContext(ctx, `SELECT id, path, label FROM file_roots WHERE provider=? AND lower(path)=lower(?)`, s.Provider, real).
		Scan(&existing.ID, &existing.Path, &existing.Label)
	if err == nil {
		existing.Provider = s.Provider
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return Root{}, err
	}
	r := Root{ID: uuid.NewString(), Provider: s.Provider, Path: real, Label: label}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO file_roots(id,provider,path,label,created_at) VALUES(?,?,?,?,?)`,
		r.ID, r.Provider, r.Path, r.Label, time.Now().Unix()); err != nil {
		return Root{}, fmt.Errorf("add root: %w", err)
	}
	return r, nil
}

// Remove revokes a root. Files on disk are untouched.
func (s RootStore) Remove(ctx context.Context, id string) (Root, error) {
	var r Root
	err := s.DB.QueryRowContext(ctx, `SELECT id, path, label FROM file_roots WHERE provider=? AND id=?`, s.Provider, id).Scan(&r.ID, &r.Path, &r.Label)
	if err == sql.ErrNoRows {
		return Root{}, fmt.Errorf("%w: no such root", ErrNotFound)
	}
	if err != nil {
		return Root{}, err
	}
	r.Provider = s.Provider
	_, err = s.DB.ExecContext(ctx, `DELETE FROM file_roots WHERE provider=? AND id=?`, s.Provider, id)
	return r, err
}

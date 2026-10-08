// Package applications owns durable, explicitly configured native application shortcuts.
// Launches are never classified as read-only or exposed as agent tools.
package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

var (
	ErrInvalid  = errors.New("applications: invalid shortcut")
	ErrNotFound = errors.New("applications: shortcut not found")
	ErrChanged  = errors.New("applications: shortcut changed while approval was pending; launch again")
)

// Shortcut contains non-secret launch configuration, never credentials or environment overrides.
type Shortcut struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Executable       string   `json:"executable"`
	Arguments        []string `json:"arguments"`
	WorkingDirectory string   `json:"workingDirectory"`
	Revision         int64    `json:"revision"`
	CreatedAt        int64    `json:"createdAt"`
	UpdatedAt        int64    `json:"updatedAt"`
}

type SaveRequest struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Executable       string   `json:"executable"`
	Arguments        []string `json:"arguments"`
	WorkingDirectory string   `json:"workingDirectory"`
	Revision         int64    `json:"revision"`
}

type LaunchResult struct {
	ShortcutID string `json:"shortcutId"`
	PID        int    `json:"pid"`
}

type Authorizer interface {
	Require(context.Context, policy.Request) error
}

// Manager serializes changes with the final launch check, but never holds its lock
// while awaiting user approval. The daemon owns one manager for all launcher IPC.
type Manager struct {
	db     *sql.DB
	policy Authorizer
	mu     sync.Mutex
	start  func(Shortcut) (int, error)
}

func New(db *sql.DB, auth Authorizer) *Manager {
	return &Manager{db: db, policy: auth, start: startProcess}
}

func validText(s string, max int) bool {
	return len(s) <= max && !strings.ContainsFunc(s, unicode.IsControl)
}

func validate(req SaveRequest) (SaveRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || !validText(req.Name, 120) {
		return req, fmt.Errorf("%w: name must contain 1–120 bytes without control characters", ErrInvalid)
	}
	if req.ID != "" {
		if _, err := uuid.Parse(req.ID); err != nil || req.Revision < 1 {
			return req, fmt.Errorf("%w: update requires an ID and revision", ErrInvalid)
		}
	}
	if !validText(req.Executable, 4096) || !filepath.IsAbs(req.Executable) || !localPath(req.Executable) {
		return req, fmt.Errorf("%w: executable must be an absolute local path", ErrInvalid)
	}
	path, err := filepath.EvalSymlinks(req.Executable)
	if err != nil {
		return req, fmt.Errorf("%w: executable is unavailable", ErrInvalid)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || !nativeExecutable(path, info.Mode()) {
		return req, fmt.Errorf("%w: choose a local native executable, not a folder or shell script", ErrInvalid)
	}
	req.Executable = path
	if len(req.Arguments) > 64 {
		return req, fmt.Errorf("%w: at most 64 arguments", ErrInvalid)
	}
	total := 0
	for _, arg := range req.Arguments {
		total += len(arg)
		if !validText(arg, 4096) || total > 8192 {
			return req, fmt.Errorf("%w: arguments exceed limits or contain control characters", ErrInvalid)
		}
	}
	if req.Arguments == nil {
		req.Arguments = []string{}
	}
	if req.WorkingDirectory != "" {
		if !validText(req.WorkingDirectory, 4096) || !filepath.IsAbs(req.WorkingDirectory) || !localPath(req.WorkingDirectory) {
			return req, fmt.Errorf("%w: working directory must be an absolute local path", ErrInvalid)
		}
		dir, err := filepath.EvalSymlinks(req.WorkingDirectory)
		if err != nil {
			return req, fmt.Errorf("%w: working directory is unavailable", ErrInvalid)
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() || !localPath(dir) {
			return req, fmt.Errorf("%w: working directory must be a local folder", ErrInvalid)
		}
		req.WorkingDirectory = filepath.Clean(dir)
	}
	// The complete launch command must fit in policy's approval detail (2000 bytes),
	// so an unseen suffix can never be authorized by a truncated prompt.
	args, _ := json.Marshal(req.Arguments)
	if len(req.Name)+len(req.Executable)+len(args)+len(req.WorkingDirectory) > 1700 {
		return req, fmt.Errorf("%w: launch configuration is too long for the approval prompt", ErrInvalid)
	}
	return req, nil
}

const columns = `id,name,executable,arguments,working_directory,revision,created_at,updated_at`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Shortcut, error) {
	var s Shortcut
	var args string
	err := row.Scan(&s.ID, &s.Name, &s.Executable, &args, &s.WorkingDirectory, &s.Revision, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal([]byte(args), &s.Arguments); err != nil {
		return s, err
	}
	if s.Arguments == nil {
		s.Arguments = []string{}
	}
	return s, nil
}

func (m *Manager) List(ctx context.Context) ([]Shortcut, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT `+columns+` FROM application_shortcuts ORDER BY name COLLATE NOCASE,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Shortcut{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (m *Manager) Get(ctx context.Context, id string) (Shortcut, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Shortcut{}, fmt.Errorf("%w: invalid ID", ErrInvalid)
	}
	return scan(m.db.QueryRowContext(ctx, `SELECT `+columns+` FROM application_shortcuts WHERE id=?`, id))
}

func (m *Manager) Save(ctx context.Context, req SaveRequest) (Shortcut, error) {
	req, err := validate(req)
	if err != nil {
		return Shortcut{}, err
	}
	args, err := json.Marshal(req.Arguments)
	if err != nil {
		return Shortcut{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UnixMilli()
	if req.ID == "" {
		req.ID = uuid.NewString()
		_, err = m.db.ExecContext(ctx, `INSERT INTO application_shortcuts (`+columns+`) VALUES(?,?,?,?,?,1,?,?)`, req.ID, req.Name, req.Executable, string(args), req.WorkingDirectory, now, now)
	} else {
		var result sql.Result
		result, err = m.db.ExecContext(ctx, `UPDATE application_shortcuts SET name=?,executable=?,arguments=?,working_directory=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, req.Name, req.Executable, string(args), req.WorkingDirectory, now, req.ID, req.Revision)
		if err == nil {
			n, e := result.RowsAffected()
			if e != nil {
				return Shortcut{}, e
			}
			if n == 0 {
				return Shortcut{}, ErrChanged
			}
		}
	}
	if err != nil {
		return Shortcut{}, err
	}
	return m.Get(ctx, req.ID)
}

func (m *Manager) Remove(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.Get(ctx, id); err != nil {
		return err
	}
	_, err := m.db.ExecContext(ctx, `DELETE FROM application_shortcuts WHERE id=?`, id)
	return err
}

// Launch authorizes a fixed, persisted snapshot, not caller-supplied executable data.
// Only interactive user launches are supported. Adding an agent/automation capability
// requires its own trusted actor propagation, not reusing this user-only entry point.
func (m *Manager) Launch(ctx context.Context, id string) (LaunchResult, error) {
	s, err := m.Get(ctx, id)
	if err != nil {
		return LaunchResult{}, err
	}
	if m.policy == nil {
		return LaunchResult{}, policy.ErrDenied
	}
	args, _ := json.Marshal(s.Arguments)
	err = m.policy.Require(ctx, policy.Request{
		Actor: "user", Action: "applications.launch",
		Resource: fmt.Sprintf("applications/%s/revision/%d", s.ID, s.Revision),
		Effect:   policy.EffectDestructive,
		Detail:   fmt.Sprintf("Launch %s. Native executables can access your account and files.\nExecutable: %s\nArguments: %s\nWorking directory: %s", s.Name, s.Executable, args, s.WorkingDirectory),
	})
	if err != nil {
		return LaunchResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.Get(ctx, id)
	if err != nil {
		return LaunchResult{}, err
	}
	if current.Revision != s.Revision {
		return LaunchResult{}, ErrChanged
	}
	// Recheck availability after approval; do not silently follow a replaced symlink.
	checked, err := validate(SaveRequest{ID: s.ID, Revision: s.Revision, Name: s.Name, Executable: s.Executable, Arguments: s.Arguments, WorkingDirectory: s.WorkingDirectory})
	if err != nil {
		return LaunchResult{}, err
	}
	if checked.Executable != s.Executable || checked.WorkingDirectory != s.WorkingDirectory {
		return LaunchResult{}, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return LaunchResult{}, err
	}
	pid, err := m.start(s)
	if err != nil {
		return LaunchResult{}, fmt.Errorf("applications: could not start executable: %w", err)
	}
	return LaunchResult{ShortcutID: s.ID, PID: pid}, nil
}

func startProcess(s Shortcut) (int, error) {
	// No shell expansion, PATH lookup, environment overrides or command strings.
	// The launched app is independent of the request/GUI lifetime; Wait reaps it.
	cmd := exec.Command(s.Executable, s.Arguments...)
	cmd.Dir = s.WorkingDirectory
	if cmd.Dir == "" {
		cmd.Dir = filepath.Dir(s.Executable)
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

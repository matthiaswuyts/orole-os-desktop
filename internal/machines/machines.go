// Package machines separates machine transport (native/SSH) from execution backend.
package machines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/systemmetrics"
)

var (
	ErrHostKeyChanged = errors.New("machine identity changed: connection refused; verify the server independently before removing and pairing it again")
	ErrDenied         = errors.New("machine capability is not authorized")
	ErrInvalid        = errors.New("invalid machine request")
	ErrAccessChanged  = errors.New("machine access or identity changed; submit a fresh request")
)

type Machine struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	OS             string   `json:"os"`
	Transport      string   `json:"transport"`
	Backend        string   `json:"backend"`
	Address        string   `json:"address"`
	Username       string   `json:"username"`
	Fingerprint    string   `json:"fingerprint"`
	Roots          []string `json:"roots"`
	AllowCommands  bool     `json:"allowCommands"`
	AllowWrite     bool     `json:"allowWrite"`
	CreatedAt      int64    `json:"createdAt"`
	hostKey        string
	accessRevision int64
}
type Checker interface {
	Check(context.Context, policy.Request) (policy.Decision, error)
}
type Manager struct {
	db          *sql.DB
	secrets     credentials.Store
	policy      Checker
	mu          sync.Mutex
	running     map[string]context.CancelFunc
	writes      chan struct{}
	started     time.Time
	accessMu    sync.RWMutex
	operations  map[*accessOperation]struct{}
	closed      bool
	hostMetrics *systemmetrics.Sampler
	pool        *sessionPool
}

func New(db *sql.DB, secrets credentials.Store, check Checker) *Manager {
	m := &Manager{db: db, secrets: secrets, policy: check, running: map[string]context.CancelFunc{}, writes: make(chan struct{}, 1), started: time.Now(), operations: map[*accessOperation]struct{}{}}
	m.pool = newSessionPool(m.dialSession)
	return m
}

// SetHostMetrics binds the daemon-owned native sampler before serving requests.
func (m *Manager) SetHostMetrics(s *systemmetrics.Sampler) {
	m.hostMetrics = s
}
func (m *Manager) Close() {
	m.accessMu.Lock()
	m.closed = true
	for op := range m.operations {
		op.cancel(context.Canceled)
	}
	m.accessMu.Unlock()
	m.pool.closeAll()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cancel := range m.running {
		cancel()
	}
}
func (m *Manager) authorize(ctx context.Context, actor, action, id string, effect policy.Effect, detail string) error {
	if m.policy == nil {
		return ErrDenied
	}
	decision, err := m.policy.Check(ctx, policy.Request{Actor: actor, Action: "machines." + action, Resource: "machine/" + id, Effect: effect, Detail: detail})
	if err != nil {
		return err
	}
	if decision != policy.Allow {
		return ErrDenied
	}
	return nil
}
func (m *Manager) ensureLocal(ctx context.Context) error {
	name, err := os.Hostname()
	if err != nil {
		return err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO machines(id,name,kind,os,transport,backend,created_at) VALUES('local',?,'local',?,'native','native',?) ON CONFLICT(id) DO UPDATE SET os=excluded.os`, name, runtime.GOOS, time.Now().UnixMilli())
	return err
}

const columns = `id,name,kind,os,transport,backend,address,username,fingerprint,roots,allow_commands,allow_write,created_at,host_key,access_revision`

type scanner interface{ Scan(...any) error }

func scan(s scanner) (Machine, error) {
	var v Machine
	var roots string
	err := s.Scan(&v.ID, &v.Name, &v.Kind, &v.OS, &v.Transport, &v.Backend, &v.Address, &v.Username, &v.Fingerprint, &roots, &v.AllowCommands, &v.AllowWrite, &v.CreatedAt, &v.hostKey, &v.accessRevision)
	if err == nil {
		err = json.Unmarshal([]byte(roots), &v.Roots)
	}
	if v.Roots == nil {
		v.Roots = []string{}
	}
	return v, err
}
func (m *Manager) List(ctx context.Context) ([]Machine, error) {
	if err := m.ensureLocal(ctx); err != nil {
		return nil, err
	}
	rows, err := m.db.QueryContext(ctx, `SELECT `+columns+` FROM machines ORDER BY kind,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Machine{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (m *Manager) Get(ctx context.Context, id string) (Machine, error) {
	if id == "local" {
		if err := m.ensureLocal(ctx); err != nil {
			return Machine{}, err
		}
	}
	return scan(m.db.QueryRowContext(ctx, `SELECT `+columns+` FROM machines WHERE id=?`, id))
}

// Rename changes presentation metadata only, never grants or transport identity.
func (m *Manager) Rename(ctx context.Context, actor, id, name string) (Machine, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 || strings.ContainsAny(name, "\r\n\t") || !approvalText(name) {
		return Machine{}, ErrInvalid
	}
	op, err := m.beginAccess(ctx, id)
	if err != nil {
		return Machine{}, err
	}
	defer op.finish()
	if err = m.authorize(op.ctx, actor, "rename", id, policy.EffectWrite, "Rename machine to "+name); err != nil {
		return Machine{}, op.result(err)
	}
	unlock, err := op.guard()
	if err != nil {
		return Machine{}, err
	}
	_, err = m.db.ExecContext(op.ctx, `UPDATE machines SET name=? WHERE id=?`, name, id)
	unlock()
	if err != nil {
		return Machine{}, op.result(err)
	}
	v := op.machine
	v.Name = name
	return v, op.result(nil)
}

type Access struct {
	ID            string   `json:"id"`
	Roots         []string `json:"roots"`
	AllowCommands bool     `json:"allowCommands"`
	AllowWrite    bool     `json:"allowWrite"`
}

func (m *Manager) SetAccess(ctx context.Context, actor string, p Access) (Machine, error) {
	op, err := m.beginAccess(ctx, p.ID)
	if err != nil {
		return Machine{}, err
	}
	defer op.finish()
	v := op.machine
	// Canonicalization only reads metadata. Approve the canonical roots that
	// will actually be persisted, never a different post-consent resolution.
	roots, err := m.canonicalRoots(op.ctx, v, p.Roots)
	if err != nil {
		return v, op.result(err)
	}
	detail, err := accessDetail(v, roots, p.AllowCommands, p.AllowWrite)
	if err != nil {
		return v, err
	}
	if err = m.authorize(op.ctx, actor, "access", v.ID, policy.EffectDestructive, detail); err != nil {
		return v, op.result(err)
	}
	checked, err := m.canonicalRoots(op.ctx, v, roots)
	if err != nil {
		return v, op.result(err)
	}
	if !slices.Equal(checked, roots) {
		return v, ErrAccessChanged
	}
	encoded, err := json.Marshal(roots)
	if err != nil {
		return v, err
	}
	m.accessMu.Lock()
	defer m.accessMu.Unlock()
	if err = op.validateLocked(); err != nil {
		return v, err
	}
	result, err := m.db.ExecContext(op.ctx, `UPDATE machines SET roots=?,allow_commands=?,allow_write=?,access_revision=access_revision+1 WHERE id=? AND access_revision=?`, string(encoded), p.AllowCommands, p.AllowWrite, v.ID, v.accessRevision)
	if err != nil {
		return v, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return v, err
	}
	if n != 1 {
		return v, ErrAccessChanged
	}
	m.invalidateLocked(v.ID, op)
	return m.Get(op.ctx, v.ID)
}
func (m *Manager) Remove(ctx context.Context, actor, id string) error {
	if id == "local" {
		return ErrDenied
	}
	op, err := m.beginAccess(ctx, id)
	if err != nil {
		return err
	}
	defer op.finish()
	identity, err := machineIdentity(op.machine)
	if err != nil {
		return err
	}
	detail, err := approvalDetail(struct {
		Action string           `json:"action"`
		Target approvalIdentity `json:"target"`
	}{"Remove pinned identity and credentials", identity})
	if err != nil {
		return err
	}
	if err = m.authorize(op.ctx, actor, "remove", id, policy.EffectDestructive, detail); err != nil {
		return op.result(err)
	}
	m.accessMu.Lock()
	defer m.accessMu.Unlock()
	if err = op.validateLocked(); err != nil {
		return err
	}
	if err = m.secrets.Delete("machines/" + id + "/key"); err != nil && !errors.Is(err, credentials.ErrNotFound) {
		return err
	}
	// Once the credential is removed, invalidate operations even if persistence
	// fails. In-memory signers must not remain usable after credential removal.
	m.invalidateLocked(id, op)
	_, err = m.db.ExecContext(op.ctx, `DELETE FROM machines WHERE id=?`, id)
	return err
}
func validateAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" || strings.ContainsAny(address, "\x00\r\n") {
		return fmt.Errorf("%w: use host:port (IPv6: [address]:port)", ErrInvalid)
	}
	if _, err = net.LookupPort("tcp", port); err != nil {
		return ErrInvalid
	}
	return nil
}
func operationContext(ctx context.Context, seconds int) (context.Context, context.CancelFunc) {
	if seconds <= 0 {
		seconds = 30
	}
	if seconds > 300 {
		seconds = 300
	}
	return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
}
func (m *Manager) track(ctx context.Context, id string) (context.Context, func(), error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, nil, fmt.Errorf("%w: operation id must be a UUID", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.running[id]; ok {
		return nil, nil, fmt.Errorf("%w: operation already active", ErrInvalid)
	}
	ctx, cancel := context.WithCancel(ctx)
	m.running[id] = cancel
	return ctx, func() { cancel(); m.mu.Lock(); delete(m.running, id); m.mu.Unlock() }, nil
}
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cancel, ok := m.running[id]
	if !ok {
		return sql.ErrNoRows
	}
	cancel()
	return nil
}

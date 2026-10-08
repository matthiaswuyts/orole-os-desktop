package plugins

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

type operation struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Manager is daemon-owned. Its lock serializes durable mutations, cancellation
// registration and dispatch guards, but is never held while awaiting approval.
// Cancellation prevents pending dispatch and discards late results; it cannot
// undo effects that a remote MCP server has already performed.
type Manager struct {
	db      *sql.DB
	policy  Authorizer
	invoker Invoker
	mu      sync.Mutex
	closed  bool
	active  map[string]map[*operation]struct{}
}

func New(db *sql.DB, auth Authorizer, invoker Invoker) *Manager {
	return &Manager{db: db, policy: auth, invoker: invoker, active: make(map[string]map[*operation]struct{})}
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for id := range m.active {
		m.cancelLocked(id)
	}
}

func (m *Manager) cancelLocked(id string) {
	for op := range m.active[id] {
		op.cancel()
	}
	delete(m.active, id)
}

func (m *Manager) trackLocked(ctx context.Context, id string) *operation {
	ctx, cancel := context.WithCancel(ctx)
	op := &operation{ctx: ctx, cancel: cancel}
	if m.active[id] == nil {
		m.active[id] = make(map[*operation]struct{})
	}
	m.active[id][op] = struct{}{}
	return op
}

func (m *Manager) finishLocked(id string, op *operation) {
	op.cancel()
	delete(m.active[id], op)
	if len(m.active[id]) == 0 {
		delete(m.active, id)
	}
}

func (m *Manager) finish(id string, op *operation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishLocked(id, op)
}

const columns = `id,manifest,digest,enabled,approved_digest,updated_at`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (PluginRecord, error) {
	var out PluginRecord
	var id string
	var raw []byte
	err := row.Scan(&id, &raw, &out.Digest, &out.Enabled, &out.ApprovedDigest, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out.Manifest); err != nil {
		return out, invalid("stored manifest is invalid")
	}
	manifest, _, digest, err := validateManifest(out.Manifest)
	if err != nil || id != manifest.ID || digest != out.Digest || (out.Enabled && out.ApprovedDigest != digest) {
		return out, invalid("stored manifest integrity check failed")
	}
	out.Manifest = manifest
	return out, nil
}

func (m *Manager) getLocked(ctx context.Context, id string) (PluginRecord, error) {
	if m.closed {
		return PluginRecord{}, ErrClosed
	}
	return scan(m.db.QueryRowContext(ctx, `SELECT `+columns+` FROM plugins WHERE id=?`, id))
}

func (m *Manager) List(ctx context.Context) ([]PluginRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	rows, err := m.db.QueryContext(ctx, `SELECT `+columns+` FROM plugins ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PluginRecord{}
	for rows.Next() {
		record, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (m *Manager) Save(ctx context.Context, req PluginSaveRequest) (PluginRecord, error) {
	manifest, canonical, digest, err := validateManifest(req.Manifest)
	if err != nil {
		return PluginRecord{}, err
	}
	if req.ExpectedDigest != "" && !digestPattern.MatchString(req.ExpectedDigest) {
		return PluginRecord{}, invalid("expectedDigest must be empty for creation or an exact SHA-256 digest")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.getLocked(ctx, manifest.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return PluginRecord{}, err
	}
	if errors.Is(err, ErrNotFound) {
		if req.ExpectedDigest != "" {
			return PluginRecord{}, ErrChanged
		}
		out := PluginRecord{Manifest: manifest, Digest: digest, UpdatedAt: time.Now().UnixMilli()}
		_, err = m.db.ExecContext(ctx, `INSERT INTO plugins(id,manifest,digest,enabled,approved_digest,updated_at) VALUES(?,?,?,0,'',?)`, manifest.ID, canonical, digest, out.UpdatedAt)
		return out, err
	}
	if req.ExpectedDigest != current.Digest {
		return PluginRecord{}, ErrChanged
	}
	if digest == current.Digest {
		return current, nil
	}
	out := PluginRecord{Manifest: manifest, Digest: digest, UpdatedAt: time.Now().UnixMilli()}
	result, err := m.db.ExecContext(ctx, `UPDATE plugins SET manifest=?,digest=?,enabled=0,approved_digest='',updated_at=? WHERE id=? AND digest=?`, canonical, digest, out.UpdatedAt, manifest.ID, current.Digest)
	if err = changed(result, err); err != nil {
		return PluginRecord{}, err
	}
	m.cancelLocked(manifest.ID)
	return out, nil
}

func changed(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrChanged
	}
	return err
}

func (m *Manager) Trust(ctx context.Context, req PluginTrustRequest) (out PluginRecord, err error) {
	if !validRef(req.ID, req.Digest) {
		return out, invalid("trust requires a valid ID and exact digest")
	}
	defer func() {
		outcome, detail := "denied", "Plugin trust change did not complete"
		if err == nil {
			outcome, detail = "allowed", "Plugin trust revoked"
			if req.Enabled {
				detail = "Exact plugin manifest enabled after central approval"
			}
		}
		m.audit("user", "plugins.trust", req.ID, req.Digest, outcome, detail)
	}()
	m.mu.Lock()
	out, err = m.getLocked(ctx, req.ID)
	if err == nil && out.Digest != req.Digest {
		err = ErrChanged
	}
	if err != nil {
		m.mu.Unlock()
		return PluginRecord{}, err
	}
	if !req.Enabled {
		out.Enabled, out.ApprovedDigest, out.UpdatedAt = false, "", time.Now().UnixMilli()
		result, updateErr := m.db.ExecContext(ctx, `UPDATE plugins SET enabled=0,approved_digest='',updated_at=? WHERE id=? AND digest=?`, out.UpdatedAt, req.ID, req.Digest)
		err = changed(result, updateErr)
		if err == nil {
			m.cancelLocked(req.ID)
		}
		m.mu.Unlock()
		return out, err
	}
	if m.policy == nil {
		m.mu.Unlock()
		return PluginRecord{}, policy.ErrDenied
	}
	op := m.trackLocked(ctx, req.ID)
	m.mu.Unlock()
	defer m.finish(req.ID, op)
	err = m.policy.Require(op.ctx, policy.Request{
		Actor: "user", Action: "plugins.trust", Resource: "plugins/" + req.ID + "/" + req.Digest,
		Effect: policy.EffectDestructive, Detail: trustDetail(out.Manifest, req.Digest),
	})
	if err != nil {
		return PluginRecord{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// A revoke, change-away-and-back, removal or shutdown invalidates this
	// operation even if a later record happens to have the same digest.
	if err = op.ctx.Err(); err != nil {
		return PluginRecord{}, err
	}
	out, err = m.getLocked(op.ctx, req.ID)
	if err != nil {
		return PluginRecord{}, err
	}
	if out.Digest != req.Digest {
		return PluginRecord{}, ErrChanged
	}
	out.Enabled, out.ApprovedDigest, out.UpdatedAt = true, req.Digest, time.Now().UnixMilli()
	result, updateErr := m.db.ExecContext(op.ctx, `UPDATE plugins SET enabled=1,approved_digest=?,updated_at=? WHERE id=? AND digest=?`, req.Digest, out.UpdatedAt, req.ID, req.Digest)
	if err = changed(result, updateErr); err != nil {
		return PluginRecord{}, err
	}
	m.finishLocked(req.ID, op)
	m.cancelLocked(req.ID)
	return out, nil
}

func (m *Manager) Remove(ctx context.Context, req PluginRef) error {
	if !validRef(req.ID, req.Digest) {
		return invalid("removal requires a valid ID and exact digest")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.getLocked(ctx, req.ID)
	if err != nil {
		return err
	}
	if current.Digest != req.Digest {
		return ErrChanged
	}
	result, err := m.db.ExecContext(ctx, `DELETE FROM plugins WHERE id=? AND digest=?`, req.ID, req.Digest)
	if err = changed(result, err); err != nil {
		return err
	}
	m.cancelLocked(req.ID)
	return nil
}

func (m *Manager) trustedLocked(ctx context.Context, req PluginRunRequest) (PluginRecord, error) {
	if err := ctx.Err(); err != nil {
		return PluginRecord{}, err
	}
	current, err := m.getLocked(ctx, req.ID)
	if err != nil {
		return PluginRecord{}, err
	}
	if current.Digest != req.Digest {
		return PluginRecord{}, ErrChanged
	}
	if !current.Enabled || current.ApprovedDigest != req.Digest {
		return PluginRecord{}, ErrUntrusted
	}
	return current, nil
}

func (m *Manager) Run(ctx context.Context, req PluginRunRequest) (out PluginRunResult, err error) {
	if !validRef(req.ID, req.Digest) || !validLocalID(req.ActionID) {
		return out, invalid("run requires a valid plugin ID, exact digest and action ID")
	}
	defer func() {
		outcome, detail := "denied", "Plugin action did not complete"
		if err == nil {
			outcome, detail = "allowed", "Plugin action completed"
			if out.IsError {
				outcome, detail = "failed", "MCP tool returned an error result"
			}
		}
		m.audit("plugin:"+req.ID, "plugins.run", req.ID, req.Digest, outcome, detail+"; action="+req.ActionID)
	}()
	m.mu.Lock()
	record, err := m.trustedLocked(ctx, req)
	if err != nil {
		m.mu.Unlock()
		return out, err
	}
	var action *PluginAction
	for i := range record.Manifest.Actions {
		if record.Manifest.Actions[i].ID == req.ActionID {
			action = &record.Manifest.Actions[i]
			break
		}
	}
	if action == nil {
		m.mu.Unlock()
		return out, invalid("unknown action ID")
	}
	if m.invoker == nil {
		m.mu.Unlock()
		return out, errors.New("plugins: MCP invocation is unavailable")
	}
	op := m.trackLocked(ctx, req.ID)
	m.mu.Unlock()
	defer m.finish(req.ID, op)
	value, err := decodeJSON(action.Arguments)
	if err != nil {
		return out, err
	}
	args, ok := value.(map[string]any)
	if !ok {
		return out, invalid("stored action arguments are not an object")
	}
	result, err := m.invoker.InvokeChecked(op.ctx, action.ServerID, action.Tool, args, mcp.CallOptions{
		Actor: "plugin:" + req.ID,
		BeforeCall: func(callCtx context.Context) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			_, err := m.trustedLocked(callCtx, req)
			return err
		},
	})
	var rendered PluginRunResult
	if err == nil && result != nil {
		rendered = plainResult(result)
	}
	// Do not publish late output from a revoked operation, including from a
	// transport which returned successfully without honoring cancellation.
	m.mu.Lock()
	_, stateErr := m.trustedLocked(op.ctx, req)
	// Completing and untracking are one linearized operation with revocation.
	m.finishLocked(req.ID, op)
	m.mu.Unlock()
	if stateErr != nil {
		return out, stateErr
	}
	if err != nil {
		switch {
		case errors.Is(err, policy.ErrDenied), errors.Is(err, policy.ErrApprovalTimeout),
			errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
			errors.Is(err, ErrChanged), errors.Is(err, ErrUntrusted), errors.Is(err, ErrClosed):
			return out, err
		default:
			// Upstream failures may include request bodies or authentication data.
			return out, errors.New("plugins: MCP invocation failed; inspect MCP diagnostics")
		}
	}
	if result == nil {
		return out, errors.New("plugins: MCP returned no result")
	}
	return rendered, nil
}

func (m *Manager) audit(actor, action, id, digest, outcome, detail string) {
	_, err := m.db.Exec(`INSERT INTO audit_events(at,actor,action,target,outcome,detail) VALUES(?,?,?,?,?,?)`,
		time.Now().Unix(), actor, action, "plugins/"+id+"/"+digest, outcome, untrusted.Line(detail, 500))
	if err != nil {
		slog.Warn("plugin audit write failed", "err", err)
	}
}

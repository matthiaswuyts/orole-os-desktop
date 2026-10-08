package machines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

// Keep the entire approval within policy's 2000-byte detail limit. Never trim an
// authorization payload: an omitted argv or grant suffix changes its meaning.
const maxApprovalDetail = 2000

type approvalIdentity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OS          string `json:"os"`
	Transport   string `json:"transport"`
	Backend     string `json:"backend"`
	Address     string `json:"address"`
	Username    string `json:"username"`
	Fingerprint string `json:"fingerprint"`
}

type approvalGrants struct {
	Roots         []string `json:"roots"`
	AllowCommands bool     `json:"allowCommands"`
	AllowWrite    bool     `json:"allowWrite"`
}

func approvalText(s string) bool {
	return len(s) <= maxApprovalDetail && utf8.ValidString(s) && !strings.ContainsRune(s, 0) && untrusted.Text(s, 0) == s
}

func machineIdentity(v Machine) (approvalIdentity, error) {
	identity := approvalIdentity{v.ID, v.Name, v.OS, v.Transport, v.Backend, v.Address, v.Username, v.Fingerprint}
	if v.ID == "local" {
		account, err := user.Current()
		if err != nil {
			return identity, err
		}
		identity.Username = account.Username
	}
	for _, value := range []string{identity.ID, identity.Name, identity.OS, identity.Transport, identity.Backend, identity.Address, identity.Username, identity.Fingerprint} {
		if !approvalText(value) {
			return identity, fmt.Errorf("%w: machine identity cannot be shown completely in approval", ErrInvalid)
		}
	}
	return identity, nil
}

func approvalDetail(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	detail := string(encoded)
	if len(detail) > maxApprovalDetail || untrusted.Text(detail, maxApprovalDetail) != detail {
		return "", fmt.Errorf("%w: complete approval detail exceeds the display limit or contains hidden characters", ErrInvalid)
	}
	return detail, nil
}

func accessDetail(v Machine, roots []string, commands, write bool) (string, error) {
	identity, err := machineIdentity(v)
	if err != nil {
		return "", err
	}
	for _, list := range [][]string{v.Roots, roots} {
		for _, root := range list {
			if !approvalText(root) {
				return "", fmt.Errorf("%w: root cannot be shown completely in approval", ErrInvalid)
			}
		}
	}
	return approvalDetail(struct {
		Target approvalIdentity `json:"target"`
		Old    approvalGrants   `json:"old"`
		New    approvalGrants   `json:"new"`
	}{identity, approvalGrants{v.Roots, v.AllowCommands, v.AllowWrite}, approvalGrants{roots, commands, write}})
}

// accessOperation owns a durable access snapshot and its cancellation context.
// The daemon has one Manager. accessMu serializes snapshot registration and
// mutations with final dispatch; it is never held during an approval or SSH dial.
// File calls hold the guard through their commit, retaining the providers' root,
// etag and write serialization. Command calls release it immediately after Start.
// Revocation cancels active contexts and closes SSH transports, but cannot undo
// remote effects already dispatched or guarantee detached children are stopped.
type accessOperation struct {
	manager *Manager
	machine Machine
	ctx     context.Context
	cancel  context.CancelCauseFunc
}

func (m *Manager) beginAccess(ctx context.Context, id string) (*accessOperation, error) {
	m.accessMu.Lock()
	defer m.accessMu.Unlock()
	if m.closed {
		return nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	op := &accessOperation{manager: m, machine: v, ctx: ctx, cancel: cancel}
	m.operations[op] = struct{}{}
	return op, nil
}

func (op *accessOperation) finish() {
	op.manager.accessMu.Lock()
	delete(op.manager.operations, op)
	op.cancel(context.Canceled)
	op.manager.accessMu.Unlock()
}

func (op *accessOperation) result(err error) error {
	if cause := context.Cause(op.ctx); cause != nil {
		return cause
	}
	return err
}

// validateLocked must run under accessMu, shared with SetAccess and Remove.
func (op *accessOperation) validateLocked() error {
	if err := context.Cause(op.ctx); err != nil {
		return err
	}
	v, err := scan(op.manager.db.QueryRowContext(op.ctx, `SELECT `+columns+` FROM machines WHERE id=?`, op.machine.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccessChanged
	}
	if err != nil {
		return err
	}
	old := op.machine
	if v.accessRevision != old.accessRevision || v.Name != old.Name || v.Kind != old.Kind || v.OS != old.OS || v.Transport != old.Transport || v.Backend != old.Backend || v.Address != old.Address || v.Username != old.Username || v.Fingerprint != old.Fingerprint || v.hostKey != old.hostKey || v.AllowCommands != old.AllowCommands || v.AllowWrite != old.AllowWrite || !slices.Equal(v.Roots, old.Roots) {
		return ErrAccessChanged
	}
	return nil
}

func (op *accessOperation) guard() (func(), error) {
	op.manager.accessMu.RLock()
	if err := op.validateLocked(); err != nil {
		op.manager.accessMu.RUnlock()
		return nil, err
	}
	return op.manager.accessMu.RUnlock, nil
}

// invalidateLocked also cancels pending SetAccess requests so approval of one
// transition cannot later overwrite a different transition the user approved.
func (m *Manager) invalidateLocked(id string, except *accessOperation) {
	for op := range m.operations {
		if op != except && op.machine.ID == id {
			op.cancel(ErrAccessChanged)
		}
	}
}

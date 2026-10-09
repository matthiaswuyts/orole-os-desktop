package manifest

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

// Action and resource used for the trust approval and its audit entries.
const (
	TrustAction = "llm.provider.trust"
)

// TrustResource is the policy/audit resource for one exact manifest digest.
func TrustResource(id, digest string) string { return "llm/" + id + "/" + digest }

// Config wires a Service.
type Config struct {
	DB     *sql.DB
	Policy Authorizer
	// Reserved reports built-in provider ids a manifest must not claim. Optional.
	Reserved func(id string) bool
	// ForgetKey deletes the stored API key for a provider id. It is called when a manifest is
	// removed or its origin changes, so a key reviewed for one host is never reused for another.
	ForgetKey func(id string)
}

type operation struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type life struct {
	digest string
	ctx    context.Context
	cancel context.CancelFunc
}

// Service persists manifests and owns their trust state. Its lock serializes durable
// mutations and is never held while awaiting approval. Revoking, changing or removing a
// manifest (or closing the service) cancels its pending approvals and its Life contexts.
type Service struct {
	cfg    Config
	mu     sync.Mutex
	closed bool
	active map[string]map[*operation]struct{}
	lives  map[string]*life
}

// New returns a Service over cfg.DB (migration 0098).
func New(cfg Config) *Service {
	return &Service{cfg: cfg, active: map[string]map[*operation]struct{}{}, lives: map[string]*life{}}
}

// Close cancels every pending approval and every provider built from a manifest.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for id := range s.active {
		s.cancelLocked(id)
	}
	for id := range s.lives {
		s.cancelLocked(id)
	}
}

func (s *Service) cancelLocked(id string) {
	for op := range s.active[id] {
		op.cancel()
	}
	delete(s.active, id)
	if l := s.lives[id]; l != nil {
		l.cancel()
		delete(s.lives, id)
	}
}

func (s *Service) trackLocked(ctx context.Context, id string) *operation {
	ctx, cancel := context.WithCancel(ctx)
	op := &operation{ctx: ctx, cancel: cancel}
	if s.active[id] == nil {
		s.active[id] = map[*operation]struct{}{}
	}
	s.active[id][op] = struct{}{}
	return op
}

func (s *Service) finishLocked(id string, op *operation) {
	op.cancel()
	delete(s.active[id], op)
	if len(s.active[id]) == 0 {
		delete(s.active, id)
	}
}

func (s *Service) finish(id string, op *operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishLocked(id, op)
}

const columns = `id,manifest,digest,enabled,approved_digest,updated_at`

type scanner interface{ Scan(...any) error }

func (s *Service) scan(row scanner) (ManifestRecord, error) {
	var out ManifestRecord
	var id string
	var raw []byte
	err := row.Scan(&id, &raw, &out.Digest, &out.Enabled, &out.ApprovedDigest, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	stored, err := Parse(raw)
	if err != nil {
		return out, invalid("stored manifest is invalid")
	}
	norm, _, digest, err := Validate(stored, s.cfg.Reserved)
	if err != nil || id != norm.ID || digest != out.Digest || (out.Enabled && out.ApprovedDigest != digest) {
		return out, invalid("stored manifest integrity check failed")
	}
	out.Manifest = norm
	return out, nil
}

func (s *Service) getLocked(ctx context.Context, id string) (ManifestRecord, error) {
	if s.closed {
		return ManifestRecord{}, ErrClosed
	}
	return s.scan(s.cfg.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM llm_manifests WHERE id=?`, id))
}

// List returns every stored manifest ordered by id. A row that fails its integrity check is an error.
func (s *Service) List(ctx context.Context) ([]ManifestRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	rows, err := s.cfg.DB.QueryContext(ctx, `SELECT `+columns+` FROM llm_manifests ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManifestRecord{}
	for rows.Next() {
		rec, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Save stores a manifest. A new id requires an empty ExpectedDigest and lands disabled. An
// existing id requires its exact current digest; a changed manifest is disabled and its
// approval cleared (a format-only edit with the same canonical digest keeps its trust).
func (s *Service) Save(ctx context.Context, req ManifestSaveRequest) (ManifestRecord, error) {
	norm, canonical, digest, err := Validate(req.Manifest, s.cfg.Reserved)
	if err != nil {
		return ManifestRecord{}, err
	}
	if req.ExpectedDigest != "" && !ValidDigest(req.ExpectedDigest) {
		return ManifestRecord{}, invalid("expectedDigest must be empty for creation or an exact SHA-256 digest")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.getLocked(ctx, norm.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ManifestRecord{}, err
	}
	if errors.Is(err, ErrNotFound) {
		if req.ExpectedDigest != "" {
			return ManifestRecord{}, ErrChanged
		}
		out := ManifestRecord{Manifest: norm, Digest: digest, UpdatedAt: time.Now().UnixMilli()}
		_, err = s.cfg.DB.ExecContext(ctx, `INSERT INTO llm_manifests(id,manifest,digest,enabled,approved_digest,updated_at) VALUES(?,?,?,0,'',?)`,
			norm.ID, string(canonical), digest, out.UpdatedAt)
		return out, err
	}
	if req.ExpectedDigest != current.Digest {
		return ManifestRecord{}, ErrChanged
	}
	if digest == current.Digest {
		return current, nil
	}
	out := ManifestRecord{Manifest: norm, Digest: digest, UpdatedAt: time.Now().UnixMilli()}
	result, err := s.cfg.DB.ExecContext(ctx, `UPDATE llm_manifests SET manifest=?,digest=?,enabled=0,approved_digest='',updated_at=? WHERE id=? AND digest=?`,
		string(canonical), digest, out.UpdatedAt, norm.ID, current.Digest)
	if err = changed(result, err); err != nil {
		return ManifestRecord{}, err
	}
	s.cancelLocked(norm.ID)
	if current.Manifest.Origin() != norm.Origin() {
		s.forgetKey(norm.ID)
	}
	return out, nil
}

func (s *Service) forgetKey(id string) {
	if s.cfg.ForgetKey != nil {
		s.cfg.ForgetKey(id)
	}
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

// Trust enables the exact digest after a destructive-style policy approval (the prompt names the
// host and capabilities), or disables it and clears the approval. Denial leaves it disabled.
func (s *Service) Trust(ctx context.Context, req ManifestTrustRequest) (out ManifestRecord, err error) {
	if !validRef(req.ID, req.Digest) {
		return out, invalid("trust requires a valid ID and exact digest")
	}
	defer func() {
		outcome, detail := "denied", "AI provider trust change did not complete"
		if err == nil {
			outcome, detail = "allowed", "AI provider trust revoked"
			if req.Enabled {
				detail = "Exact AI provider manifest enabled after central approval"
			}
		}
		s.audit("user", TrustAction, req.ID, req.Digest, outcome, detail)
	}()
	s.mu.Lock()
	out, err = s.getLocked(ctx, req.ID)
	if err == nil && out.Digest != req.Digest {
		err = ErrChanged
	}
	if err != nil {
		s.mu.Unlock()
		return ManifestRecord{}, err
	}
	if !req.Enabled {
		out.Enabled, out.ApprovedDigest, out.UpdatedAt = false, "", time.Now().UnixMilli()
		result, updateErr := s.cfg.DB.ExecContext(ctx, `UPDATE llm_manifests SET enabled=0,approved_digest='',updated_at=? WHERE id=? AND digest=?`, out.UpdatedAt, req.ID, req.Digest)
		err = changed(result, updateErr)
		if err == nil {
			s.cancelLocked(req.ID)
		}
		s.mu.Unlock()
		return out, err
	}
	if s.cfg.Policy == nil {
		s.mu.Unlock()
		return ManifestRecord{}, policy.ErrDenied
	}
	op := s.trackLocked(ctx, req.ID)
	detail := TrustDetail(out.Manifest, req.Digest)
	s.mu.Unlock()
	defer s.finish(req.ID, op)
	err = s.cfg.Policy.Require(op.ctx, policy.Request{
		Actor: "user", Action: TrustAction, Resource: TrustResource(req.ID, req.Digest),
		Effect: policy.EffectDestructive, Detail: detail,
	})
	if err != nil {
		return ManifestRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A revoke, change-away-and-back, removal or shutdown invalidates this operation
	// even if a later record happens to have the same digest.
	if err = op.ctx.Err(); err != nil {
		return ManifestRecord{}, err
	}
	out, err = s.getLocked(op.ctx, req.ID)
	if err != nil {
		return ManifestRecord{}, err
	}
	if out.Digest != req.Digest {
		return ManifestRecord{}, ErrChanged
	}
	out.Enabled, out.ApprovedDigest, out.UpdatedAt = true, req.Digest, time.Now().UnixMilli()
	result, updateErr := s.cfg.DB.ExecContext(op.ctx, `UPDATE llm_manifests SET enabled=1,approved_digest=?,updated_at=? WHERE id=? AND digest=?`, req.Digest, out.UpdatedAt, req.ID, req.Digest)
	if err = changed(result, updateErr); err != nil {
		return ManifestRecord{}, err
	}
	s.finishLocked(req.ID, op)
	s.cancelLocked(req.ID)
	return out, nil
}

// Remove deletes the exact manifest, cancels providers built from it and forgets its stored key.
func (s *Service) Remove(ctx context.Context, req ManifestRef) error {
	if !validRef(req.ID, req.Digest) {
		return invalid("removal requires a valid ID and exact digest")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.getLocked(ctx, req.ID)
	if err != nil {
		return err
	}
	if current.Digest != req.Digest {
		return ErrChanged
	}
	result, err := s.cfg.DB.ExecContext(ctx, `DELETE FROM llm_manifests WHERE id=? AND digest=?`, req.ID, req.Digest)
	if err = changed(result, err); err != nil {
		return err
	}
	s.cancelLocked(req.ID)
	s.forgetKey(req.ID)
	return nil
}

// Trusted lists the enabled manifests whose digest was approved, each with a Life context that
// ends when the manifest is revoked, changed or removed. A stored row that fails its integrity
// check is not trusted.
func (s *Service) Trusted() []Trusted {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.cfg.DB.QueryContext(ctx, `SELECT `+columns+` FROM llm_manifests WHERE enabled=1 AND approved_digest=digest ORDER BY id`)
	if err != nil {
		slog.Warn("llm manifests unavailable", "err", err)
		return nil
	}
	defer rows.Close()
	var out []Trusted
	for rows.Next() {
		rec, err := s.scan(rows)
		if err != nil {
			slog.Warn("llm manifest skipped", "err", err)
			continue
		}
		id := rec.Manifest.ID
		l := s.lives[id]
		if l != nil && l.digest != rec.Digest {
			l.cancel()
			l = nil
		}
		if l == nil {
			lctx, lcancel := context.WithCancel(context.Background())
			l = &life{digest: rec.Digest, ctx: lctx, cancel: lcancel}
			s.lives[id] = l
		}
		out = append(out, Trusted{Manifest: rec.Manifest, Digest: rec.Digest, Life: l.ctx})
	}
	return out
}

func (s *Service) audit(actor, action, id, digest, outcome, detail string) {
	_, err := s.cfg.DB.Exec(`INSERT INTO audit_events(at,actor,action,target,outcome,detail) VALUES(?,?,?,?,?,?)`,
		time.Now().Unix(), actor, action, TrustResource(id, digest), outcome, untrusted.Line(detail, 500))
	if err != nil {
		slog.Warn("llm manifest audit write failed", "err", err)
	}
}

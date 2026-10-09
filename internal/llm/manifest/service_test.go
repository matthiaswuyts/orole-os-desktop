package manifest

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type authorizeFunc func(context.Context, policy.Request) error

func (f authorizeFunc) Require(ctx context.Context, req policy.Request) error { return f(ctx, req) }

type env struct {
	svc    *Service
	db     *database.DB
	mu     sync.Mutex
	reqs   []policy.Request
	forget []string
	allow  bool
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "llm.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := &env{db: db, allow: true}
	e.svc = New(Config{
		DB: db.DB,
		Policy: authorizeFunc(func(_ context.Context, r policy.Request) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.reqs = append(e.reqs, r)
			if !e.allow {
				return policy.ErrDenied
			}
			return nil
		}),
		Reserved:  func(id string) bool { return id == "reserved" },
		ForgetKey: func(id string) { e.mu.Lock(); e.forget = append(e.forget, id); e.mu.Unlock() },
	})
	t.Cleanup(e.svc.Close)
	return e
}

func (e *env) save(t *testing.T, m ProviderManifest, expected string) ManifestRecord {
	t.Helper()
	rec, err := e.svc.Save(context.Background(), ManifestSaveRequest{Manifest: m, ExpectedDigest: expected})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func (e *env) enable(t *testing.T, rec ManifestRecord) ManifestRecord {
	t.Helper()
	out, err := e.svc.Trust(context.Background(), ManifestTrustRequest{ID: rec.Manifest.ID, Digest: rec.Digest, Enabled: true})
	if err != nil || !out.Enabled || out.ApprovedDigest != rec.Digest {
		t.Fatalf("enable: %+v, %v", out, err)
	}
	return out
}

func TestImportLandsDisabledAndEnableAsksPolicy(t *testing.T) {
	e := newEnv(t)
	rec := e.save(t, fixture(), "")
	if rec.Enabled || rec.ApprovedDigest != "" || rec.UpdatedAt <= 0 {
		t.Fatalf("import trusted: %+v", rec)
	}
	if len(e.svc.Trusted()) != 0 {
		t.Fatal("disabled manifest is trusted")
	}
	trusted := e.enable(t, rec)
	if len(e.reqs) != 1 {
		t.Fatalf("policy asked %d times", len(e.reqs))
	}
	r := e.reqs[0]
	if r.Actor != "user" || r.Action != "llm.provider.trust" || r.Resource != "llm/ollama/"+rec.Digest ||
		r.Effect != policy.EffectDestructive || r.FromHint {
		t.Fatalf("policy request: %+v", r)
	}
	if !strings.Contains(r.Detail, "localhost:11434") || !strings.Contains(r.Detail, rec.Digest) || !strings.Contains(r.Detail, "Capabilities") {
		t.Fatalf("approval detail: %s", r.Detail)
	}
	got := e.svc.Trusted()
	if len(got) != 1 || got[0].Manifest.ID != "ollama" || got[0].Digest != trusted.Digest || got[0].Life.Err() != nil {
		t.Fatalf("trusted: %+v", got)
	}
	var audited int
	if err := e.db.QueryRow(`SELECT count(*) FROM audit_events WHERE action='llm.provider.trust' AND outcome='allowed' AND target=?`, "llm/ollama/"+rec.Digest).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("audit rows %d, %v", audited, err)
	}
}

func TestPolicyDenialLeavesManifestDisabled(t *testing.T) {
	e := newEnv(t)
	rec := e.save(t, fixture(), "")
	e.allow = false
	_, err := e.svc.Trust(context.Background(), ManifestTrustRequest{ID: "ollama", Digest: rec.Digest, Enabled: true})
	if !errors.Is(err, policy.ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	list, err := e.svc.List(context.Background())
	if err != nil || len(list) != 1 || list[0].Enabled || list[0].ApprovedDigest != "" {
		t.Fatalf("denied manifest was enabled: %+v, %v", list, err)
	}
	if len(e.svc.Trusted()) != 0 {
		t.Fatal("denied manifest is trusted")
	}
	var denied int
	if err := e.db.QueryRow(`SELECT count(*) FROM audit_events WHERE action='llm.provider.trust' AND outcome='denied'`).Scan(&denied); err != nil || denied != 1 {
		t.Fatalf("denied audit rows %d, %v", denied, err)
	}
	// With no policy engine at all, enabling is refused too.
	bare := New(Config{DB: e.db.DB})
	if _, err := bare.Trust(context.Background(), ManifestTrustRequest{ID: "ollama", Digest: rec.Digest, Enabled: true}); !errors.Is(err, policy.ErrDenied) {
		t.Fatalf("no policy engine: %v", err)
	}
}

func TestChangedManifestLosesApprovalAndLife(t *testing.T) {
	e := newEnv(t)
	rec := e.enable(t, e.save(t, fixture(), ""))
	life := e.svc.Trusted()[0].Life

	// A format-only save keeps the canonical digest and the trust.
	same := fixture()
	same.BaseURL += "/"
	kept := e.save(t, same, rec.Digest)
	if !kept.Enabled || kept.Digest != rec.Digest || life.Err() != nil {
		t.Fatalf("format-only save lost trust: %+v", kept)
	}

	edited := fixture()
	edited.Description = "different"
	changed := e.save(t, edited, rec.Digest)
	if changed.Enabled || changed.ApprovedDigest != "" || changed.Digest == rec.Digest {
		t.Fatalf("changed manifest kept trust: %+v", changed)
	}
	if life.Err() == nil {
		t.Fatal("in-flight providers were not cancelled")
	}
	if len(e.svc.Trusted()) != 0 {
		t.Fatal("changed manifest is still trusted")
	}
	if len(e.forget) != 0 {
		t.Fatalf("key forgotten without an origin change: %v", e.forget)
	}
	// Approving the old digest is refused: the reviewed document is gone.
	if _, err := e.svc.Trust(context.Background(), ManifestTrustRequest{ID: "ollama", Digest: rec.Digest, Enabled: true}); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale digest trust: %v", err)
	}
	// A stale expectedDigest cannot overwrite.
	if _, err := e.svc.Save(context.Background(), ManifestSaveRequest{Manifest: edited, ExpectedDigest: rec.Digest}); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale save: %v", err)
	}
	// Creating over an existing id needs the current digest.
	if _, err := e.svc.Save(context.Background(), ManifestSaveRequest{Manifest: edited}); !errors.Is(err, ErrChanged) {
		t.Fatalf("blind overwrite: %v", err)
	}
}

func TestOriginChangeForgetsKeyAndRemoveToo(t *testing.T) {
	e := newEnv(t)
	rec := e.enable(t, e.save(t, fixture(), ""))
	moved := fixture()
	moved.BaseURL = "http://localhost:9999/v1"
	rec2 := e.save(t, moved, rec.Digest)
	if len(e.forget) != 1 || e.forget[0] != "ollama" {
		t.Fatalf("forget after origin change: %v", e.forget)
	}
	e.enable(t, rec2)
	l := e.svc.Trusted()[0].Life
	if err := e.svc.Remove(context.Background(), ManifestRef{ID: "ollama", Digest: rec2.Digest}); err != nil {
		t.Fatal(err)
	}
	if l.Err() == nil || len(e.svc.Trusted()) != 0 || len(e.forget) != 2 {
		t.Fatalf("remove: life=%v trusted=%d forget=%v", l.Err(), len(e.svc.Trusted()), e.forget)
	}
	if err := e.svc.Remove(context.Background(), ManifestRef{ID: "ollama", Digest: rec2.Digest}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestRevokeCancelsAndClears(t *testing.T) {
	e := newEnv(t)
	rec := e.enable(t, e.save(t, fixture(), ""))
	life := e.svc.Trusted()[0].Life
	out, err := e.svc.Trust(context.Background(), ManifestTrustRequest{ID: "ollama", Digest: rec.Digest, Enabled: false})
	if err != nil || out.Enabled || out.ApprovedDigest != "" {
		t.Fatalf("revoke: %+v, %v", out, err)
	}
	if life.Err() == nil || len(e.svc.Trusted()) != 0 {
		t.Fatal("revoked manifest still usable")
	}
}

func TestRevokeDuringApprovalInvalidatesIt(t *testing.T) {
	e := newEnv(t)
	rec := e.save(t, fixture(), "")
	asked := make(chan struct{})
	release := make(chan struct{})
	e.svc.cfg.Policy = authorizeFunc(func(ctx context.Context, _ policy.Request) error {
		close(asked)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() {
		_, err := e.svc.Trust(context.Background(), ManifestTrustRequest{ID: "ollama", Digest: rec.Digest, Enabled: true})
		done <- err
	}()
	<-asked
	if _, err := e.svc.Trust(context.Background(), ManifestTrustRequest{ID: "ollama", Digest: rec.Digest, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("approval completed after revocation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending approval was not cancelled")
	}
	if len(e.svc.Trusted()) != 0 {
		t.Fatal("manifest enabled by a revoked approval")
	}
}

func TestReservedAndInvalidRequests(t *testing.T) {
	e := newEnv(t)
	m := fixture()
	m.ID = "reserved"
	if _, err := e.svc.Save(context.Background(), ManifestSaveRequest{Manifest: m}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reserved id: %v", err)
	}
	if _, err := e.svc.Save(context.Background(), ManifestSaveRequest{Manifest: fixture(), ExpectedDigest: "nothex"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad expected digest: %v", err)
	}
	if _, err := e.svc.Trust(context.Background(), ManifestTrustRequest{ID: "ollama", Digest: "short", Enabled: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad digest: %v", err)
	}
	if err := e.svc.Remove(context.Background(), ManifestRef{ID: "ollama", Digest: strings.Repeat("a", 64)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestStoredRowIntegrity(t *testing.T) {
	e := newEnv(t)
	rec := e.enable(t, e.save(t, fixture(), ""))
	// The schema refuses an enabled row whose approval is not its own digest.
	if _, err := e.db.Exec(`UPDATE llm_manifests SET approved_digest=? WHERE id='ollama'`, strings.Repeat("0", 64)); err == nil {
		t.Fatal("CHECK constraint did not reject mismatched approval")
	}
	// A tampered document no longer matches its digest and is not trusted.
	tampered := strings.Replace(rec.Manifest.Name, "Ollama", "Evil", 1)
	if _, err := e.db.Exec(`UPDATE llm_manifests SET manifest=replace(manifest, 'Ollama', ?) WHERE id='ollama'`, tampered); err != nil {
		t.Fatal(err)
	}
	if got := e.svc.Trusted(); len(got) != 0 {
		t.Fatalf("tampered manifest trusted: %+v", got)
	}
	if _, err := e.svc.List(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("list of tampered row: %v", err)
	}
	// Oversize documents are refused by the schema.
	if _, err := e.db.Exec(`UPDATE llm_manifests SET manifest=? WHERE id='ollama'`, strings.Repeat("x", 40000)); err == nil {
		t.Fatal("oversize manifest accepted by the schema")
	}
}

func TestCloseCancelsLives(t *testing.T) {
	e := newEnv(t)
	e.enable(t, e.save(t, fixture(), ""))
	life := e.svc.Trusted()[0].Life
	e.svc.Close()
	if life.Err() == nil {
		t.Fatal("close did not cancel providers")
	}
	if _, err := e.svc.List(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("list after close: %v", err)
	}
}

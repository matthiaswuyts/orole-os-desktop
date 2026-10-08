package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type authorizeFunc func(context.Context, policy.Request) error

func (f authorizeFunc) Require(ctx context.Context, req policy.Request) error { return f(ctx, req) }

type invokeFunc func(context.Context, string, string, map[string]any, mcp.CallOptions) (*mcp.CallResult, error)

func (f invokeFunc) InvokeChecked(ctx context.Context, server, tool string, args map[string]any, opts mcp.CallOptions) (*mcp.CallResult, error) {
	return f(ctx, server, tool, args, opts)
}

func fixture(t *testing.T) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugins.db")
	db, err := database.Open(path, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m := New(db.DB, authorizeFunc(func(context.Context, policy.Request) error { return nil }), nil)
	t.Cleanup(m.Close)
	return m, path
}

func saveFixture(t *testing.T, m *Manager) PluginRecord {
	t.Helper()
	record, err := m.Save(context.Background(), PluginSaveRequest{Manifest: manifestFixture()})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func enable(t *testing.T, m *Manager, record PluginRecord) PluginRecord {
	t.Helper()
	trusted, err := m.Trust(context.Background(), PluginTrustRequest{ID: record.Manifest.ID, Digest: record.Digest, Enabled: true})
	if err != nil || !trusted.Enabled || trusted.ApprovedDigest != record.Digest {
		t.Fatalf("enable: %+v, %v", trusted, err)
	}
	return trusted
}

func runRequest(record PluginRecord) PluginRunRequest {
	return PluginRunRequest{ID: record.Manifest.ID, Digest: record.Digest, ActionID: "read"}
}

func TestPersistenceCanonicalSaveAndOptimisticMutations(t *testing.T) {
	ctx := context.Background()
	m, path := fixture(t)
	created := saveFixture(t, m)
	if created.Enabled || created.ApprovedDigest != "" || created.UpdatedAt <= 0 {
		t.Fatalf("fresh import trusted: %+v", created)
	}
	trusted := enable(t, m, created)
	unchanged := manifestFixture()
	unchanged.Actions[0].Arguments = json.RawMessage(`{ "scope" : "current" }`)
	same, err := m.Save(ctx, PluginSaveRequest{Manifest: unchanged, ExpectedDigest: trusted.Digest})
	if err != nil || !same.Enabled || same.Digest != trusted.Digest {
		t.Fatalf("format-only save lost trust: %+v, %v", same, err)
	}
	m.Close()
	if err := m.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(path, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reopened := New(db.DB, m.policy, nil)
	defer reopened.Close()
	items, err := reopened.List(ctx)
	if err != nil || len(items) != 1 || !items[0].Enabled || items[0].ApprovedDigest != trusted.Digest {
		t.Fatalf("durable trust: %+v, %v", items, err)
	}
	changedManifest := manifestFixture()
	changedManifest.Version = "2.0.0"
	updated, err := reopened.Save(ctx, PluginSaveRequest{Manifest: changedManifest, ExpectedDigest: trusted.Digest})
	if err != nil || updated.Enabled || updated.ApprovedDigest != "" || updated.Digest == trusted.Digest {
		t.Fatalf("changed manifest retained trust: %+v, %v", updated, err)
	}
	if _, err := reopened.Save(ctx, PluginSaveRequest{Manifest: changedManifest, ExpectedDigest: trusted.Digest}); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale update: %v", err)
	}
	if _, err := reopened.Trust(ctx, PluginTrustRequest{ID: trusted.Manifest.ID, Digest: trusted.Digest, Enabled: true}); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale trust: %v", err)
	}
	if err := reopened.Remove(ctx, PluginRef{ID: trusted.Manifest.ID, Digest: trusted.Digest}); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale remove: %v", err)
	}
	if err := reopened.Remove(ctx, PluginRef{ID: updated.Manifest.ID, Digest: updated.Digest}); err != nil {
		t.Fatal(err)
	}
	items, err = reopened.List(ctx)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("removed plugin remains: %+v, %v", items, err)
	}
}

func TestRunFailsClosedBeforeInvocation(t *testing.T) {
	m, _ := fixture(t)
	record := saveFixture(t, m)
	calls := 0
	m.invoker = invokeFunc(func(context.Context, string, string, map[string]any, mcp.CallOptions) (*mcp.CallResult, error) {
		calls++
		return nil, nil
	})
	if _, err := m.Run(context.Background(), runRequest(record)); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("fresh import ran: %v", err)
	}
	m.policy = nil
	if _, err := m.Trust(context.Background(), PluginTrustRequest{ID: record.Manifest.ID, Digest: record.Digest, Enabled: true}); !errors.Is(err, policy.ErrDenied) {
		t.Fatalf("missing policy trusted manifest: %v", err)
	}
	m.policy = authorizeFunc(func(_ context.Context, req policy.Request) error {
		if req.Actor != "user" || req.Effect != policy.EffectDestructive || req.FromHint || req.Action != "plugins.trust" ||
			!strings.Contains(req.Resource, record.Digest) || !strings.Contains(req.Detail, "local-server / status") {
			t.Fatalf("incomplete or unsafe approval request: %+v", req)
		}
		return policy.ErrDenied
	})
	if _, err := m.Trust(context.Background(), PluginTrustRequest{ID: record.Manifest.ID, Digest: record.Digest, Enabled: true}); !errors.Is(err, policy.ErrDenied) {
		t.Fatalf("denied trust enabled manifest: %v", err)
	}
	if _, err := m.Run(context.Background(), runRequest(record)); !errors.Is(err, ErrUntrusted) || calls != 0 {
		t.Fatalf("denied plugin reached MCP: calls=%d, err=%v", calls, err)
	}
}

func TestTrustedRunStillUsesCheckedExactCapability(t *testing.T) {
	m, _ := fixture(t)
	manifest := manifestFixture()
	manifest.Actions[0].Arguments = json.RawMessage(`{"scope":"audit-sentinel-secret","number":9007199254740993}`)
	record, err := m.Save(context.Background(), PluginSaveRequest{Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	enable(t, m, record)
	calls := 0
	m.invoker = invokeFunc(func(ctx context.Context, server, tool string, args map[string]any, opts mcp.CallOptions) (*mcp.CallResult, error) {
		calls++
		if server != "local-server" || tool != "status" || opts.Actor != "plugin:example.status" || opts.BeforeCall == nil ||
			args["scope"] != "audit-sentinel-secret" || args["number"] != json.Number("9007199254740993") {
			t.Fatalf("invocation escaped configured capability: %s, %s, %+v, %+v", server, tool, args, opts)
		}
		if err := opts.BeforeCall(ctx); err != nil {
			t.Fatal(err)
		}
		return nil, policy.ErrDenied
	})
	if _, err := m.Run(context.Background(), runRequest(record)); !errors.Is(err, policy.ErrDenied) || calls != 1 {
		t.Fatalf("plugin trust bypassed MCP denial: calls=%d, err=%v", calls, err)
	}
	unknown := runRequest(record)
	unknown.ActionID = "delete"
	if _, err := m.Run(context.Background(), unknown); !errors.Is(err, ErrInvalid) || calls != 1 {
		t.Fatalf("unknown action invoked MCP: calls=%d, err=%v", calls, err)
	}
	var leaked int
	if err := m.db.QueryRow(`SELECT count(*) FROM audit_events WHERE detail LIKE '%audit-sentinel-secret%'`).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("plugin arguments leaked to audit: count=%d, err=%v", leaked, err)
	}
}

func TestPendingTrustCannotSurviveManifestOrTrustMutation(t *testing.T) {
	for _, mutation := range []string{"change", "revoke", "remove", "close", "change away and back"} {
		t.Run(mutation, func(t *testing.T) {
			m, _ := fixture(t)
			record := saveFixture(t, m)
			m.policy = authorizeFunc(func(ctx context.Context, _ policy.Request) error {
				mutate(t, m, record, mutation)
				if ctx.Err() == nil {
					t.Fatal("pending trust was not canceled")
				}
				// Model a concurrent approval already queued before cancellation.
				return nil
			})
			if _, err := m.Trust(context.Background(), PluginTrustRequest{ID: record.Manifest.ID, Digest: record.Digest, Enabled: true}); err == nil {
				t.Fatal("stale approval resurrected trust")
			}
			if mutation != "close" {
				items, err := m.List(context.Background())
				if err != nil || (len(items) != 0 && (items[0].Enabled || items[0].ApprovedDigest != "")) {
					t.Fatalf("mutation retained trust: %+v, %v", items, err)
				}
			}
		})
	}
}

func mutate(t *testing.T, m *Manager, record PluginRecord, mutation string) {
	t.Helper()
	ctx := context.Background()
	switch mutation {
	case "change", "change away and back":
		manifest := manifestFixture()
		manifest.Version = "2"
		changed, err := m.Save(ctx, PluginSaveRequest{Manifest: manifest, ExpectedDigest: record.Digest})
		if err != nil {
			t.Fatal(err)
		}
		if mutation == "change away and back" {
			if _, err := m.Save(ctx, PluginSaveRequest{Manifest: record.Manifest, ExpectedDigest: changed.Digest}); err != nil {
				t.Fatal(err)
			}
		}
	case "revoke":
		if _, err := m.Trust(ctx, PluginTrustRequest{ID: record.Manifest.ID, Digest: record.Digest, Enabled: false}); err != nil {
			t.Fatal(err)
		}
	case "remove":
		if err := m.Remove(ctx, PluginRef{ID: record.Manifest.ID, Digest: record.Digest}); err != nil {
			t.Fatal(err)
		}
	case "close":
		m.Close()
	default:
		t.Fatalf("unknown mutation %s", mutation)
	}
}

func TestMutationCancelsPendingMCPAndRejectsLateOutput(t *testing.T) {
	for _, mutation := range []string{"change", "revoke", "remove", "close"} {
		t.Run(mutation, func(t *testing.T) {
			m, _ := fixture(t)
			record := enable(t, m, saveFixture(t, m))
			started := make(chan struct{})
			m.invoker = invokeFunc(func(ctx context.Context, _ string, _ string, _ map[string]any, opts mcp.CallOptions) (*mcp.CallResult, error) {
				close(started)
				<-ctx.Done()
				// An implementation that returns late success must not leak stale output.
				return &mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: "stale output"}}}, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := m.Run(ctx, runRequest(record))
				finished <- err
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("MCP invocation did not start")
			}
			mutate(t, m, record, mutation)
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("mutation returned late result or failed to cancel: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("mutation did not cancel MCP wait")
			}
		})
	}
}

func TestDispatchGuardRechecksDurableDigestAfterPolicyWait(t *testing.T) {
	m, _ := fixture(t)
	record := enable(t, m, saveFixture(t, m))
	dispatched := false
	m.invoker = invokeFunc(func(ctx context.Context, _ string, _ string, _ map[string]any, opts mcp.CallOptions) (*mcp.CallResult, error) {
		// Change storage without the manager's cancellation hook. The dispatch
		// guard must independently re-read the durable exact trust identity.
		manifest := manifestFixture()
		manifest.Version = "2"
		_, raw, digest, err := validateManifest(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.db.Exec(`UPDATE plugins SET manifest=?,digest=?,enabled=0,approved_digest='' WHERE id=?`, raw, digest, record.Manifest.ID); err != nil {
			t.Fatal(err)
		}
		if err := opts.BeforeCall(ctx); err != nil {
			return nil, err
		}
		dispatched = true
		return &mcp.CallResult{}, nil
	})
	if _, err := m.Run(context.Background(), runRequest(record)); !errors.Is(err, ErrChanged) || dispatched {
		t.Fatalf("stale tool dispatched: dispatched=%v, err=%v", dispatched, err)
	}
}

type pendingPublisher struct{ pending chan policy.Pending }

func (p pendingPublisher) Publish(event string, data any) {
	if event != "policy.pending" {
		return
	}
	for _, item := range data.(map[string]any)["items"].([]policy.Pending) {
		p.pending <- item
	}
}

func TestTrustUsesRealDestructiveCentralApproval(t *testing.T) {
	m, _ := fixture(t)
	record := saveFixture(t, m)
	pub := pendingPublisher{pending: make(chan policy.Pending, 2)}
	engine := policy.New(m.db, pub, nil)
	m.policy = engine
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := m.Trust(ctx, PluginTrustRequest{ID: record.Manifest.ID, Digest: record.Digest, Enabled: true})
		finished <- err
	}()
	var pending policy.Pending
	select {
	case pending = <-pub.pending:
	case <-ctx.Done():
		t.Fatal("central approval was not requested")
	}
	if pending.CanRemember || pending.Effect != policy.EffectDestructive || !strings.Contains(pending.Detail, record.Digest) || !strings.Contains(pending.Detail, "local-server / status") {
		t.Fatalf("wrong approval: %+v", pending)
	}
	if err := engine.Resolve(ctx, pending.ID, policy.Resolution{Allow: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("resolved approval did not complete")
	}
	items, err := m.List(ctx)
	if err != nil || len(items) != 1 || !items[0].Enabled || items[0].ApprovedDigest != record.Digest {
		t.Fatalf("approved identity not trusted: %+v, %v", items, err)
	}
	var trusted int
	if err := m.db.QueryRow(`SELECT count(*) FROM audit_events WHERE action='plugins.trust' AND outcome='allowed'`).Scan(&trusted); err != nil {
		t.Fatal(err)
	}
	if trusted == 0 {
		t.Fatal("trust approval was not audited")
	}
}

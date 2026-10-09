package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/manifest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type memSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSettings) Get(_ context.Context, k string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	return v, ok, nil
}

func (s *memSettings) Set(_ context.Context, k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[k] = v
	return nil
}

type allowAll struct{}

func (allowAll) Require(context.Context, policy.Request) error { return nil }

type managerEnv struct {
	mgr   *llm.Manager
	svc   *manifest.Service
	creds *credentials.Memory
}

// newManagerEnv wires the real migration, manifest service, source and Manager together.
func newManagerEnv(t *testing.T) *managerEnv {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "llm.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := &managerEnv{creds: credentials.NewMemory()}
	e.svc = manifest.New(manifest.Config{
		DB: db.DB, Policy: allowAll{},
		ForgetKey: func(id string) { _ = e.creds.Delete(llm.CredentialKey(id)) },
	})
	t.Cleanup(e.svc.Close)
	reg, err := llm.NewRegistry(llm.Descriptor{
		ID: "openai", Name: "OpenAI",
		New: func(llm.Config) (llm.Provider, error) { return nil, errors.New("not used") },
	})
	if err != nil {
		t.Fatal(err)
	}
	e.mgr, err = llm.NewManager(llm.ManagerConfig{
		Registry: reg, Credentials: e.creds, Settings: &memSettings{}, Dynamic: &Source{Service: e.svc},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *managerEnv) statuses(t *testing.T) map[string]llm.ProviderStatus {
	t.Helper()
	list, err := e.mgr.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]llm.ProviderStatus{}
	for _, st := range list {
		out[st.ID] = st
	}
	return out
}

func (e *managerEnv) enable(t *testing.T, m manifest.ProviderManifest) manifest.ManifestRecord {
	t.Helper()
	ctx := context.Background()
	rec, err := e.svc.Save(ctx, manifest.ManifestSaveRequest{Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	rec, err = e.svc.Trust(ctx, manifest.ManifestTrustRequest{ID: rec.Manifest.ID, Digest: rec.Digest, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func keyedServer(t *testing.T, blocked chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			http.Error(w, `{"error":{"message":"invalid key"}}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"m1"}]}`)
		case "/v1/chat/completions":
			if blocked != nil {
				sse(w, `{"choices":[{"delta":{"content":"partial"}}]}`)
				close(blocked)
				<-r.Context().Done()
				return
			}
			sse(w, `{"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`, `[DONE]`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestManagerListsTrustedManifestAndDropsRevoked(t *testing.T) {
	ctx := context.Background()
	e := newManagerEnv(t)
	blocked := make(chan struct{})
	srv := keyedServer(t, blocked)
	m := testManifest(srv)

	rec, err := e.svc.Save(ctx, manifest.ManifestSaveRequest{Manifest: m})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.statuses(t)["gateway"]; ok {
		t.Fatal("an imported, unapproved manifest is listed as a provider")
	}
	rec, err = e.svc.Trust(ctx, manifest.ManifestTrustRequest{ID: "gateway", Digest: rec.Digest, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	st, ok := e.statuses(t)["gateway"]
	if !ok || st.Kind != "manifest" || st.Name != "Gateway" || st.Connected {
		t.Fatalf("status: %+v ok=%v", st, ok)
	}
	if _, ok := e.statuses(t)["openai"]; !ok {
		t.Fatal("built-in provider vanished")
	}
	if _, err := e.mgr.Provider(ctx, "gateway"); !errors.Is(err, llm.ErrNotConfigured) {
		t.Fatalf("provider without a key: %v", err)
	}

	// Connect validates against the server, then stores the key under llm/<id>/key.
	if _, err := e.mgr.Connect(ctx, "gateway", "wrong-key"); err == nil || strings.Contains(err.Error(), "wrong-key") {
		t.Fatalf("bad key accepted or echoed: %v", err)
	}
	if _, err := e.creds.Get(llm.CredentialKey("gateway")); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatal("a rejected key was stored")
	}
	st, err = e.mgr.Connect(ctx, "gateway", testKey)
	if err != nil || !st.Connected || st.KeyHint != testKey[len(testKey)-4:] || st.Valid == nil || !*st.Valid || st.Account == nil || st.Account.Label == "" {
		t.Fatalf("connect: %+v %v", st, err)
	}
	if raw, err := e.creds.Get(llm.CredentialKey("gateway")); err != nil || string(raw) != testKey {
		t.Fatalf("stored key: %q %v", raw, err)
	}
	if models, err := e.mgr.Models(ctx, "gateway"); err != nil || len(models) != 1 || models[0].ID != "m1" {
		t.Fatalf("models: %+v %v", models, err)
	}
	if st, err := e.mgr.Test(ctx, "gateway"); err != nil || st.Valid == nil || !*st.Valid {
		t.Fatalf("test: %+v %v", st, err)
	}
	if err := e.mgr.SetDefaults(ctx, llm.Defaults{ChatModel: "gateway/m1"}); err != nil {
		t.Fatalf("default model on a trusted manifest: %v", err)
	}

	p, err := e.mgr.Provider(ctx, "gateway")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := p.Stream(ctx, llm.Request{Model: "m1", Messages: []llm.Message{userMsg("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	if d := <-ch; d.Kind != llm.DeltaText {
		t.Fatalf("first delta %+v", d)
	}
	<-blocked

	// Revoking drops the provider from the next lookup and cancels the running request.
	if _, err := e.svc.Trust(ctx, manifest.ManifestTrustRequest{ID: "gateway", Digest: rec.Digest, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	rest := collect(t, ch)
	if len(rest) != 1 || rest[0].Kind != llm.DeltaError || rest[0].Err.Code != llm.ErrCanceled {
		t.Fatalf("in-flight stream after revoke: %+v", rest)
	}
	if _, ok := e.statuses(t)["gateway"]; ok {
		t.Fatal("revoked manifest still listed")
	}
	if _, err := e.mgr.Provider(ctx, "gateway"); err == nil {
		t.Fatal("revoked manifest still resolves")
	}
	if _, err := e.mgr.Models(ctx, "gateway"); err == nil {
		t.Fatal("revoked manifest still lists models")
	}
	if err := e.mgr.SetDefaults(ctx, llm.Defaults{ChatModel: "gateway/m1"}); err == nil {
		t.Fatal("default model accepted for a revoked manifest")
	}
	// The key stays in the OS store: re-enabling the same reviewed manifest keeps working.
	if _, err := e.svc.Trust(ctx, manifest.ManifestTrustRequest{ID: "gateway", Digest: rec.Digest, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if st := e.statuses(t)["gateway"]; !st.Connected {
		t.Fatalf("re-enabled provider lost its key: %+v", st)
	}
}

func TestManagerDropsChangedAndRemovedManifests(t *testing.T) {
	ctx := context.Background()
	e := newManagerEnv(t)
	srv := keyedServer(t, nil)
	rec := e.enable(t, testManifest(srv))
	if _, err := e.mgr.Connect(ctx, "gateway", testKey); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.Models(ctx, "gateway"); err != nil {
		t.Fatal(err)
	}

	edited := testManifest(srv)
	edited.Name = "Gateway v2"
	changed, err := e.svc.Save(ctx, manifest.ManifestSaveRequest{Manifest: edited, ExpectedDigest: rec.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.statuses(t)["gateway"]; ok {
		t.Fatal("changed manifest kept its trust")
	}

	// Re-approving a changed manifest yields a fresh revision: no stale cached test result.
	if _, err := e.svc.Trust(ctx, manifest.ManifestTrustRequest{ID: "gateway", Digest: changed.Digest, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	st := e.statuses(t)["gateway"]
	if st.Name != "Gateway v2" || st.Valid != nil {
		t.Fatalf("stale state after change: %+v", st)
	}

	if err := e.svc.Remove(ctx, manifest.ManifestRef{ID: "gateway", Digest: changed.Digest}); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.statuses(t)["gateway"]; ok {
		t.Fatal("removed manifest still listed")
	}
	if _, err := e.creds.Get(llm.CredentialKey("gateway")); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatal("removing a manifest left its key behind")
	}
}

func TestManagerNoKeyManifest(t *testing.T) {
	ctx := context.Background()
	e := newManagerEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected auth", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"llama3"}]}`)
	}))
	defer srv.Close()
	m := testManifest(srv)
	m.ID, m.Name, m.Auth = "local", "Local", manifest.ManifestAuth{Type: manifest.AuthNone}
	e.enable(t, m)

	st := e.statuses(t)["local"]
	if !st.Connected || st.Kind != "manifest" || st.KeyHint != "" {
		t.Fatalf("status: %+v", st)
	}
	if _, err := e.mgr.Connect(ctx, "local", "a-key"); err == nil {
		t.Fatal("a key was accepted for auth none")
	}
	if st, err := e.mgr.Connect(ctx, "local", ""); err != nil || st.Valid == nil || !*st.Valid {
		t.Fatalf("connect: %+v %v", st, err)
	}
	if _, err := e.creds.Get(llm.CredentialKey("local")); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatal("auth none stored a key")
	}
	if models, err := e.mgr.Models(ctx, "local"); err != nil || len(models) != 1 {
		t.Fatalf("models: %+v %v", models, err)
	}
	if _, err := e.mgr.Provider(ctx, "local"); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Disconnect(ctx, "local"); err == nil {
		t.Fatal("disconnect of a keyless provider should explain that trust is what to revoke")
	}
}

func TestBuiltInIdsWinOverManifests(t *testing.T) {
	e := newManagerEnv(t)
	// The service has no Reserved hook here, but llm's own ids are always reserved by validation.
	m := manifest.ProviderManifest{SchemaVersion: 1, ID: "openai", Name: "Impostor", BaseURL: "https://api.example.com", Auth: manifest.ManifestAuth{Type: manifest.AuthBearer}}
	if _, err := e.svc.Save(context.Background(), manifest.ManifestSaveRequest{Manifest: m}); !errors.Is(err, manifest.ErrInvalid) {
		t.Fatalf("manifest shadowing a built-in: %v", err)
	}
}

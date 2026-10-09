package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/manifest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

// TestLLMManifestsOverIPC drives the real daemon: a saved manifest is inert until the exact
// digest is approved through the shared policy engine, then it is a provider; removing it
// takes it away again.
func TestLLMManifestsOverIPC(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	defer daemon.UseMemoryLLMCredentials()()
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := control.NewClient(dir, "llm-manifests-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"llama3"}]}`)
	}))
	defer srv.Close()

	call := func(method string, params, out any) error { return c.Call(ctx, method, params, out) }
	m := manifest.ProviderManifest{
		SchemaVersion: 1, ID: "local-llm", Name: "Local LLM", BaseURL: srv.URL + "/v1",
		Auth: manifest.ManifestAuth{Type: manifest.AuthNone},
	}

	// Invalid manifests and built-in ids are bad requests, nothing is stored.
	bad := m
	bad.ID = "openai"
	var ipcErr *ipc.Error
	if err := call("llm.manifests.save", manifest.ManifestSaveRequest{Manifest: bad}, nil); !errors.As(err, &ipcErr) || ipcErr.Code != ipc.CodeBadRequest {
		t.Fatalf("built-in id: %v", err)
	}
	var rec manifest.ManifestRecord
	if err := call("llm.manifests.save", manifest.ManifestSaveRequest{Manifest: m}, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Enabled || rec.ApprovedDigest != "" || !manifest.ValidDigest(rec.Digest) || rec.Manifest.ChatPath != "/chat/completions" {
		t.Fatalf("saved: %+v", rec)
	}
	var list []manifest.ManifestRecord
	if err := call("llm.manifests.list", struct{}{}, &list); err != nil || len(list) != 1 || list[0].Digest != rec.Digest {
		t.Fatalf("list: %+v %v", list, err)
	}
	providerIDs := func() map[string]llm.ProviderStatus {
		var sts []llm.ProviderStatus
		if err := call("llm.providers.list", struct{}{}, &sts); err != nil {
			t.Fatal(err)
		}
		out := map[string]llm.ProviderStatus{}
		for _, st := range sts {
			out[st.ID] = st
		}
		return out
	}
	if _, ok := providerIDs()["local-llm"]; ok {
		t.Fatal("unapproved manifest is a provider")
	}

	// A denied approval leaves it disabled; the prompt names the exact digest and host.
	trust := func(allow bool) error {
		done := make(chan error, 1)
		go func() {
			done <- call("llm.manifests.trust", manifest.ManifestTrustRequest{ID: m.ID, Digest: rec.Digest, Enabled: true}, nil)
		}()
		deadline := time.Now().Add(20 * time.Second)
		for {
			var pending []policy.Pending
			if err := call("policy.pending", struct{}{}, &pending); err != nil {
				t.Fatal(err)
			}
			for _, p := range pending {
				if p.Action != "llm.provider.trust" {
					continue
				}
				if p.Actor != "user" || p.Resource != "llm/local-llm/"+rec.Digest || p.Effect != policy.EffectDestructive ||
					!strings.Contains(p.Detail, rec.Digest) || !strings.Contains(p.Detail, strings.TrimPrefix(srv.URL, "http://")) {
					t.Fatalf("approval prompt: %+v", p)
				}
				if err := call("policy.resolve", map[string]any{"id": p.ID, "allow": allow}, nil); err != nil {
					t.Fatal(err)
				}
				return <-done
			}
			if time.Now().After(deadline) {
				t.Fatal("no approval was requested")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if err := trust(false); !errors.As(err, &ipcErr) || ipcErr.Code != "denied" {
		t.Fatalf("denied trust: %v", err)
	}
	if _, ok := providerIDs()["local-llm"]; ok {
		t.Fatal("denied manifest is a provider")
	}
	if err := trust(true); err != nil {
		t.Fatal(err)
	}
	st, ok := providerIDs()["local-llm"]
	if !ok || st.Kind != "manifest" || !st.Connected {
		t.Fatalf("approved manifest status: %+v ok=%v", st, ok)
	}
	var models []llm.Model
	if err := call("llm.models", llm.ProviderRef{ID: "local-llm"}, &models); err != nil || len(models) != 1 || models[0].ID != "llama3" {
		t.Fatalf("models: %+v %v", models, err)
	}

	// A stale digest cannot be removed; the exact one can, and the provider disappears.
	if err := call("llm.manifests.remove", manifest.ManifestRef{ID: m.ID, Digest: strings.Repeat("0", 64)}, nil); !errors.As(err, &ipcErr) || ipcErr.Code != "conflict" {
		t.Fatalf("stale remove: %v", err)
	}
	if err := call("llm.manifests.remove", manifest.ManifestRef{ID: m.ID, Digest: rec.Digest}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := providerIDs()["local-llm"]; ok {
		t.Fatal("removed manifest is still a provider")
	}
}

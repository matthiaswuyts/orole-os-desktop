package daemon

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/manifest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func manifestError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, manifest.ErrInvalid):
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	case errors.Is(err, manifest.ErrNotFound):
		return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	case errors.Is(err, manifest.ErrChanged):
		return &ipc.Error{Code: "conflict", Message: err.Error()}
	case errors.Is(err, policy.ErrDenied), errors.Is(err, policy.ErrApprovalTimeout):
		return &ipc.Error{Code: "denied", Message: err.Error()}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.Is(err, manifest.ErrClosed):
		return &ipc.Error{Code: "canceled", Message: err.Error()}
	default:
		return err
	}
}

// newLLMManifests builds the declarative provider store. A manifest id may not shadow a
// built-in provider, and a stored key is forgotten when its manifest is removed or moved to
// another origin so a key reviewed for one host is never sent to another.
func (d *Daemon) newLLMManifests(reg *llm.Registry, store credentials.Store) *manifest.Service {
	d.LLMManifests = manifest.New(manifest.Config{
		DB:     d.DB.DB,
		Policy: d.Policy,
		Reserved: func(id string) bool {
			_, ok := reg.Get(id)
			return ok
		},
		ForgetKey: func(id string) {
			if err := store.Delete(llm.CredentialKey(id)); err != nil {
				d.Log.Warn("could not remove a provider key from the credential store", "provider", id, "err", err)
			}
		},
	})
	return d.LLMManifests
}

// registerLLMManifests exposes the manifest store. Saving only stores configuration; enabling
// asks the shared policy engine (llm.provider.trust) about the exact digest.
func (d *Daemon) registerLLMManifests(changed func(id string)) {
	svc := d.LLMManifests
	handle(d, "llm.manifests.list", func(ctx context.Context, _ none) ([]manifest.ManifestRecord, error) {
		out, err := svc.List(ctx)
		return out, manifestError(err)
	})
	handle(d, "llm.manifests.save", func(ctx context.Context, req manifest.ManifestSaveRequest) (manifest.ManifestRecord, error) {
		out, err := svc.Save(ctx, req)
		if err == nil {
			d.audit("llm.manifest.save", "llm/"+out.Manifest.ID, "allowed", "Declarative provider manifest saved")
			changed(out.Manifest.ID)
		}
		return out, manifestError(err)
	})
	handle(d, "llm.manifests.trust", func(ctx context.Context, req manifest.ManifestTrustRequest) (manifest.ManifestRecord, error) {
		out, err := svc.Trust(ctx, req)
		if err == nil {
			changed(out.Manifest.ID)
		}
		return out, manifestError(err)
	})
	handle(d, "llm.manifests.remove", func(ctx context.Context, req manifest.ManifestRef) (none, error) {
		err := svc.Remove(ctx, req)
		if err == nil {
			d.audit("llm.manifest.remove", "llm/"+req.ID, "allowed", "Declarative provider removed")
			changed(req.ID)
		}
		return none{}, manifestError(err)
	})
}

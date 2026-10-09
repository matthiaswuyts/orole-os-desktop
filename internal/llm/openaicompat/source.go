package openaicompat

import (
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/manifest"
)

// Source adapts a manifest.Service to llm.DynamicSource: exactly the enabled manifests whose
// current digest was approved are providers. Revoking, changing or removing a manifest drops
// it from the next lookup and cancels requests of providers already built from it.
type Source struct {
	Service *manifest.Service
	// HTTP replaces the pinned client in tests; production leaves it nil.
	HTTP llm.HTTPDoer
}

var _ llm.DynamicSource = (*Source)(nil)

// Descriptors lists one "manifest" descriptor per trusted manifest, ordered by id.
func (s *Source) Descriptors() []llm.Descriptor {
	trusted := s.Service.Trusted()
	out := make([]llm.Descriptor, 0, len(trusted))
	for _, t := range trusted {
		out = append(out, llm.Descriptor{
			ID: t.Manifest.ID, Name: t.Manifest.Name, Kind: "manifest",
			NoKey: t.Manifest.Auth.Type == manifest.AuthNone, Revision: t.Digest,
			New: func(cfg llm.Config) (llm.Provider, error) {
				return New(Options{Manifest: t.Manifest, Key: cfg.APIKey, HTTP: s.HTTP, Life: t.Life})
			},
		})
	}
	return out
}

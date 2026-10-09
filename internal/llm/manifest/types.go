// Package manifest implements declarative OpenAI-compatible model providers: strict,
// size-bounded JSON documents that carry configuration, never code and never secrets.
//
// A manifest is enabled only after the user approved its exact canonical digest through
// the central policy engine (action llm.provider.trust). Saving a changed manifest clears
// the approval, imports land disabled, and the API key stays in the OS credential store
// under llm/<id>/key like the built-in providers. Auth type "none" has no key at all.
//
// IPC methods (daemon names, implemented in internal/daemon/svc_llm_manifests.go):
//
//	llm.manifests.list    struct{}                -> []ManifestRecord
//	llm.manifests.save    ManifestSaveRequest     -> ManifestRecord
//	llm.manifests.trust   ManifestTrustRequest    -> ManifestRecord
//	llm.manifests.remove  ManifestRef             -> struct{}
//
// Every successful change publishes llm.changed (llm.Change).
package manifest

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/policy"
)

// SchemaVersion is the only supported manifest schema.
const SchemaVersion = 1

// Limits enforced on every manifest.
const (
	MaxBytes  = 32 << 10
	maxDepth  = 8
	maxModels = 64
	maxHeader = 16
)

// Defaults filled in by Validate, so the canonical form (and digest) is stable.
const (
	DefaultModelsPath = "/models"
	DefaultChatPath   = "/chat/completions"
)

var (
	ErrInvalid  = errors.New("llm manifest: invalid manifest or request")
	ErrNotFound = errors.New("llm manifest: provider manifest not found")
	ErrChanged  = errors.New("llm manifest: manifest or trust changed; reload and review again")
	ErrClosed   = errors.New("llm manifest: manager is closed")
)

// AuthType says how the API key is presented to the provider.
type AuthType string

const (
	AuthBearer AuthType = "bearer" // Authorization: Bearer <key>
	AuthHeader AuthType = "header" // <header>: <key>
	AuthNone   AuthType = "none"   // no key is stored or sent
)

// ManifestAuth selects the key placement. Header is required for (and only for) AuthHeader.
type ManifestAuth struct {
	Type   AuthType `json:"type"`
	Header string   `json:"header,omitempty"`
}

// ManifestModel declares one model's capabilities. It is used when the models endpoint is
// unavailable and overrides the (capability-less) catalogue entry of the same id otherwise.
type ManifestModel struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Vision        bool   `json:"vision"`
	PDF           bool   `json:"pdf"`
	Tools         bool   `json:"tools"`
	Reasoning     bool   `json:"reasoning"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
}

// ProviderManifest is the schema v1 document describing an OpenAI-compatible provider.
type ProviderManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	BaseURL       string          `json:"baseUrl"`
	Auth          ManifestAuth    `json:"auth"`
	ModelsPath    string          `json:"modelsPath,omitempty"`
	ChatPath      string          `json:"chatPath,omitempty"`
	Models        []ManifestModel `json:"models,omitempty"`
	// Headers are static, non-secret request headers (never credentials).
	Headers map[string]string `json:"headers,omitempty"`
}

// ManifestRecord is a stored manifest with its trust state. Trusted means
// Enabled && ApprovedDigest == Digest (the store guarantees the latter whenever Enabled).
type ManifestRecord struct {
	Manifest       ProviderManifest `json:"manifest"`
	Digest         string           `json:"digest"`
	Enabled        bool             `json:"enabled"`
	ApprovedDigest string           `json:"approvedDigest"`
	UpdatedAt      int64            `json:"updatedAt"`
}

// ManifestSaveRequest creates (ExpectedDigest empty) or replaces (ExpectedDigest = current digest) a manifest.
type ManifestSaveRequest struct {
	Manifest       ProviderManifest `json:"manifest"`
	ExpectedDigest string           `json:"expectedDigest"`
}

// ManifestTrustRequest enables (after policy approval) or disables the exact manifest digest.
type ManifestTrustRequest struct {
	ID      string `json:"id"`
	Digest  string `json:"digest"`
	Enabled bool   `json:"enabled"`
}

// ManifestRef names one exact stored manifest.
type ManifestRef struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// Trusted is an enabled, approved manifest. Life is cancelled the moment the manifest is
// revoked, changed or removed (or the service closes): providers built from it must stop.
type Trusted struct {
	Manifest ProviderManifest
	Digest   string
	Life     context.Context
}

// Authorizer is implemented by the daemon's shared policy.Engine.
type Authorizer interface {
	Require(context.Context, policy.Request) error
}

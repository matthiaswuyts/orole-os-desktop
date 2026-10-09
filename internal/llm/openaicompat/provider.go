// Package openaicompat is the provider for declarative OpenAI-compatible manifests
// (internal/llm/manifest): Chat Completions streaming with function tools and usage,
// image/PDF input only where the manifest declares it, and model listing from the models
// endpoint with the manifest's static list as fallback. It never uses hosted tools.
//
// Network safety: the manifest's origin is pinned at review time. Requests to any other
// origin and redirects to another host are refused, so the key only ever reaches the
// reviewed host; loopback manifests may only reach loopback addresses and all others only
// public addresses.
package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/manifest"
)

// reasoningLevels are the effort levels sent as reasoning_effort for models declared with reasoning.
var reasoningLevels = []string{"low", "medium", "high"}

const maxModels = 1000

// Options builds a Provider.
type Options struct {
	Manifest manifest.ProviderManifest
	// Key is the API key; empty for auth "none".
	Key string
	// HTTP overrides the pinned client (tests). Requests are still pinned to the manifest origin.
	HTTP llm.HTTPDoer
	// Life ends when the manifest is revoked, changed or removed; in-flight requests are cancelled.
	// nil means the provider lives until its own contexts end.
	Life context.Context
}

// Provider talks to one reviewed OpenAI-compatible endpoint.
type Provider struct {
	m    manifest.ProviderManifest
	key  string
	http llm.HTTPDoer
	life context.Context
}

// New validates the manifest again (a stored row could have been tampered with) and builds
// the provider. It performs no network call.
func New(o Options) (*Provider, error) {
	m, _, _, err := manifest.Validate(o.Manifest, nil)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(o.Key)
	switch {
	case m.Auth.Type == manifest.AuthNone && key != "":
		return nil, errors.New("this provider does not use an API key")
	case m.Auth.Type != manifest.AuthNone && key == "":
		return nil, llm.ErrNotConfigured
	}
	if o.Life != nil && o.Life.Err() != nil {
		return nil, errors.New("this provider is no longer enabled")
	}
	doer := o.HTTP
	if doer == nil {
		doer = newClient(m)
	}
	return &Provider{m: m, key: key, http: pinned{doer: doer, origin: origin(schemeOf(m), m.Host())}, life: o.Life}, nil
}

// ID is the manifest id.
func (p *Provider) ID() string { return p.m.ID }

// bind returns ctx cancelled when the manifest's life ends. release must be called when done.
func (p *Provider) bind(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	if p.life == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(p.life, cancel)
	return ctx, func() { stop(); cancel() }
}

func (p *Provider) newRequest(ctx context.Context, method, url string, body []byte) (*http.Request, error) {
	var rd *bytes.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if rd != nil {
		req, err = http.NewRequestWithContext(ctx, method, url, rd)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return nil, llm.NetworkError(err, p.key)
	}
	for name, value := range p.m.Headers {
		req.Header.Set(name, value)
	}
	switch p.m.Auth.Type {
	case manifest.AuthBearer:
		req.Header.Set("Authorization", "Bearer "+p.key)
	case manifest.AuthHeader:
		req.Header.Set(p.m.Auth.Header, p.key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// Validate lists models: a cheap authenticated call that also proves the host is reachable.
func (p *Provider) Validate(ctx context.Context) (llm.Account, error) {
	if _, err := p.Models(ctx); err != nil {
		return llm.Account{}, err
	}
	return llm.Account{Label: p.m.Host()}, nil
}

type catalog struct {
	Data []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
}

// Models lists models from the models endpoint. Entries matching a declared model take the
// declared name and capabilities; others have no capabilities (they are never guessed). When
// the endpoint is unavailable (404/405/501 or not JSON) or empty, the declared list is used.
func (p *Provider) Models(ctx context.Context) ([]llm.Model, error) {
	ctx, release := p.bind(ctx)
	defer release()
	ctx, cancel := context.WithTimeout(ctx, llm.RequestTimeout)
	defer cancel()
	req, err := p.newRequest(ctx, http.MethodGet, p.m.ModelsURL(), nil)
	if err != nil {
		return nil, err
	}
	var out catalog
	if err := llm.DoJSON(p.http, req, &out, p.key); err != nil {
		if len(p.m.Models) > 0 && unavailable(err) && ctx.Err() == nil {
			return p.declared(), nil
		}
		return nil, p.lifeError(err)
	}
	models := make([]llm.Model, 0, min(len(out.Data), maxModels))
	seen := map[string]bool{}
	for _, e := range out.Data {
		id := strings.TrimSpace(e.ID)
		if id == "" || seen[id] || len(models) == maxModels {
			continue
		}
		seen[id] = true
		if i := slices.IndexFunc(p.m.Models, func(d manifest.ManifestModel) bool { return d.ID == id }); i >= 0 {
			models = append(models, toModel(p.m.Models[i]))
			continue
		}
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = id
		}
		models = append(models, llm.Model{ID: id, Name: name, Caps: llm.Caps{HostedTools: []string{}}, Reasoning: []string{}})
	}
	if len(models) == 0 && len(p.m.Models) > 0 {
		return p.declared(), nil
	}
	return models, nil
}

// lifeError reports a revoked manifest instead of a bare cancellation.
func (p *Provider) lifeError(err error) error {
	if p.life != nil && p.life.Err() != nil {
		return &llm.Error{Code: llm.ErrCanceled, Message: "the provider was disabled or changed"}
	}
	return err
}

// unavailable reports a models endpoint the server does not implement.
func unavailable(err error) bool {
	var le *llm.Error
	if !errors.As(err, &le) {
		return false
	}
	switch le.Status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	return le.Code == llm.ErrServer && le.Status/100 == 2 // 2xx that is not an OpenAI model list
}

func (p *Provider) declared() []llm.Model {
	out := make([]llm.Model, len(p.m.Models))
	for i, d := range p.m.Models {
		out[i] = toModel(d)
	}
	return out
}

func toModel(d manifest.ManifestModel) llm.Model {
	name := d.Name
	if name == "" {
		name = d.ID
	}
	m := llm.Model{
		ID: d.ID, Name: name, Reasoning: []string{},
		Caps: llm.Caps{Tools: d.Tools, Vision: d.Vision, PDF: d.PDF, Reasoning: d.Reasoning, HostedTools: []string{}, ContextWindow: d.ContextWindow},
	}
	if d.Reasoning {
		m.Reasoning = slices.Clone(reasoningLevels)
	}
	return m
}

// declaredModel returns the manifest's declaration for id.
func (p *Provider) declaredModel(id string) (manifest.ManifestModel, bool) {
	i := slices.IndexFunc(p.m.Models, func(d manifest.ManifestModel) bool { return d.ID == id })
	if i < 0 {
		return manifest.ManifestModel{}, false
	}
	return p.m.Models[i], true
}

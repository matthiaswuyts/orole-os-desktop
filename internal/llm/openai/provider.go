// Package openai is the direct OpenAI provider on the Responses API: streaming text,
// reasoning summaries, function calls, hosted web search with citations, image and PDF input.
package openai

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

const (
	// ID is the provider id used in "provider/model" selections.
	ID = llm.ProviderOpenAI
	// DefaultBaseURL is the API root.
	DefaultBaseURL = "https://api.openai.com/v1"
)

// Provider talks to OpenAI with one API key.
type Provider struct {
	key  string
	base string
	http llm.HTTPDoer
}

// New builds the provider. It performs no network call.
func New(cfg llm.Config) (llm.Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("openai: api key required")
	}
	p := &Provider{key: strings.TrimSpace(cfg.APIKey), base: strings.TrimRight(cfg.BaseURL, "/"), http: cfg.HTTP}
	if p.base == "" {
		p.base = DefaultBaseURL
	}
	if p.http == nil {
		p.http = llm.NewHTTPClient()
	}
	return p, nil
}

// Descriptor registers OpenAI with a Registry.
func Descriptor() llm.Descriptor {
	return llm.Descriptor{ID: ID, Name: "OpenAI", Kind: "builtin", New: New}
}

func (p *Provider) ID() string { return ID }

func (p *Provider) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, p.base+path, bytes.NewReader(body))
	} else {
		req, err = http.NewRequestWithContext(ctx, method, p.base+path, nil)
	}
	if err != nil {
		return nil, llm.NetworkError(err, p.key)
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

type modelInfo struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
}

type modelList struct {
	Data []modelInfo `json:"data"`
}

func (p *Provider) list(ctx context.Context) (modelList, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.RequestTimeout)
	defer cancel()
	var out modelList
	req, err := p.newRequest(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return out, err
	}
	return out, llm.DoJSON(p.http, req, &out, p.key)
}

// Validate lists models: a cheap authenticated call.
func (p *Provider) Validate(ctx context.Context) (llm.Account, error) {
	_, err := p.list(ctx)
	return llm.Account{}, err
}

// Models lists chat-capable models (newest first), filtered through the capability table.
func (p *Provider) Models(ctx context.Context) ([]llm.Model, error) {
	list, err := p.list(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(list.Data, func(a, b modelInfo) int { return cmp.Compare(b.Created, a.Created) })
	models := make([]llm.Model, 0, len(list.Data))
	for _, d := range list.Data {
		if m, ok := modelFor(d.ID); ok {
			models = append(models, m)
		}
	}
	return models, nil
}

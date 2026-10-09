// Package openrouter is the direct OpenRouter provider: Chat Completions streaming with
// server tools (web search, web fetch, datetime), PDF/image input and the model catalogue.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

const (
	// ID is the provider id used in "provider/model" selections.
	ID = llm.ProviderOpenRouter
	// DefaultBaseURL is the API root.
	DefaultBaseURL = "https://openrouter.ai/api/v1"
	// AuthURL is the OAuth PKCE authorization page.
	AuthURL = "https://openrouter.ai/auth"

	// Attribution shown in OpenRouter's app rankings and the key's activity.
	appReferer = "https://github.com/rayorole/orole-os-desktop"
	appTitle   = "Orole OS"
	keyLabel   = "Orole OS"
)

// Provider talks to OpenRouter with one API key.
type Provider struct {
	key  string
	base string
	http llm.HTTPDoer
}

// New builds the provider. It performs no network call.
func New(cfg llm.Config) (llm.Provider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("openrouter: api key required")
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

// Descriptor registers OpenRouter (including its headless OAuth sign-in) with a Registry.
func Descriptor() llm.Descriptor {
	return llm.Descriptor{
		ID: ID, Name: "OpenRouter", Kind: "builtin", New: New,
		OAuth: &llm.OAuth{
			AuthURL: BuildAuthURL,
			Exchange: func(ctx context.Context, doer llm.HTTPDoer, code, verifier string) (string, error) {
				return Exchange(ctx, doer, DefaultBaseURL, code, verifier)
			},
		},
	}
}

func (p *Provider) ID() string { return ID }

func (p *Provider) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var rd *bytes.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if rd != nil {
		req, err = http.NewRequestWithContext(ctx, method, p.base+path, rd)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, p.base+path, nil)
	}
	if err != nil {
		return nil, llm.NetworkError(err, p.key)
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("HTTP-Referer", appReferer)
	req.Header.Set("X-Title", appTitle)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// Validate calls GET /key: the cheapest authenticated endpoint; it also reports credits.
func (p *Provider) Validate(ctx context.Context) (llm.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.RequestTimeout)
	defer cancel()
	req, err := p.newRequest(ctx, http.MethodGet, "/key", nil)
	if err != nil {
		return llm.Account{}, err
	}
	var out struct {
		Data struct {
			Label          string   `json:"label"`
			Limit          *float64 `json:"limit"`
			LimitRemaining *float64 `json:"limit_remaining"`
		} `json:"data"`
	}
	if err := llm.DoJSON(p.http, req, &out, p.key); err != nil {
		return llm.Account{}, err
	}
	return llm.Account{Label: out.Data.Label, Limit: out.Data.Limit, Remaining: out.Data.LimitRemaining}, nil
}

// effortOrder ranks reasoning levels for stable low-to-high presentation.
var effortOrder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// Models lists text-output models from GET /models.
func (p *Provider) Models(ctx context.Context) ([]llm.Model, error) {
	ctx, cancel := context.WithTimeout(ctx, llm.RequestTimeout)
	defer cancel()
	req, err := p.newRequest(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []catalogEntry `json:"data"`
	}
	if err := llm.DoJSON(p.http, req, &out, p.key); err != nil {
		return nil, err
	}
	models := make([]llm.Model, 0, len(out.Data))
	for i := range out.Data {
		if m, ok := out.Data[i].model(); ok {
			models = append(models, m)
		}
	}
	return models, nil
}

type catalogEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int64  `json:"context_length"`
	Architecture  struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
	SupportedParameters []string `json:"supported_parameters"`
	Reasoning           *struct {
		SupportedEfforts []string `json:"supported_efforts"`
	} `json:"reasoning"`
}

// model maps a catalogue entry. Server tools are run by OpenRouter for every model, and
// PDFs are parsed by OpenRouter (file-parser plugin) when the model has no native file input.
func (e *catalogEntry) model() (llm.Model, bool) {
	if e.ID == "" || !slices.Contains(e.Architecture.Output, "text") {
		return llm.Model{}, false
	}
	name := e.Name
	if name == "" {
		name = e.ID
	}
	levels := reasoningLevels(e)
	return llm.Model{
		ID:   e.ID,
		Name: name,
		Caps: llm.Caps{
			Tools:         slices.Contains(e.SupportedParameters, "tools"),
			Vision:        slices.Contains(e.Architecture.Input, "image"),
			PDF:           true,
			Reasoning:     len(levels) > 0,
			HostedTools:   []string{llm.HostedWebSearch, llm.HostedWebFetch, llm.HostedDatetime},
			ContextWindow: e.ContextLength,
		},
		Reasoning: levels,
	}, true
}

func reasoningLevels(e *catalogEntry) []string {
	var levels []string
	if e.Reasoning != nil && len(e.Reasoning.SupportedEfforts) > 0 {
		levels = slices.Clone(e.Reasoning.SupportedEfforts)
	} else if slices.Contains(e.SupportedParameters, "reasoning") || slices.Contains(e.SupportedParameters, "reasoning_effort") {
		levels = []string{"low", "medium", "high"}
	}
	rank := func(s string) int {
		if i := slices.Index(effortOrder, s); i >= 0 {
			return i
		}
		return len(effortOrder)
	}
	slices.SortStableFunc(levels, func(a, b string) int { return rank(a) - rank(b) })
	return slices.Compact(levels)
}

// BuildAuthURL is the headless PKCE sign-in page (no callback URL: the page shows a code).
func BuildAuthURL(challenge string) string {
	esc := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	return AuthURL + "?code_challenge=" + esc(challenge) + "&code_challenge_method=S256&key_label=" + esc(keyLabel)
}

// Exchange trades the pasted code for an API key (POST /auth/keys).
func Exchange(ctx context.Context, doer llm.HTTPDoer, baseURL, code, verifier string) (string, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	ctx, cancel := context.WithTimeout(ctx, llm.RequestTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]string{
		"code": code, "code_verifier": verifier, "code_challenge_method": "S256",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/auth/keys", bytes.NewReader(body))
	if err != nil {
		return "", llm.NetworkError(err, code, verifier)
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Key string `json:"key"`
	}
	if err := llm.DoJSON(doer, req, &out, code, verifier); err != nil {
		return "", err
	}
	if out.Key == "" {
		return "", &llm.Error{Code: llm.ErrServer, Message: "OpenRouter did not return a key"}
	}
	return out.Key, nil
}

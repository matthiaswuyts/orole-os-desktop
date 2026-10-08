package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBase    = "https://api.github.com"
	apiVersion     = "2022-11-28"
	maxBodyBytes   = 32 << 20
	maxCachedBytes = 4 << 20
	maxCacheItems  = 512
)

// Authorize is the hook consulted before any permission-sensitive write. It
// returns nil to allow. The policy engine plugs in here; until then the daemon
// passes [RequireConfirmation].
type Authorize func(ctx context.Context, action, detail string) error

// Errors returned by the client.
var (
	// ErrNoAuthorizer is returned for writes when the client has no Authorize hook.
	ErrNoAuthorizer = errors.New("github: writes are disabled: no authorization hook configured")
	// ErrUnauthorized means GitHub rejected the token (401).
	ErrUnauthorized = errors.New("github: token rejected (401); sign in again")
)

// APIError is a non-success GitHub response.
type APIError struct {
	Status  int
	Message string
	DocURL  string
}

func (e *APIError) Error() string {
	m := fmt.Sprintf("github: %d %s", e.Status, http.StatusText(e.Status))
	if e.Message != "" {
		m += ": " + e.Message
	}
	return m
}

// RateLimitError means the primary or secondary rate limit blocks the request.
type RateLimitError struct {
	Secondary bool
	RetryAt   time.Time
}

func (e *RateLimitError) Error() string {
	kind := "primary"
	if e.Secondary {
		kind = "secondary"
	}
	return fmt.Sprintf("github: %s rate limit reached; retry after %s", kind, e.RetryAt.Format(time.RFC3339))
}

// RateLimit is the last observed limit for a resource bucket.
type RateLimit struct {
	Resource  string    `json:"resource"`
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"resetAt"`
}

// Client talks to the GitHub REST API. Safe for concurrent use.
type Client struct {
	BaseURL   string
	HTTP      *http.Client
	Tokens    TokenSource
	Authorize Authorize
	Log       *slog.Logger
	UserAgent string
	// MaxRetries bounds rate-limit/5xx retries per request (default 3).
	MaxRetries int
	// MaxBackoff is the longest the client will sleep for a rate limit before
	// returning a RateLimitError instead (default 2 minutes).
	MaxBackoff time.Duration
	// Sleep waits d or until ctx ends (replaced in tests).
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock (replaced in tests).
	Now func() time.Time

	mu    sync.Mutex
	cache map[string]*cacheEntry
	rate  map[string]RateLimit
	scope string // X-OAuth-Scopes of last response
	tick  uint64
}

type cacheEntry struct {
	etag   string
	body   []byte
	header http.Header
	used   uint64
}

// NewClient returns a client for api.github.com using tokens.
func NewClient(tokens TokenSource) *Client {
	return &Client{BaseURL: defaultBase, Tokens: tokens, UserAgent: "orole-os"}
}

func (c *Client) base() string {
	if c.BaseURL == "" {
		return defaultBase
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) httpc() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 45 * time.Second}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) maxRetries() int {
	if c.MaxRetries > 0 {
		return c.MaxRetries
	}
	return 3
}

func (c *Client) maxBackoff() time.Duration {
	if c.MaxBackoff > 0 {
		return c.MaxBackoff
	}
	return 2 * time.Minute
}

// Reset drops the response cache and rate info (call when the identity changes).
func (c *Client) Reset() {
	c.mu.Lock()
	c.cache, c.rate, c.scope = nil, nil, ""
	c.mu.Unlock()
	if c.Tokens != nil {
		c.Tokens.Invalidate()
	}
}

// RateLimits returns the last observed limits per resource bucket.
func (c *Client) RateLimits() []RateLimit {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]RateLimit, 0, len(c.rate))
	for _, r := range c.rate {
		out = append(out, r)
	}
	return out
}

// Scopes returns the OAuth scopes reported by the last response (classic tokens).
func (c *Client) Scopes() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.scope
}

func (c *Client) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.New(slog.DiscardHandler)
}

// resp is a fully-read response.
type resp struct {
	status int
	header http.Header
	body   []byte
	cached bool
}

func resourceFor(path string) string {
	if strings.HasPrefix(path, "/search") {
		return "search"
	}
	return "core"
}

func (c *Client) waitForKnownLimit(ctx context.Context, path string) error {
	c.mu.Lock()
	r, ok := c.rate[resourceFor(path)]
	c.mu.Unlock()
	if !ok || r.Remaining > 0 || !r.ResetAt.After(c.now()) {
		return nil
	}
	wait := r.ResetAt.Sub(c.now()) + time.Second
	if wait > c.maxBackoff() {
		return &RateLimitError{RetryAt: r.ResetAt}
	}
	return c.sleep(ctx, wait)
}

// absURL resolves path (or an absolute next-page URL) against the base.
func (c *Client) absURL(path string, q url.Values) (string, error) {
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		base, _ := url.Parse(c.base())
		if u.Host != base.Host || u.Scheme != base.Scheme || u.User != nil {
			return "", errors.New("github: refusing to follow link to foreign host or origin")
		}
		return u.String(), nil
	}
	u := c.base() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u, nil
}

// request performs one logical request with auth, ETag, rate-limit and retry handling.
func (c *Client) request(ctx context.Context, method, path string, q url.Values, payload any) (*resp, error) {
	full, err := c.absURL(path, q)
	if err != nil {
		return nil, err
	}
	var body []byte
	if payload != nil {
		if body, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}
	resPath := path
	if u, err := url.Parse(full); err == nil {
		resPath = u.Path
	}
	if err := c.waitForKnownLimit(ctx, resPath); err != nil {
		return nil, err
	}
	retried401 := false
	for attempt := 0; ; attempt++ {
		tok, err := c.Tokens.Token(ctx)
		if err != nil {
			return nil, err
		}
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, full, rdr)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", apiVersion)
		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Authorization", "Bearer "+tok)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		var ce *cacheEntry
		if method == http.MethodGet {
			c.mu.Lock()
			if ce = c.cache[full]; ce != nil {
				req.Header.Set("If-None-Match", ce.etag)
			}
			c.mu.Unlock()
		}
		hr, err := c.httpc().Do(req)
		if err != nil {
			// net/http errors embed the URL, never headers, but scrub anyway.
			return nil, errors.New(Redact(err.Error(), tok))
		}
		data, rerr := io.ReadAll(io.LimitReader(hr.Body, maxBodyBytes+1))
		hr.Body.Close()
		if rerr != nil {
			return nil, errors.New(Redact(rerr.Error(), tok))
		}
		if len(data) > maxBodyBytes {
			return nil, errors.New("github: response too large")
		}
		c.noteRate(hr.Header)
		r := &resp{status: hr.StatusCode, header: hr.Header, body: data}

		switch {
		case hr.StatusCode == http.StatusNotModified && ce != nil:
			c.mu.Lock()
			c.tick++
			ce.used = c.tick
			c.mu.Unlock()
			return &resp{status: 200, header: ce.header, body: ce.body, cached: true}, nil
		case hr.StatusCode >= 200 && hr.StatusCode < 300:
			if method == http.MethodGet {
				c.store(full, hr, data)
			}
			return r, nil
		case hr.StatusCode == http.StatusUnauthorized:
			c.Tokens.Invalidate()
			if !retried401 {
				retried401 = true
				continue
			}
			return nil, ErrUnauthorized
		}

		apiErr := parseAPIError(hr.StatusCode, data, tok)
		if wait, secondary, limited := c.rateWait(hr, apiErr, attempt); limited {
			retryAt := c.now().Add(wait)
			if attempt >= c.maxRetries() || wait > c.maxBackoff() {
				return nil, &RateLimitError{Secondary: secondary, RetryAt: retryAt}
			}
			c.log().Warn("github rate limited; backing off", "wait", wait.String(), "secondary", secondary)
			if err := c.sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		}
		if method == http.MethodGet && hr.StatusCode >= 500 && attempt < c.maxRetries() {
			if err := c.sleep(ctx, time.Duration(1<<attempt)*500*time.Millisecond); err != nil {
				return nil, err
			}
			continue
		}
		return nil, apiErr
	}
}

func parseAPIError(status int, data []byte, tok string) *APIError {
	var e struct {
		Message string `json:"message"`
		Doc     string `json:"documentation_url"`
	}
	_ = json.Unmarshal(data, &e)
	return &APIError{Status: status, Message: Redact(e.Message, tok), DocURL: e.Doc}
}

// rateWait decides whether a 403/429 is a rate limit and how long to wait.
func (c *Client) rateWait(hr *http.Response, e *APIError, attempt int) (wait time.Duration, secondary, limited bool) {
	if hr.StatusCode != http.StatusForbidden && hr.StatusCode != http.StatusTooManyRequests {
		return
	}
	if ra := hr.Header.Get("Retry-After"); ra != "" {
		if s, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && s >= 0 {
			return time.Duration(s) * time.Second, true, true
		}
		if t, err := http.ParseTime(ra); err == nil {
			return max(0, t.Sub(c.now())), true, true
		}
	}
	if hr.Header.Get("X-RateLimit-Remaining") == "0" {
		if s, err := strconv.ParseInt(hr.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return max(time.Second, time.Unix(s, 0).Sub(c.now())+time.Second), false, true
		}
	}
	if strings.Contains(strings.ToLower(e.Message), "secondary rate limit") || hr.StatusCode == http.StatusTooManyRequests {
		return time.Duration(60<<attempt) * time.Second, true, true
	}
	return
}

func (c *Client) noteRate(h http.Header) {
	if s := h.Get("X-OAuth-Scopes"); s != "" {
		c.mu.Lock()
		c.scope = s
		c.mu.Unlock()
	}
	rem, err1 := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	lim, err2 := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	rst, err3 := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return
	}
	res := h.Get("X-RateLimit-Resource")
	if res == "" {
		res = "core"
	}
	c.mu.Lock()
	if c.rate == nil {
		c.rate = map[string]RateLimit{}
	}
	c.rate[res] = RateLimit{Resource: res, Limit: lim, Remaining: rem, ResetAt: time.Unix(rst, 0)}
	c.mu.Unlock()
}

func (c *Client) store(key string, hr *http.Response, data []byte) {
	etag := hr.Header.Get("ETag")
	if etag == "" || len(data) > maxCachedBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]*cacheEntry{}
	}
	if len(c.cache) >= maxCacheItems {
		var oldK string
		var oldU uint64 = ^uint64(0)
		for k, e := range c.cache {
			if e.used < oldU {
				oldK, oldU = k, e.used
			}
		}
		delete(c.cache, oldK)
	}
	c.tick++
	c.cache[key] = &cacheEntry{etag: etag, body: data, header: hr.Header.Clone(), used: c.tick}
}

// getJSON GETs path and decodes into out.
func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	r, err := c.request(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(r.body, out)
}

// write sends a mutating request after consulting the Authorize hook.
func (c *Client) write(ctx context.Context, action, detail, method, path string, payload, out any) error {
	if c.Authorize == nil {
		return ErrNoAuthorizer
	}
	if err := c.Authorize(ctx, action, detail); err != nil {
		return err
	}
	r, err := c.request(ctx, method, path, nil, payload)
	if err != nil {
		return err
	}
	if out != nil && len(r.body) > 0 {
		return json.Unmarshal(r.body, out)
	}
	return nil
}

// nextLink extracts rel="next" from a Link header.
func nextLink(h http.Header) string {
	for _, part := range strings.Split(h.Get("Link"), ",") {
		part = strings.TrimSpace(part)
		i := strings.Index(part, ">")
		if !strings.HasPrefix(part, "<") || i < 0 {
			continue
		}
		if strings.Contains(part[i:], `rel="next"`) {
			return part[1:i]
		}
	}
	return ""
}

// getAll follows Link: rel=next collecting up to limit items (limit<=0: 100).
// unwrap decodes one page body into items.
func getAll[T any](ctx context.Context, c *Client, path string, q url.Values, limit int, unwrap func([]byte) ([]T, error)) ([]T, error) {
	if limit <= 0 {
		limit = 100
	}
	if q == nil {
		q = url.Values{}
	}
	if q.Get("per_page") == "" {
		q.Set("per_page", strconv.Itoa(min(limit, 100)))
	}
	all := make([]T, 0)
	next, nq := path, q
	for pages := 0; next != "" && len(all) < limit && pages < 50; pages++ {
		r, err := c.request(ctx, http.MethodGet, next, nq, nil)
		if err != nil {
			return nil, err
		}
		items, err := unwrap(r.body)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		next, nq = nextLink(r.header), nil
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func list[T any](ctx context.Context, c *Client, path string, q url.Values, limit int) ([]T, error) {
	return getAll(ctx, c, path, q, limit, func(b []byte) ([]T, error) {
		var v []T
		return v, json.Unmarshal(b, &v)
	})
}

// wrapped decodes {"<key>": [...]} page bodies.
func wrapped[T any](key string) func([]byte) ([]T, error) {
	return func(b []byte) ([]T, error) {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		var v []T
		raw, ok := m[key]
		if !ok {
			return v, nil
		}
		return v, json.Unmarshal(raw, &v)
	}
}

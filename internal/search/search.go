// Package search is the daemon-side unified search aggregator. It fans a query
// out to the local providers the daemon itself owns (files by name, the
// notification center, the settings index) and merges the answers. Providers
// owned by other modules (mail, GitHub, projects) are searched by the frontend
// through lib/commands registerProvider, not here.
package search

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// Result is one hit. Action tells the UI how to open it; the UI never builds
// paths or ids on its own.
type Result struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	// Kind is a UI hint: file, folder, notification, setting.
	Kind  string  `json:"kind"`
	Score float64 `json:"score"`
	// Action: {view:"files", provider:"local", path:"..."} etc.
	Action map[string]string `json:"action"`
}

// Searcher answers a query for one provider. It must honour ctx.
type Searcher func(ctx context.Context, query string, limit int) ([]Result, error)

// Response is the merged answer. Errors maps provider id to a message for the
// providers that failed or timed out; their absence from Results is then not
// "no matches".
type Response struct {
	Results []Result          `json:"results"`
	Errors  map[string]string `json:"errors,omitempty"`
}

// Aggregator holds the registered providers.
type Aggregator struct {
	// Timeout bounds each provider (default 2.5s).
	Timeout time.Duration
	mu      sync.RWMutex
	by      map[string]Searcher
}

// New returns an empty aggregator.
func New() *Aggregator {
	return &Aggregator{Timeout: 2500 * time.Millisecond, by: map[string]Searcher{}}
}

// Register adds a provider.
func (a *Aggregator) Register(id string, s Searcher) {
	a.mu.Lock()
	a.by[id] = s
	a.mu.Unlock()
}

// IDs lists provider ids, sorted.
func (a *Aggregator) IDs() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, 0, len(a.by))
	for id := range a.by {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Query searches the named providers (all when providers is empty; unknown ids
// are reported in Errors) concurrently and returns at most limit results,
// best first.
func (a *Aggregator) Query(ctx context.Context, q string, providers []string, limit int) Response {
	q = strings.TrimSpace(q)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	resp := Response{Results: []Result{}}
	if q == "" {
		return resp
	}
	if len(providers) == 0 {
		providers = a.IDs()
	}
	type out struct {
		id  string
		res []Result
		err error
	}
	ch := make(chan out, len(providers))
	n := 0
	for _, id := range providers {
		a.mu.RLock()
		s, ok := a.by[id]
		a.mu.RUnlock()
		if !ok {
			if resp.Errors == nil {
				resp.Errors = map[string]string{}
			}
			resp.Errors[id] = "unknown search provider"
			continue
		}
		n++
		go func() {
			pctx, cancel := context.WithTimeout(ctx, a.Timeout)
			defer cancel()
			res, err := s(pctx, q, limit)
			if err == nil && pctx.Err() != nil && len(res) == 0 {
				err = pctx.Err()
			}
			ch <- out{id, res, err}
		}()
	}
	for range n {
		o := <-ch
		if o.err != nil {
			if resp.Errors == nil {
				resp.Errors = map[string]string{}
			}
			resp.Errors[o.id] = o.err.Error()
		}
		for _, r := range o.res {
			r.Provider = o.id
			resp.Results = append(resp.Results, r)
		}
	}
	sort.SliceStable(resp.Results, func(i, j int) bool {
		if resp.Results[i].Score != resp.Results[j].Score {
			return resp.Results[i].Score > resp.Results[j].Score
		}
		if resp.Results[i].Provider != resp.Results[j].Provider {
			return resp.Results[i].Provider < resp.Results[j].Provider
		}
		return resp.Results[i].Title < resp.Results[j].Title
	})
	if len(resp.Results) > limit {
		resp.Results = resp.Results[:limit]
	}
	return resp
}

// Fuzzy scores text against a query: 0 = no match; exact > prefix > word start
// > substring > subsequence. It mirrors the palette's fuzzyScore ordering.
func Fuzzy(query, text string) float64 {
	q, t := strings.ToLower(query), strings.ToLower(text)
	if q == "" {
		return 1
	}
	if t == q {
		return 120
	}
	switch i := strings.Index(t, q); {
	case i == 0:
		return 100 + float64(len(q))
	case i > 0:
		if t[i-1] == ' ' || t[i-1] == '-' || t[i-1] == '_' || t[i-1] == '/' || t[i-1] == '\\' {
			return 80 + float64(len(q))
		}
		return 60 + float64(len(q)) - float64(i)*0.1
	}
	qi, score, prev := 0, 0.0, -2
	for i := 0; i < len(t) && qi < len(q); i++ {
		if t[i] == q[qi] {
			if prev == i-1 {
				score += 3
			} else {
				score++
			}
			prev = i
			qi++
		}
	}
	if qi == len(q) {
		return score
	}
	return 0
}

package llm

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// OAuth describes a provider's headless PKCE sign-in: the user opens AuthURL, the page
// shows a code, the user pastes it and the daemon exchanges it for an API key.
type OAuth struct {
	// AuthURL builds the browser URL for an S256 code challenge.
	AuthURL func(challenge string) string
	// Exchange trades the pasted code and the verifier for an API key.
	Exchange func(ctx context.Context, doer HTTPDoer, code, verifier string) (key string, err error)
}

// Descriptor registers one provider kind.
type Descriptor struct {
	ID   string
	Name string
	// Kind is "builtin" or "manifest".
	Kind string
	// New builds a Provider for a key. It must not perform network calls.
	New func(Config) (Provider, error)
	// OAuth is optional.
	OAuth *OAuth
}

// Registry is an ordered set of provider descriptors. Safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	items []Descriptor
}

// NewRegistry returns a registry holding ds.
func NewRegistry(ds ...Descriptor) (*Registry, error) {
	r := &Registry{}
	for _, d := range ds {
		if err := r.Register(d); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds d. IDs are unique.
func (r *Registry) Register(d Descriptor) error {
	if d.ID == "" || d.New == nil {
		return fmt.Errorf("llm: provider descriptor needs an id and a constructor")
	}
	if d.Name == "" {
		d.Name = d.ID
	}
	if d.Kind == "" {
		d.Kind = "builtin"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if slices.ContainsFunc(r.items, func(x Descriptor) bool { return x.ID == d.ID }) {
		return fmt.Errorf("llm: provider %q already registered", d.ID)
	}
	r.items = append(r.items, d)
	return nil
}

// Get returns the descriptor for id.
func (r *Registry) Get(id string) (Descriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i := slices.IndexFunc(r.items, func(x Descriptor) bool { return x.ID == id })
	if i < 0 {
		return Descriptor{}, false
	}
	return r.items[i], true
}

// List returns descriptors in registration order.
func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.items)
}

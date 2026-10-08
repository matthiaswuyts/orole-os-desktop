package credentials

import (
	"sort"
	"sync"
)

// Memory is an in-process Store for tests. It is not durable and must not be used in production paths.
type Memory struct {
	mu sync.Mutex
	m  map[string][]byte
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{m: map[string][]byte{}} }

func (s *Memory) Set(key string, secret []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), secret...)
	return nil
}

func (s *Memory) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

func (s *Memory) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// Keys lists stored keys (sorted).
func (s *Memory) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

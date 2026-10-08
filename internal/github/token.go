package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
)

// ErrNoToken is returned when a source has no credential available.
var ErrNoToken = errors.New("github: no token available")

// CredentialKey is the credentials.Store key holding a pasted personal access token.
const CredentialKey = "github/token"

// TokenSource yields the bearer token used for API calls. Implementations keep
// tokens in memory only and never log them.
type TokenSource interface {
	// Name identifies the source ("gh", "pat", ...). It is safe to show in UI.
	Name() string
	// Token returns the current token or ErrNoToken.
	Token(ctx context.Context) (string, error)
	// Invalidate drops any cached token so the next Token call re-reads it
	// (called by the client after a 401).
	Invalidate()
}

// GHCLISource reads the token from the GitHub CLI (`gh auth token`). The token
// is held in memory only for the life of the source.
type GHCLISource struct {
	// Exe is the gh binary ("" = "gh" from PATH).
	Exe string
	// Host is the GitHub host ("" = github.com).
	Host string
	// run executes the command; replaced in tests.
	run func(ctx context.Context, exe string, args ...string) ([]byte, error)

	mu  sync.Mutex
	tok string
}

// Name implements TokenSource.
func (*GHCLISource) Name() string { return "gh" }

// Token implements TokenSource.
func (s *GHCLISource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok != "" {
		return s.tok, nil
	}
	exe := s.Exe
	if exe == "" {
		exe = "gh"
	}
	host := s.Host
	if host == "" {
		host = "github.com"
	}
	run := s.run
	if run == nil {
		run = runCmd
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := run(cctx, exe, "auth", "token", "--hostname", host)
	if err != nil {
		// Never include command output: on odd failures it could echo secrets.
		return "", fmt.Errorf("%w: gh auth token failed (is the GitHub CLI installed and logged in?)", ErrNoToken)
	}
	t := strings.TrimSpace(string(out))
	if t == "" {
		return "", fmt.Errorf("%w: gh returned no token", ErrNoToken)
	}
	s.tok = t
	return t, nil
}

// Invalidate implements TokenSource.
func (s *GHCLISource) Invalidate() {
	s.mu.Lock()
	s.tok = ""
	s.mu.Unlock()
}

func runCmd(ctx context.Context, exe string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	hideWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out // stderr deliberately discarded
	err := cmd.Run()
	return out.Bytes(), err
}

// StoreSource reads a pasted personal access token from the OS credential store.
type StoreSource struct {
	Store credentials.Store
	Key   string // "" = CredentialKey
}

// Name implements TokenSource.
func (StoreSource) Name() string { return "pat" }

func (s StoreSource) key() string {
	if s.Key == "" {
		return CredentialKey
	}
	return s.Key
}

// Token implements TokenSource.
func (s StoreSource) Token(context.Context) (string, error) {
	b, err := s.Store.Get(s.key())
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return "", ErrNoToken
		}
		return "", errors.New("github: could not read credential store")
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", ErrNoToken
	}
	return t, nil
}

// Invalidate implements TokenSource (the store is read on every call).
func (StoreSource) Invalidate() {}

// Save validates nothing; callers validate first. It stores the token.
func (s StoreSource) Save(token string) error {
	return s.Store.Set(s.key(), []byte(strings.TrimSpace(token)))
}

// Clear removes the stored token.
func (s StoreSource) Clear() error {
	err := s.Store.Delete(s.key())
	if errors.Is(err, credentials.ErrNotFound) {
		return nil
	}
	return err
}

// StaticSource holds a fixed in-memory token (used to validate a token before
// it is stored, and in tests).
type StaticSource struct{ T string }

// String and GoString prevent accidental formatting from exposing the token.
func (StaticSource) String() string   { return "github.StaticSource([redacted])" }
func (StaticSource) GoString() string { return "github.StaticSource([redacted])" }

// Name implements TokenSource.
func (StaticSource) Name() string { return "static" }

// Token implements TokenSource.
func (s StaticSource) Token(context.Context) (string, error) {
	if s.T == "" {
		return "", ErrNoToken
	}
	return s.T, nil
}

// Invalidate implements TokenSource.
func (StaticSource) Invalidate() {}

// Redact removes every occurrence of the given secrets from s.
func Redact(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "[redacted]")
		}
	}
	return s
}

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
)

// Setting keys for Defaults (values are JSON strings in the settings KV).
const (
	SettingChatModel   = "llm.chatModel"
	SettingMemoryModel = "llm.memoryModel"
)

const (
	modelsTTL   = 5 * time.Minute
	authTTL     = 10 * time.Minute
	keyHintLen  = 4
	minHintable = 8 // shorter secrets reveal too much of the key to show a hint
)

// CredentialKey is the credentials.Store key holding a provider's API key.
func CredentialKey(providerID string) string { return "llm/" + providerID + "/key" }

// Settings is the daemon's key/value store for non-secret preferences.
type Settings interface {
	// Get returns the raw stored value; ok is false when the key is unset.
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string) error
}

// ManagerConfig wires a Manager.
type ManagerConfig struct {
	Registry    *Registry
	Credentials credentials.Store
	Settings    Settings
	// HTTP is shared by every built-in provider and the OAuth exchange; nil = NewHTTPClient().
	HTTP HTTPDoer
	// Dynamic is optional: trusted manifest providers, resolved afresh on every lookup.
	Dynamic DynamicSource
	// Now is injectable for tests; nil = time.Now.
	Now func() time.Time
}

type testResult struct {
	rev     string
	valid   bool
	err     string
	account *Account
}

type modelsEntry struct {
	mu     sync.Mutex // held while fetching so concurrent callers share one request
	rev    string
	at     time.Time
	models []Model
}

type pendingAuth struct {
	provider string
	verifier string
	expires  time.Time
}

// Manager implements Service. It never keeps API keys: they are read from the
// credential store for every Provider call.
type Manager struct {
	reg      *Registry
	dyn      DynamicSource
	creds    credentials.Store
	settings Settings
	http     HTTPDoer
	now      func() time.Time

	mu      sync.Mutex
	tests   map[string]testResult
	models  map[string]*modelsEntry
	pending *pendingAuth
}

var _ Service = (*Manager)(nil)

// NewManager builds a Manager.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.Registry == nil || cfg.Credentials == nil || cfg.Settings == nil {
		return nil, errors.New("llm: manager needs a registry, a credential store and settings")
	}
	m := &Manager{
		reg: cfg.Registry, dyn: cfg.Dynamic, creds: cfg.Credentials, settings: cfg.Settings, http: cfg.HTTP, now: cfg.Now,
		tests: map[string]testResult{}, models: map[string]*modelsEntry{},
	}
	if m.http == nil {
		m.http = NewHTTPClient()
	}
	if m.now == nil {
		m.now = time.Now
	}
	return m, nil
}

// descriptor resolves a built-in provider, then a trusted dynamic one.
func (m *Manager) descriptor(id string) (Descriptor, error) {
	if d, ok := m.lookup(id); ok {
		return d, nil
	}
	return Descriptor{}, fmt.Errorf("unknown provider %q", id)
}

func (m *Manager) lookup(id string) (Descriptor, bool) {
	if d, ok := m.reg.Get(id); ok {
		return d, true
	}
	if m.dyn != nil {
		for _, d := range m.dyn.Descriptors() {
			if d.ID == id && d.New != nil {
				return d, true
			}
		}
	}
	return Descriptor{}, false
}

// all lists built-in providers followed by the trusted dynamic ones (built-in ids win).
func (m *Manager) all() []Descriptor {
	out := m.reg.List()
	if m.dyn == nil {
		return out
	}
	for _, d := range m.dyn.Descriptors() {
		if d.ID == "" || d.New == nil || slices.ContainsFunc(out, func(x Descriptor) bool { return x.ID == d.ID }) {
			continue
		}
		if d.Kind == "" {
			d.Kind = "manifest"
		}
		out = append(out, d)
	}
	return out
}

// readKey fetches the stored key; ErrNotConfigured when absent.
func (m *Manager) readKey(id string) (string, error) {
	raw, err := m.creds.Get(CredentialKey(id))
	if errors.Is(err, credentials.ErrNotFound) {
		return "", ErrNotConfigured
	}
	if err != nil {
		return "", errors.New("the credential store is unavailable")
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", ErrNotConfigured
	}
	return key, nil
}

func (m *Manager) status(d Descriptor) ProviderStatus {
	st := ProviderStatus{ID: d.ID, Name: d.Name, Kind: d.Kind}
	if d.NoKey {
		st.Connected = true
	} else {
		key, err := m.readKey(d.ID)
		switch {
		case err == nil:
			st.Connected = true
			if len(key) >= minHintable {
				st.KeyHint = key[len(key)-keyHintLen:]
			}
		case !errors.Is(err, ErrNotConfigured):
			st.Error = err.Error()
			return st
		default:
			return st
		}
	}
	m.mu.Lock()
	t, ok := m.tests[d.ID]
	m.mu.Unlock()
	if ok && t.rev == d.Revision {
		st.Valid = new(t.valid)
		st.Error = t.err
		if t.account != nil {
			st.Account = new(*t.account)
		}
	}
	return st
}

// Statuses lists every registered provider, built-in first, then trusted manifests.
func (m *Manager) Statuses(context.Context) ([]ProviderStatus, error) {
	ds := m.all()
	out := make([]ProviderStatus, len(ds))
	for i, d := range ds {
		out[i] = m.status(d)
	}
	return out, nil
}

// safeError returns err with every secret scrubbed from its message.
func safeError(err error, secrets ...string) error {
	var le *Error
	if errors.As(err, &le) {
		c := *le
		c.Message = Redact(le.Message, secrets...)
		return &c
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return NetworkError(err, secrets...)
	}
	return errors.New(Redact(err.Error(), secrets...))
}

// Connect validates key with the provider and only then stores it.
func (m *Manager) Connect(ctx context.Context, id, apiKey string) (ProviderStatus, error) {
	d, err := m.descriptor(id)
	if err != nil {
		return ProviderStatus{}, err
	}
	return m.connect(ctx, d, strings.TrimSpace(apiKey))
}

func (m *Manager) connect(ctx context.Context, d Descriptor, key string) (ProviderStatus, error) {
	if d.NoKey {
		if key != "" {
			return ProviderStatus{}, errors.New("this provider does not use an API key")
		}
	} else if key == "" {
		return ProviderStatus{}, errors.New("an API key is required")
	}
	p, err := d.New(Config{APIKey: key, HTTP: m.http})
	if err != nil {
		return ProviderStatus{}, safeError(err, key)
	}
	acct, err := p.Validate(ctx)
	if err != nil {
		return ProviderStatus{}, safeError(err, key)
	}
	if !d.NoKey {
		if err := m.creds.Set(CredentialKey(d.ID), []byte(key)); err != nil {
			return ProviderStatus{}, errors.New("could not store the key in the OS credential store")
		}
	}
	m.mu.Lock()
	m.tests[d.ID] = testResult{rev: d.Revision, valid: true, account: &acct}
	delete(m.models, d.ID)
	m.mu.Unlock()
	return m.status(d), nil
}

// Disconnect deletes the stored key. Disconnecting a provider that is not connected is not an error.
func (m *Manager) Disconnect(_ context.Context, id string) error {
	d, err := m.descriptor(id)
	if err != nil {
		return err
	}
	if d.NoKey {
		return errors.New("this provider does not use an API key; revoke its trust to stop using it")
	}
	if err := m.creds.Delete(CredentialKey(d.ID)); err != nil {
		return errors.New("could not remove the key from the OS credential store")
	}
	m.mu.Lock()
	delete(m.tests, d.ID)
	delete(m.models, d.ID)
	m.mu.Unlock()
	return nil
}

// Test re-validates the stored key and records the outcome in the returned status.
// A rejected key is reported through the status (Valid=false), not as an error.
func (m *Manager) Test(ctx context.Context, id string) (ProviderStatus, error) {
	d, err := m.descriptor(id)
	if err != nil {
		return ProviderStatus{}, err
	}
	p, err := m.provider(d)
	if err != nil {
		return ProviderStatus{}, err
	}
	acct, verr := p.Validate(ctx)
	if verr != nil && ctx.Err() != nil {
		return ProviderStatus{}, ctx.Err()
	}
	res := testResult{rev: d.Revision, valid: verr == nil}
	if verr != nil {
		res.err = AsError(safeError(verr)).Message
	} else {
		res.account = &acct
	}
	m.mu.Lock()
	m.tests[d.ID] = res
	m.mu.Unlock()
	return m.status(d), nil
}

// Provider builds a provider with the stored key, read just now.
func (m *Manager) Provider(_ context.Context, id string) (Provider, error) {
	d, err := m.descriptor(id)
	if err != nil {
		return nil, err
	}
	return m.provider(d)
}

func (m *Manager) provider(d Descriptor) (Provider, error) {
	var key string
	if !d.NoKey {
		var err error
		if key, err = m.readKey(d.ID); err != nil {
			return nil, err
		}
	}
	p, err := d.New(Config{APIKey: key, HTTP: m.http})
	if err != nil {
		return nil, safeError(err, key)
	}
	return p, nil
}

// Models lists a provider's models, cached for five minutes.
func (m *Manager) Models(ctx context.Context, id string) ([]Model, error) {
	d, err := m.descriptor(id)
	if err != nil {
		return nil, err
	}
	p, err := m.provider(d)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	e := m.models[id]
	if e == nil {
		e = &modelsEntry{}
		m.models[id] = e
	}
	m.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.models != nil && e.rev == d.Revision && m.now().Sub(e.at) < modelsTTL {
		return slices.Clone(e.models), nil
	}
	models, err := p.Models(ctx)
	if err != nil {
		return nil, err
	}
	if models == nil {
		models = []Model{}
	}
	e.models, e.rev, e.at = models, d.Revision, m.now()
	return slices.Clone(models), nil
}

func (m *Manager) readSetting(ctx context.Context, key string) (string, error) {
	raw, ok, err := m.settings.Get(ctx, key)
	if err != nil || !ok {
		return "", err
	}
	var s string
	if json.Unmarshal([]byte(raw), &s) != nil {
		return "", nil
	}
	return strings.TrimSpace(s), nil
}

// Defaults returns the persisted default models (empty strings when unset).
func (m *Manager) Defaults(ctx context.Context) (Defaults, error) {
	chat, err := m.readSetting(ctx, SettingChatModel)
	if err != nil {
		return Defaults{}, err
	}
	mem, err := m.readSetting(ctx, SettingMemoryModel)
	if err != nil {
		return Defaults{}, err
	}
	return Defaults{ChatModel: chat, MemoryModel: mem}, nil
}

func (m *Manager) checkSelection(label, sel string) error {
	if sel == "" {
		return nil
	}
	p, _, ok := SplitModel(sel)
	if !ok {
		return fmt.Errorf("%s must look like provider/model", label)
	}
	if _, found := m.lookup(p); !found {
		return fmt.Errorf("%s uses unknown provider %q", label, p)
	}
	return nil
}

// SetDefaults persists the default models. Empty clears a selection.
func (m *Manager) SetDefaults(ctx context.Context, d Defaults) error {
	d.ChatModel, d.MemoryModel = strings.TrimSpace(d.ChatModel), strings.TrimSpace(d.MemoryModel)
	if err := m.checkSelection("chat model", d.ChatModel); err != nil {
		return err
	}
	if err := m.checkSelection("memory model", d.MemoryModel); err != nil {
		return err
	}
	for _, kv := range [][2]string{{SettingChatModel, d.ChatModel}, {SettingMemoryModel, d.MemoryModel}} {
		enc, _ := json.Marshal(kv[1])
		if err := m.settings.Set(ctx, kv[0], string(enc)); err != nil {
			return err
		}
	}
	return nil
}

// OpenRouterAuthStart begins the headless PKCE flow; any earlier pending flow is replaced.
func (m *Manager) OpenRouterAuthStart(context.Context) (AuthStart, error) {
	d, err := m.descriptor(ProviderOpenRouter)
	if err != nil {
		return AuthStart{}, err
	}
	if d.OAuth == nil {
		return AuthStart{}, errors.New("this provider does not support sign-in")
	}
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return AuthStart{}, err
	}
	m.mu.Lock()
	m.pending = &pendingAuth{provider: d.ID, verifier: verifier, expires: m.now().Add(authTTL)}
	m.mu.Unlock()
	return AuthStart{URL: d.OAuth.AuthURL(challenge)}, nil
}

// OpenRouterAuthFinish exchanges the pasted code, validates the new key and stores it.
func (m *Manager) OpenRouterAuthFinish(ctx context.Context, code string) (ProviderStatus, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return ProviderStatus{}, errors.New("paste the code shown by OpenRouter")
	}
	d, err := m.descriptor(ProviderOpenRouter)
	if err != nil {
		return ProviderStatus{}, err
	}
	if d.OAuth == nil {
		return ProviderStatus{}, errors.New("this provider does not support sign-in")
	}
	m.mu.Lock()
	p := m.pending
	if p != nil && !m.now().Before(p.expires) {
		m.pending, p = nil, nil
	}
	m.mu.Unlock()
	if p == nil {
		return ProviderStatus{}, errors.New("the sign-in expired or was never started; start it again")
	}
	key, err := d.OAuth.Exchange(ctx, m.http, code, p.verifier)
	if err != nil {
		return ProviderStatus{}, safeError(err, code, p.verifier)
	}
	st, err := m.connect(ctx, d, strings.TrimSpace(key))
	if err != nil {
		return ProviderStatus{}, safeError(err, key, code, p.verifier)
	}
	m.mu.Lock()
	if m.pending == p {
		m.pending = nil
	}
	m.mu.Unlock()
	return st, nil
}

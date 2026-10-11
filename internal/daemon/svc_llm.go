package daemon

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/builtin"
	"github.com/rayorole/orole-os-desktop/internal/llm/cli"
	"github.com/rayorole/orole-os-desktop/internal/llm/openaicompat"
)

// Test seam: production uses the OS credential store.
var llmCredentialStore = credentials.New

// settingsKV adapts the daemon settings table (JSON values keyed by string).
type settingsKV struct{ db *database.DB }

func (s settingsKV) Get(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s settingsKV) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().Unix())
	return err
}

// registerLLM wires model providers (OpenRouter, OpenAI and user-approved declarative manifests):
// credentials in the OS store, defaults in settings. Connecting and disconnecting are user actions
// and audited without the key.
func (d *Daemon) registerLLM() {
	reg, store := builtin.Registry(), llmCredentialStore()
	if d.CLI != nil {
		// Account providers: the user's own Claude and ChatGPT plans through the vendors' CLIs. They need no key.
		for _, desc := range []llm.Descriptor{cli.ClaudeDescriptor(d.CLI), cli.CodexDescriptor(d.CLI)} {
			if err := reg.Register(desc); err != nil {
				d.Log.Error("account provider unavailable", "id", desc.ID, "err", err)
			}
		}
	}
	mgr, err := llm.NewManager(llm.ManagerConfig{
		Registry:    reg,
		Credentials: store,
		Settings:    settingsKV{d.DB},
		Dynamic:     &openaicompat.Source{Service: d.newLLMManifests(reg, store)},
	})
	if err != nil {
		d.Log.Error("llm providers unavailable", "err", err)
		return
	}
	d.LLM = mgr
	changed := func(id string) { d.IPC.Publish("llm.changed", llm.Change{ID: id}) }
	d.registerLLMManifests(changed)

	handle(d, "llm.providers.list", func(ctx context.Context, _ none) ([]llm.ProviderStatus, error) {
		return mgr.Statuses(ctx)
	})
	handle(d, "llm.providers.connect", func(ctx context.Context, p llm.ConnectRequest) (llm.ProviderStatus, error) {
		st, err := mgr.Connect(ctx, p.ID, p.APIKey)
		if err != nil {
			d.audit("llm.connect", "llm/"+p.ID, "denied", string(llm.AsError(err).Code))
			return llm.ProviderStatus{}, llmUserError(err)
		}
		d.audit("llm.connect", "llm/"+p.ID, "allowed", "key …"+st.KeyHint)
		changed(p.ID)
		return st, nil
	})
	handle(d, "llm.providers.disconnect", func(ctx context.Context, p llm.ProviderRef) (none, error) {
		if err := mgr.Disconnect(ctx, p.ID); err != nil {
			d.audit("llm.disconnect", "llm/"+p.ID, "error", "")
			return none{}, err
		}
		d.audit("llm.disconnect", "llm/"+p.ID, "allowed", "")
		changed(p.ID)
		return none{}, nil
	})
	handle(d, "llm.providers.test", func(ctx context.Context, p llm.ProviderRef) (llm.ProviderStatus, error) {
		st, err := mgr.Test(ctx, p.ID)
		return st, llmUserError(err)
	})
	handle(d, "llm.models", func(ctx context.Context, p llm.ProviderRef) ([]llm.Model, error) {
		models, err := mgr.Models(ctx, p.ID)
		if models == nil {
			models = []llm.Model{}
		}
		return models, llmUserError(err)
	})
	handle(d, "llm.defaults.get", func(ctx context.Context, _ none) (llm.Defaults, error) {
		return mgr.Defaults(ctx)
	})
	handle(d, "llm.defaults.set", func(ctx context.Context, p llm.Defaults) (llm.Defaults, error) {
		if err := mgr.SetDefaults(ctx, p); err != nil {
			return llm.Defaults{}, err
		}
		changed("")
		return mgr.Defaults(ctx)
	})
	handle(d, "llm.openrouter.auth.start", func(ctx context.Context, _ none) (llm.AuthStart, error) {
		return mgr.OpenRouterAuthStart(ctx)
	})
	handle(d, "llm.openrouter.auth.finish", func(ctx context.Context, p llm.AuthFinish) (llm.ProviderStatus, error) {
		st, err := mgr.OpenRouterAuthFinish(ctx, p.Code)
		if err != nil {
			d.audit("llm.connect", "llm/"+llm.ProviderOpenRouter, "denied", "oauth: "+string(llm.AsError(err).Code))
			return llm.ProviderStatus{}, llmUserError(err)
		}
		d.audit("llm.connect", "llm/"+llm.ProviderOpenRouter, "allowed", "oauth, key …"+st.KeyHint)
		changed(llm.ProviderOpenRouter)
		return st, nil
	})
}

// llmUserError turns a provider failure into a message the UI can show as is.
func llmUserError(err error) error {
	var le *llm.Error
	if err == nil || !errors.As(err, &le) {
		return err
	}
	switch le.Code {
	case llm.ErrAuth:
		return errors.New("The provider rejected the key: " + le.Message)
	case llm.ErrQuota:
		return errors.New("The account has no credits left: " + le.Message)
	case llm.ErrRateLimit:
		return errors.New("The provider is rate limiting requests: " + le.Message)
	case llm.ErrNetwork:
		return errors.New("Could not reach the provider: " + le.Message)
	}
	return errors.New(le.Message)
}

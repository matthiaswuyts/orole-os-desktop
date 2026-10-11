package daemon

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rayorole/orole-os-desktop/internal/cliruntime"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// cliRef names one provider CLI.
type cliRef struct {
	ID string `json:"id"`
}

// registerCLIRuntimes exposes the Claude, ChatGPT (Codex) and Grok CLIs: where they are, which version,
// whether a newer one exists and who is signed in. Downloading and signing in are user actions and are
// audited (never with credentials: the CLIs keep those). OROLE_<ID>_PATH (CLAUDE, CODEX, GROK) picks a CLI
// by path.
func (d *Daemon) registerCLIRuntimes() {
	over := map[string]string{}
	for _, id := range []string{cliruntime.Claude, cliruntime.Codex, cliruntime.Grok} {
		if p := os.Getenv("OROLE_" + upper(id) + "_PATH"); p != "" {
			over[id] = p
		}
	}
	m := cliruntime.New(cliruntime.Config{StoreDir: filepath.Join(d.DataDir, "cli"), Overrides: over})
	d.CLI = m
	// Provider lists read the last known state, so take it once at start and again after every change.
	changed := func(id string) { d.IPC.Publish("llm.changed", llm.Change{ID: id}) }
	go func() {
		m.All(context.Background())
		changed("")
	}()

	handle(d, "cli.providers.list", func(ctx context.Context, _ none) ([]cliruntime.Status, error) {
		return m.All(ctx), nil
	})
	handle(d, "cli.providers.install", func(ctx context.Context, p cliRef) (cliruntime.Status, error) {
		st, err := m.Install(ctx, p.ID)
		outcome := "allowed"
		if err != nil {
			outcome = "failed"
		}
		d.audit("cli.install", p.ID, outcome, st.Version)
		changed(p.ID)
		return st, err
	})
	handle(d, "cli.providers.login", func(ctx context.Context, p cliRef) (cliruntime.Status, error) {
		st, err := m.Login(ctx, p.ID)
		d.audit("cli.login", p.ID, outcomeOf(err), "")
		changed(p.ID)
		return st, err
	})
	handle(d, "cli.providers.logout", func(ctx context.Context, p cliRef) (cliruntime.Status, error) {
		st, err := m.Logout(ctx, p.ID)
		d.audit("cli.logout", p.ID, outcomeOf(err), "")
		changed(p.ID)
		return st, err
	})
}

func outcomeOf(err error) string {
	if err != nil {
		return "failed"
	}
	return "allowed"
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

package daemon

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
	"github.com/rayorole/orole-os-desktop/internal/search"
)

// settingsIndex lists searchable settings. Each entry opens a Settings
// section; keywords widen matching beyond the title.
var settingsIndex = []struct{ section, title, keywords string }{
	{"appearance", "Appearance", "theme colour color dark light midnight obsidian density compact custom"},
	{"ascii", "ASCII scenes", "background animation scene fps motion"},
	{"accessibility", "Accessibility", "reduce motion animations a11y"},
	{"terminal", "Terminal", "font size shell"},
	{"notifications", "Notifications", "alerts toasts mute sources native"},
	{"workspaces", "Workspaces", "layouts widgets dashboard"},
	{"sound", "Sound", "audio volume effects mute"},
	{"plugins", "Plugins", "extensions install permissions"},
	{"machines", "Machines", "remote hosts ssh"},
	{"agents", "Agents", "models permissions"},
	{"github", "GitHub", "token repositories issues pull requests"},
	{"mcp", "MCP servers", "model context protocol tools"},
	{"automations", "Automations", "workflows triggers"},
	{"security", "Security", "permissions audit credentials"},
	{"storage", "Storage", "files roots folders disk"},
	{"updates", "Updates", "version release"},
	{"diagnostics", "Diagnostics", "daemon health logs"},
}

// registerSearch wires search.query over the providers this daemon owns.
func (d *Daemon) registerSearch(reg *files.Registry) {
	agg := search.New()

	for _, id := range []string{"local", "git", "remote"} {
		pr, err := reg.Get(id)
		if err != nil {
			continue
		}
		agg.Register("files:"+id, func(ctx context.Context, q string, limit int) ([]search.Result, error) {
			es, err := searchAllRoots(ctx, pr, q, files.SearchOptions{Limit: limit})
			out := make([]search.Result, 0, len(es))
			for _, e := range es {
				kind := "file"
				if e.IsDir {
					kind = "folder"
				}
				out = append(out, search.Result{
					ID: e.Path, Title: e.Name, Subtitle: filepath.Dir(e.Path), Kind: kind,
					Score:  search.Fuzzy(q, e.Name) - 10, // names are weaker evidence than palette commands
					Action: map[string]string{"view": "files", "provider": pr.ID(), "path": e.Path, "isDir": boolStr(e.IsDir)},
				})
			}
			return out, err
		})
	}

	st := notifications.New(d.DB.DB)
	agg.Register("notifications", func(ctx context.Context, q string, limit int) ([]search.Result, error) {
		items, err := st.List(ctx, notifications.ListOptions{Limit: notifications.MaxList})
		if err != nil {
			return nil, err
		}
		var out []search.Result
		for _, n := range items {
			score := max(search.Fuzzy(q, n.Title), search.Fuzzy(q, n.Body)*0.5)
			if score <= 0 {
				continue
			}
			out = append(out, search.Result{ID: n.ID, Title: n.Title, Subtitle: n.Source, Kind: "notification", Score: score - 15,
				Action: map[string]string{"notification": n.ID}})
			if len(out) >= limit {
				break
			}
		}
		return out, nil
	})

	agg.Register("settings", func(ctx context.Context, q string, limit int) ([]search.Result, error) {
		var out []search.Result
		for _, s := range settingsIndex {
			score := max(search.Fuzzy(q, s.title), search.Fuzzy(q, s.keywords)*0.6)
			if score > 0 {
				out = append(out, search.Result{ID: s.section, Title: "Settings: " + s.title, Kind: "setting", Score: score,
					Action: map[string]string{"view": "settings", "section": s.section}})
			}
		}
		return out, nil
	})

	handle(d, "search.providers", func(ctx context.Context, _ none) ([]string, error) { return agg.IDs(), nil })
	handle(d, "search.query", func(ctx context.Context, p struct {
		Query     string   `json:"query"`
		Providers []string `json:"providers"`
		Limit     int      `json:"limit"`
	}) (search.Response, error) {
		// Remote filesystem reads can require policy approval. Only an explicit
		// provider selection may initiate them, never default discovery.
		if len(p.Providers) == 0 {
			p.Providers = []string{"files:local", "files:git", "notifications", "settings"}
		}
		return agg.Query(ctx, strings.TrimSpace(p.Query), p.Providers, p.Limit), nil
	})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

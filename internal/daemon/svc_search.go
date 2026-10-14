package daemon

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/notifications"
	"github.com/rayorole/orole-os-desktop/internal/search"
)

// settingsIndex lists configuration destinations within Settings.
// Keep Settings sections/tabs in step with frontend/src/features/settings/SettingsView.tsx.
var settingsIndex = []struct{ view, section, tab, title, keywords string }{
	{"settings", "customization", "appearance", "Theme and density", "appearance theme colour color dark light midnight obsidian density compact custom"},
	{"settings", "customization", "ascii", "Background scenes", "ascii background animation scene opacity"},
	{"settings", "customization", "motion", "Motion and accessibility", "accessibility reduce motion animations a11y fps pause static"},
	{"settings", "customization", "sound", "Sounds", "sound audio volume effects mute"},
	{"settings", "customization", "terminal", "Terminal font", "terminal font size shell"},
	{"settings", "integrations", "github", "GitHub", "token repositories issues pull requests"},
	{"settings", "skills", "", "Skills", "skills harness instructions import sync"},
	{"settings", "mcp", "", "MCP servers", "model context protocol tools"},
	{"settings", "voice", "", "Voice and dictation", "voice dictation speech microphone transcription speech-to-text model parakeet whisper language download"},
	{"settings", "integrations", "plugins", "Plugins", "extensions install permissions"},
	{"settings", "notifications", "notifications", "Notifications", "alerts sounds needs you approvals failed finished replies mute"},
	{"settings", "task-permissions", "", "Task permissions", "yolo manual auto permission mode bots approve approvals remembered allow"},
	{"settings", "bot-tools", "", "Bot tools", "bots integrations tools access without asking read write approve"},
	{"settings", "bot-code-tasks", "", "Bot code tasks", "bots agent harness model effort reasoning claude codex code tasks"},
	{"settings", "system", "updates", "Updates", "version release"},
	{"settings", "system", "diagnostics", "Diagnostics and startup", "diagnostics daemon health logs autostart login startup"},
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
				title := s.title
				if s.view == "settings" {
					title = "Settings: " + title
				}
				out = append(out, search.Result{ID: s.view + "/" + s.section + "/" + s.tab, Title: title, Kind: "setting", Score: score,
					Action: map[string]string{"view": s.view, "section": s.section, "tab": s.tab}})
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

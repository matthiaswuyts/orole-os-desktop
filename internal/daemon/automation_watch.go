package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/github"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
)

// Watch triggers poll integrations (Orole has no network listener). Items carry only
// display fields; titles, messages and branch names are untrusted data.

const watchPage = 30

type ghWatch struct{ g *ghService }

func (w ghWatch) Poll(ctx context.Context, _ string, cfg automations.Watch) ([]automations.WatchItem, error) {
	c, err := w.g.conn()
	if err != nil {
		return nil, err
	}
	var out []automations.WatchItem
	switch cfg.Kind {
	case "push":
		commits, err := c.Commits(ctx, cfg.Repo, cfg.Branch, watchPage)
		if err != nil {
			return nil, err
		}
		for _, cm := range commits {
			author := cm.Commit.Author.Name
			if cm.Author != nil && cm.Author.Login != "" {
				author = cm.Author.Login
			}
			out = append(out, automations.WatchItem{Key: cm.SHA, Data: map[string]any{
				"repo": cfg.Repo, "branch": cfg.Branch, "sha": cm.SHA, "url": cm.HTMLURL, "author": author,
				"message": clip(cm.Commit.Message.String()), "title": firstLine(cm.Commit.Message.String()),
			}})
		}
	case "pr_opened", "pr_merged":
		pulls, err := c.ListPulls(ctx, cfg.Repo, "all", watchPage)
		if err != nil {
			return nil, err
		}
		for _, p := range pulls {
			if cfg.Kind == "pr_merged" && p.MergedAt == nil {
				continue
			}
			out = append(out, automations.WatchItem{Key: "pr:" + strconv.Itoa(p.Number), Data: map[string]any{
				"repo": cfg.Repo, "number": p.Number, "title": clip(p.Title.String()), "url": p.HTMLURL, "author": p.User.Login,
				"head": p.Head.Ref, "base": p.Base.Ref, "state": p.State, "draft": p.Draft, "merged": p.MergedAt != nil,
			}})
		}
	case "workflow_failed":
		runs, err := c.Runs(ctx, cfg.Repo, github.RunFilter{Status: "failure", Branch: cfg.Branch, Limit: watchPage})
		if err != nil {
			return nil, err
		}
		for _, r := range runs {
			out = append(out, automations.WatchItem{Key: "run:" + strconv.FormatInt(r.ID, 10), Data: map[string]any{
				"repo": cfg.Repo, "runId": r.ID, "name": clip(r.Name.String()), "title": clip(r.DisplayTitle.String()), "url": r.HTMLURL,
				"branch": r.HeadBranch.String(), "sha": r.HeadSHA, "event": r.Event, "conclusion": r.Conclusion, "actor": r.Actor.Login,
			}})
		}
	case "issue_opened":
		issues, err := c.ListIssues(ctx, cfg.Repo, github.IssueFilter{State: "all", Limit: watchPage})
		if err != nil {
			return nil, err
		}
		for _, is := range issues {
			out = append(out, automations.WatchItem{Key: "issue:" + strconv.Itoa(is.Number), Data: map[string]any{
				"repo": cfg.Repo, "number": is.Number, "title": clip(is.Title.String()), "url": is.HTMLURL, "author": is.User.Login, "state": is.State,
			}})
		}
	default:
		return nil, fmt.Errorf("unknown GitHub watch %q", cfg.Kind)
	}
	return out, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return clip(line)
}

// mcpWatch polls an MCP integration tool that lists deployments (Vercel
// list_deployments, Railway list-deployments, ...). The tool runs through the shared
// policy as the automation, like an mcp.tool step.
type mcpWatch struct{ d *Daemon }

func (w mcpWatch) Poll(ctx context.Context, actor string, cfg automations.Watch) ([]automations.WatchItem, error) {
	if w.d.MCP == nil {
		return nil, errors.New("integrations are unavailable")
	}
	res, err := w.d.MCP.InvokeChecked(ctx, cfg.Server, cfg.Tool, cfg.Args, mcp.CallOptions{Actor: actor})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		return nil, fmt.Errorf("%s reported an error: %s", cfg.Tool, clip(resultText(res)))
	}
	return deploymentItems(cfg, res)
}

// resultText joins the text blocks of a tool result.
func resultText(res *mcp.CallResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// deploymentItems finds the list of deployments in a tool result (structured content,
// or JSON text) and keys each by id and state, so a state change is a new item.
func deploymentItems(cfg automations.Watch, res *mcp.CallResult) ([]automations.WatchItem, error) {
	var doc any
	if len(res.Structured) > 0 && json.Unmarshal(res.Structured, &doc) == nil {
		if list := findObjects(doc, 0); list != nil {
			return toDeployments(cfg, list), nil
		}
	}
	text := strings.TrimSpace(resultText(res))
	if start := strings.IndexAny(text, "[{"); start >= 0 && json.Unmarshal([]byte(text[start:]), &doc) == nil {
		if list := findObjects(doc, 0); list != nil {
			return toDeployments(cfg, list), nil
		}
	}
	return nil, fmt.Errorf("%s did not return a JSON list of deployments", cfg.Tool)
}

// findObjects returns the first array of objects in doc, breadth before depth.
func findObjects(doc any, depth int) []map[string]any {
	if depth > 4 {
		return nil
	}
	switch v := doc.(type) {
	case []any:
		var out []map[string]any
		for _, x := range v {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
	case map[string]any:
		for _, key := range []string{"deployments", "items", "data", "nodes", "edges", "results"} {
			if list := findObjects(v[key], depth+1); list != nil {
				return list
			}
		}
		for _, x := range v {
			if list := findObjects(x, depth+1); list != nil {
				return list
			}
		}
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

func toDeployments(cfg automations.Watch, list []map[string]any) []automations.WatchItem {
	out := make([]automations.WatchItem, 0, len(list))
	for _, raw := range list {
		if node, ok := raw["node"].(map[string]any); ok { // GraphQL edges
			raw = node
		}
		id := firstString(raw, "uid", "id", "deploymentId", "deployment_id")
		state := firstString(raw, "state", "readyState", "status")
		if id == "" || state == "" {
			continue
		}
		if len(cfg.States) > 0 && !slices.ContainsFunc(cfg.States, func(s string) bool { return strings.EqualFold(s, state) }) {
			continue
		}
		data := map[string]any{"server": cfg.Server, "tool": cfg.Tool, "id": id, "state": state}
		for k, v := range raw {
			switch x := v.(type) {
			case string:
				data[k] = clip(x)
			case float64, bool:
				data[k] = x
			}
		}
		data["id"], data["state"] = id, state
		out = append(out, automations.WatchItem{Key: id + ":" + strings.ToUpper(state), Data: data})
	}
	return out
}

var (
	_ automations.WatchSource = ghWatch{}
	_ automations.WatchSource = mcpWatch{}
)

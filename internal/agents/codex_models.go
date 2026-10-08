package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"
)

// codexModelsTTL is how long a model list is reused; spawning app-server costs about a second.
const codexModelsTTL = 5 * time.Minute

var codexModelCache struct {
	mu     sync.Mutex
	path   string
	at     time.Time
	models []ModelOption
}

// codexModels asks `codex app-server` (model/list) which models the signed-in account can use and
// which reasoning levels each supports. The result is cached per executable for a few minutes.
func codexModels(ctx context.Context, bin string) ([]ModelOption, error) {
	codexModelCache.mu.Lock()
	defer codexModelCache.mu.Unlock()
	if codexModelCache.path == bin && time.Since(codexModelCache.at) < codexModelsTTL {
		return slices.Clone(codexModelCache.models), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	p, err := startCodexProc("", bin)
	if err != nil {
		return nil, err
	}
	defer p.abort()
	go func() {
		for range p.events { // nothing here consumes notifications
		}
	}()
	// call() takes its own timeout; cap each one by what is left of ctx.
	left := func() time.Duration {
		d, _ := ctx.Deadline()
		return max(time.Until(d), time.Millisecond)
	}
	if _, err := p.call("initialize", map[string]any{"clientInfo": map[string]any{"name": "orole", "title": "Orole OS", "version": "1"}}, left()); err != nil {
		return nil, fmt.Errorf("codex initialize: %w", err)
	}
	if err := p.notify("initialized", nil); err != nil {
		return nil, err
	}
	var out []ModelOption
	cursor := ""
	for range 5 {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := p.call("model/list", params, left())
		if err != nil {
			return nil, fmt.Errorf("codex model/list: %w", err)
		}
		var resp struct {
			Data []struct {
				ID          string `json:"id"`
				Model       string `json:"model"`
				DisplayName string `json:"displayName"`
				IsDefault   bool   `json:"isDefault"`
				Hidden      bool   `json:"hidden"`
				Efforts     []struct {
					Effort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, err
		}
		for _, m := range resp.Data {
			if m.Hidden {
				continue
			}
			id := m.Model
			if id == "" {
				id = m.ID
			}
			name := m.DisplayName
			if name == "" {
				name = id
			}
			levels := make([]string, 0, len(m.Efforts))
			for _, e := range m.Efforts {
				if e.Effort != "" {
					levels = append(levels, e.Effort)
				}
			}
			out = append(out, ModelOption{ID: id, Name: name, Reasoning: levels, Default: m.IsDefault})
		}
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	codexModelCache.path, codexModelCache.at, codexModelCache.models = bin, time.Now(), slices.Clone(out)
	return out, nil
}

// modelReasoning is the union of the models' reasoning levels, in first-seen order.
func modelReasoning(models []ModelOption) []string {
	out := []string{}
	for _, m := range models {
		for _, level := range m.Reasoning {
			if !slices.Contains(out, level) {
				out = append(out, level)
			}
		}
	}
	return out
}

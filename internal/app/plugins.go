package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/plugins"
)

var PluginsChanged = mygo.NewEvent[plugins.PluginChange]("plugins:changed")

func forwardPlugins(event string, data json.RawMessage) bool {
	if event != "plugins.changed" {
		return false
	}
	var change plugins.PluginChange
	if json.Unmarshal(data, &change) == nil {
		PluginsChanged.Broadcast(change)
	}
	return true
}

// Plugins is a stateless bridge; all validation, trust and execution live in Go's daemon.
type Plugins struct{ L *Link }

func (p Plugins) List(ctx context.Context) ([]plugins.PluginRecord, error) {
	var out []plugins.PluginRecord
	err := p.L.call(ctx, "plugins.list", struct{}{}, &out)
	return out, err
}

func (p Plugins) Save(ctx context.Context, req plugins.PluginSaveRequest) (plugins.PluginRecord, error) {
	var out plugins.PluginRecord
	err := p.L.call(ctx, "plugins.save", req, &out)
	return out, err
}

func (p Plugins) Trust(ctx context.Context, req plugins.PluginTrustRequest) (plugins.PluginRecord, error) {
	var out plugins.PluginRecord
	err := p.L.call(ctx, "plugins.trust", req, &out)
	return out, err
}

func (p Plugins) Remove(ctx context.Context, req plugins.PluginRef) error {
	return p.L.call(ctx, "plugins.remove", req, nil)
}

func (p Plugins) Run(ctx context.Context, req plugins.PluginRunRequest) (plugins.PluginRunResult, error) {
	var out plugins.PluginRunResult
	err := p.L.call(ctx, "plugins.run", req, &out)
	return out, err
}

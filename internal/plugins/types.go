// Package plugins implements capability-limited, declarative MCP extensions.
// Manifests are data, never executable code; plugin trust never grants tool permission.
package plugins

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

var (
	ErrInvalid   = errors.New("plugins: invalid manifest or request")
	ErrNotFound  = errors.New("plugins: plugin not found")
	ErrChanged   = errors.New("plugins: manifest or trust changed; reload and review again")
	ErrUntrusted = errors.New("plugins: this exact manifest is not enabled and trusted")
	ErrClosed    = errors.New("plugins: manager is closed")
)

type PluginCapability struct {
	ServerID string `json:"serverId"`
	Tool     string `json:"tool"`
}

type PluginAction struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	ServerID    string          `json:"serverId"`
	Tool        string          `json:"tool"`
	Arguments   json.RawMessage `json:"arguments"`
}

type PluginCommand struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ActionID    string `json:"actionId"`
}

type PluginWidget struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ActionID    string `json:"actionId"`
}

type PluginManifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Description   string             `json:"description"`
	Capabilities  []PluginCapability `json:"capabilities"`
	Actions       []PluginAction     `json:"actions"`
	Commands      []PluginCommand    `json:"commands"`
	Widgets       []PluginWidget     `json:"widgets"`
}

type PluginRecord struct {
	Manifest       PluginManifest `json:"manifest"`
	Digest         string         `json:"digest"`
	Enabled        bool           `json:"enabled"`
	ApprovedDigest string         `json:"approvedDigest"`
	UpdatedAt      int64          `json:"updatedAt"`
}

type PluginSaveRequest struct {
	Manifest       PluginManifest `json:"manifest"`
	ExpectedDigest string         `json:"expectedDigest"`
}

type PluginTrustRequest struct {
	ID      string `json:"id"`
	Digest  string `json:"digest"`
	Enabled bool   `json:"enabled"`
}

type PluginRef struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

type PluginRunRequest struct {
	ID       string `json:"id"`
	Digest   string `json:"digest"`
	ActionID string `json:"actionId"`
}

type PluginRunResult struct {
	Text      string `json:"text"`
	IsError   bool   `json:"isError"`
	Truncated bool   `json:"truncated"`
}

type PluginChange struct {
	ID string `json:"id"`
}

// Authorizer is implemented by the daemon's shared policy.Engine.
type Authorizer interface {
	Require(context.Context, policy.Request) error
}

// Invoker deliberately exposes only the policy-checked MCP entry point.
type Invoker interface {
	InvokeChecked(context.Context, string, string, map[string]any, mcp.CallOptions) (*mcp.CallResult, error)
}

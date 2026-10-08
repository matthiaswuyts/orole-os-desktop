package drive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/maildrive"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
)

const MaxShareBytes = 64 << 10

var ErrStaleSnapshot = errors.New("drive: source or content changed; refresh the preview and choose the recipient again")

// Snapshot is an in-memory read result, not a capability or a reusable grant.
// It contains no filename, credentials, remote URL or access to other files.
type Snapshot struct {
	Preview        Preview
	ReadResource   string
	SourceRevision string
}

func contentHash(mime, text string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(mime))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// Snapshot reads exactly the selected server/file and rejects anything that
// cannot be delivered as a complete, unchanged, bounded text snapshot.
func (a *Adapter) Snapshot(ctx context.Context, serverID, fileID, expectedHash string) (Snapshot, error) {
	if serverID == "" || len(expectedHash) != 64 {
		return Snapshot{}, ErrBadRequest
	}
	s, err := a.readSnapshot(ctx, serverID, fileID)
	if err != nil {
		return Snapshot{}, err
	}
	if s.Preview.SnapshotHash == "" {
		return Snapshot{}, ErrUnsupported
	}
	if s.Preview.SnapshotHash != expectedHash {
		return Snapshot{}, ErrStaleSnapshot
	}
	return s, nil
}

func (a *Adapter) source(ctx context.Context, serverID string) (mcp.ServerState, string, string, error) {
	if a.manager == nil {
		return mcp.ServerState{}, "", "", ErrNotConfigured
	}
	server, ok := a.manager.ByAdapter(ctx, "drive")
	if !ok {
		return server, "", "", ErrNotConfigured
	}
	if serverID != "" && server.ID != serverID {
		return server, "", "", ErrStaleSnapshot
	}
	if server.Status != mcp.StatusConnected {
		return server, "", "", mcp.ErrNotConnected
	}
	_, bindings, err := maildrive.Discover(ctx, a.manager, "drive", map[string]maildrive.Operation{"preview": operations["preview"]})
	if err != nil {
		return server, "", "", err
	}
	binding, ok := bindings["preview"]
	if !ok {
		return server, "", "", ErrUnsupported
	}
	tools, err := a.manager.Tools(ctx, server.ID)
	if err != nil {
		return server, "", "", err
	}
	for _, tool := range tools {
		if tool.Name == binding.Tool {
			raw, _ := json.Marshal(struct {
				Server mcp.ServerConfig
				Tool   mcp.ToolInfo
			}{server.ServerConfig, tool})
			sum := sha256.Sum256(raw)
			return server, "mcp/" + server.ID + "/" + tool.Name, hex.EncodeToString(sum[:]), nil
		}
	}
	return server, "", "", ErrStaleSnapshot
}

func (a *Adapter) readSnapshot(ctx context.Context, serverID, fileID string) (Snapshot, error) {
	if _, err := maildrive.ID(fileID); err != nil {
		return Snapshot{}, ErrBadRequest
	}
	server, resource, revision, err := a.source(ctx, serverID)
	if err != nil {
		return Snapshot{}, err
	}
	pinned := pinnedSource{Manager: a.manager, server: server.ServerConfig}
	raw, err := maildrive.Call(ctx, pinned, a.policy, "drive", a.actor, "preview", operations, map[string]any{"id": fileID})
	if err != nil {
		return Snapshot{}, err
	}
	preview, err := parsePreview(raw, fileID)
	if err != nil {
		return Snapshot{}, err
	}
	preview.ServerID = server.ID
	s := Snapshot{Preview: preview, ReadResource: resource, SourceRevision: revision}
	if err := a.CheckSnapshot(ctx, s); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

// CheckSnapshot detects source reassignment, schema/configuration and tool
// permission changes without rereading or replacing the approved content.
func (a *Adapter) CheckSnapshot(ctx context.Context, s Snapshot) error {
	_, resource, revision, err := a.source(ctx, s.Preview.ServerID)
	if err != nil {
		return err
	}
	if resource != s.ReadResource || revision != s.SourceRevision {
		return ErrStaleSnapshot
	}
	return ctx.Err()
}

type pinnedSource struct {
	maildrive.Manager
	server mcp.ServerConfig
}

func (p pinnedSource) ByAdapter(ctx context.Context, adapter string) (mcp.ServerState, bool) {
	s, ok := p.Manager.ByAdapter(ctx, adapter)
	return s, ok && s.ServerConfig == p.server
}

func (p pinnedSource) CallTool(ctx context.Context, id, tool string, args map[string]any, options mcp.CallOptions) (*mcp.CallResult, error) {
	s, ok := p.ByAdapter(ctx, "drive")
	if !ok || id != p.server.ID || s.Status != mcp.StatusConnected {
		return nil, ErrStaleSnapshot
	}
	return p.Manager.CallTool(ctx, id, tool, args, options)
}

package daemon

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

// Image generation and session artifacts.
//
// IPC methods: images.start {prompt} -> Session (a read-only Codex session in the "images" group
// that generates one image), agents.artifact {id, path} -> Artifact (an image file inside that
// session's own directory; never an arbitrary path).
// Generated images arrive as "image" events on the session (agents.event).
func (d *Daemon) registerImages() {
	handle(d, "images.start", func(ctx context.Context, p struct {
		Prompt string `json:"prompt"`
	}) (agents.Session, error) {
		return d.Agents.StartImage(ctx, p.Prompt)
	})
	handle(d, "agents.artifact", func(ctx context.Context, p struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}) (agents.Artifact, error) {
		a, err := d.Agents.Artifact(p.ID, p.Path)
		if errors.Is(err, agents.ErrNotFound) {
			return agents.Artifact{}, &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
		}
		return a, err
	})
}

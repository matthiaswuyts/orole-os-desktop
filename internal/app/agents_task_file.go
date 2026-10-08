package app

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/agents"
)

// TaskFile returns a bounded, inert preview from the task's approved local workspace.
func (a Agents) TaskFile(ctx context.Context, id, path string) (agents.TaskFilePreview, error) {
	var out agents.TaskFilePreview
	err := a.L.call(ctx, "agents.taskFile", map[string]string{"id": id, "path": path}, &out)
	return out, err
}

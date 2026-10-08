package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// TaskDirectory resolves a registered project's execution directory. Folderless projects
// get a persistent daemon-owned workspace without changing their optional public Path.
// An explicit directory must be the project's workspace or one of its registered worktrees.
func (s *Service) TaskDirectory(ctx context.Context, id, machine, dir, managedRoot string) (string, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if machine == "" {
		machine = machineLocal
	}
	if p.MachineID != machine || machine != machineLocal {
		return "", invalid("project tasks require the project's local machine")
	}
	root := p.Path
	if root == "" {
		if _, err := uuid.Parse(p.ID); err != nil {
			return "", invalid("invalid managed workspace project ID")
		}
		if !filepath.IsAbs(managedRoot) {
			return "", invalid("managed workspace root must be absolute")
		}
		if err := os.MkdirAll(managedRoot, 0o700); err != nil {
			return "", fmt.Errorf("create managed workspace root: %w", err)
		}
		base, err := approvedProjectPath(managedRoot)
		if err != nil {
			return "", err
		}
		root = filepath.Join(base, p.ID)
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", fmt.Errorf("create project workspace: %w", err)
		}
	}
	// Registered roots are pinned, not silently re-approved after a symlink/junction swap.
	resolved, err := approvedProjectPath(root)
	if err != nil {
		return "", err
	}
	if pathKey(resolved) != pathKey(root) {
		return "", invalid("project workspace has moved; register its new location explicitly")
	}
	if dir == "" || pathKey(dir) == pathKey(root) {
		return root, nil
	}
	if p.IsGit {
		worktrees, err := s.Worktrees.List(ctx, p.ID)
		if err != nil {
			return "", err
		}
		for _, wt := range worktrees {
			if pathKey(wt.Path) == pathKey(dir) {
				return wt.Path, nil
			}
		}
	}
	return "", invalid("task directory does not belong to the selected project")
}

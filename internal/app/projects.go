package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/git"
	"github.com/rayorole/orole-os-desktop/internal/projects"
)

// Events for the project tree and worktree views.
var (
	// ProjectsChanged fires when the project registry changes.
	ProjectsChanged = mygo.NewEvent[ProjectRef]("projects:changed")
	// WorktreesChanged fires when worktrees of a project were created or removed.
	WorktreesChanged = mygo.NewEvent[ProjectRef]("worktrees:changed")
)

// ProjectRef identifies the project an event concerns.
type ProjectRef struct {
	ProjectID string `json:"projectId"`
}

// forwardProjects relays project events from the daemon; it reports whether the event was handled.
func forwardProjects(event string, data json.RawMessage) bool {
	var r ProjectRef
	switch event {
	case "projects.changed":
		if json.Unmarshal(data, &r) == nil {
			ProjectsChanged.Broadcast(r)
		}
	case "worktrees.changed":
		if json.Unmarshal(data, &r) == nil {
			WorktreesChanged.Broadcast(r)
		}
	default:
		return false
	}
	return true
}

// Projects is the bound service for the project registry.
type Projects struct{ L *Link }

// List returns registered projects, most recently opened first.
func (p Projects) List(ctx context.Context) ([]projects.Project, error) {
	var out []projects.Project
	err := p.L.call(ctx, "projects.list", struct{}{}, &out)
	return out, err
}

// Add registers the git repository containing path.
func (p Projects) Add(ctx context.Context, path string) (projects.Project, error) {
	var out projects.Project
	err := p.L.call(ctx, "projects.add", map[string]string{"path": path}, &out)
	return out, err
}

// Remove unregisters a project (files stay on disk).
func (p Projects) Remove(ctx context.Context, id string) error {
	return p.L.call(ctx, "projects.remove", map[string]string{"id": id}, nil)
}

// Open records that a project was opened.
func (p Projects) Open(ctx context.Context, id string) error {
	return p.L.call(ctx, "projects.open", map[string]string{"id": id}, nil)
}

// SetWorktreeRoot configures where new worktrees are created ("" = default).
func (p Projects) SetWorktreeRoot(ctx context.Context, id, root string) (projects.Project, error) {
	var out projects.Project
	err := p.L.call(ctx, "projects.setWorktreeRoot", map[string]string{"id": id, "root": root}, &out)
	return out, err
}

// PickFolder shows the native folder picker and returns the chosen path ("" if cancelled).
func (p Projects) PickFolder(ctx context.Context) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx), Title: "Add a project", ButtonLabel: "Add project", Directory: true, CreateDirectories: true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// Git is the bound service for read-only git state of registered projects and worktrees.
// A worktreeID, when given, takes precedence over projectID.
type Git struct{ L *Link }

func target(projectID, worktreeID string) map[string]any {
	return map[string]any{"projectId": projectID, "worktreeId": worktreeID}
}

// Status returns porcelain status with branch ahead/behind.
func (g Git) Status(ctx context.Context, projectID, worktreeID string) (git.Status, error) {
	var out git.Status
	err := g.L.call(ctx, "git.status", target(projectID, worktreeID), &out)
	return out, err
}

// DiffRequest selects a diff: unstaged by default, staged, work tree vs From, or From..To.
type DiffRequest struct {
	ProjectID  string `json:"projectId"`
	WorktreeID string `json:"worktreeId"`
	Staged     bool   `json:"staged"`
	From       string `json:"from"`
	To         string `json:"to"`
	Path       string `json:"path"`
	Context    int    `json:"context"`
}

// Diff returns a parsed diff.
func (g Git) Diff(ctx context.Context, req DiffRequest) (git.DiffResult, error) {
	var out git.DiffResult
	err := g.L.call(ctx, "git.diff", req, &out)
	return out, err
}

// Log returns recent commits reachable from ref ("" = HEAD).
func (g Git) Log(ctx context.Context, projectID, worktreeID, ref string, limit int) ([]git.Commit, error) {
	var out []git.Commit
	m := target(projectID, worktreeID)
	m["ref"], m["limit"] = ref, limit
	err := g.L.call(ctx, "git.log", m, &out)
	return out, err
}

// Branches lists local and remote-tracking branches.
func (g Git) Branches(ctx context.Context, projectID, worktreeID string) ([]git.Branch, error) {
	var out []git.Branch
	err := g.L.call(ctx, "git.branches", target(projectID, worktreeID), &out)
	return out, err
}

// CreateBranch creates a branch at start ("" = HEAD) without checking it out.
func (g Git) CreateBranch(ctx context.Context, projectID, worktreeID, name, start string) error {
	m := target(projectID, worktreeID)
	m["name"], m["start"] = name, start
	return g.L.call(ctx, "git.createBranch", m, nil)
}

// Worktrees is the bound service for git worktrees.
type Worktrees struct{ L *Link }

// List returns worktrees of a project, or of every project when projectID is "".
func (w Worktrees) List(ctx context.Context, projectID string) ([]projects.Worktree, error) {
	var out []projects.Worktree
	err := w.L.call(ctx, "worktrees.list", map[string]string{"projectId": projectID}, &out)
	return out, err
}

// Create adds a worktree on new branch orole/<slug(name)> started at baseRef ("" = HEAD).
func (w Worktrees) Create(ctx context.Context, projectID, name, baseRef string) (projects.Worktree, error) {
	var out projects.Worktree
	err := w.L.call(ctx, "worktrees.create", map[string]string{"projectId": projectID, "name": name, "baseRef": baseRef}, &out)
	return out, err
}

// Remove deletes a linked worktree; force discards local changes.
func (w Worktrees) Remove(ctx context.Context, id string, force bool) error {
	return w.L.call(ctx, "worktrees.remove", map[string]any{"id": id, "force": force}, nil)
}

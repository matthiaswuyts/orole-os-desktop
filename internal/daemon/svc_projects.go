package daemon

import (
	"context"
	"errors"

	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/internal/git"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/projects"
)

// Daemon.Projects is the project registry and worktree manager (also used by other modules).
// Clients address git operations by project or worktree id, never by arbitrary filesystem path.

type gitTarget struct {
	ProjectID  string `json:"projectId"`
	WorktreeID string `json:"worktreeId"`
}

func mapProjectErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, projects.ErrNotFound):
		return &ipc.Error{Code: ipc.CodeNotFound, Message: err.Error()}
	case errors.Is(err, projects.ErrInvalid), errors.Is(err, git.ErrInvalidRef), errors.Is(err, git.ErrNotRepo):
		return &ipc.Error{Code: ipc.CodeBadRequest, Message: err.Error()}
	case errors.Is(err, projects.ErrConflict):
		return &ipc.Error{Code: "conflict", Message: err.Error()}
	}
	return err
}

// dir resolves a target to a directory the daemon knows about.
func (d *Daemon) gitDir(ctx context.Context, t gitTarget) (string, error) {
	if t.WorktreeID != "" {
		path, err := d.Projects.Worktrees.Path(ctx, t.WorktreeID)
		if err != nil {
			return "", mapProjectErr(err)
		}
		return path, nil
	}
	if t.ProjectID == "" {
		return "", &ipc.Error{Code: ipc.CodeBadRequest, Message: "projectId or worktreeId is required"}
	}
	p, err := d.Projects.Get(ctx, t.ProjectID)
	if err != nil {
		return "", mapProjectErr(err)
	}
	if p.Path == "" {
		return "", &ipc.Error{Code: ipc.CodeBadRequest, Message: "project " + p.Name + " has no folder"}
	}
	if p.Missing {
		return "", &ipc.Error{Code: ipc.CodeNotFound, Message: "project directory is missing: " + p.Path}
	}
	if !p.IsGit {
		return "", &ipc.Error{Code: ipc.CodeBadRequest, Message: "project " + p.Name + " is not a git repository"}
	}
	return p.Path, nil
}

func (d *Daemon) registerProjects() {
	d.Projects = projects.New(d.DB)
	changed := func(p projects.Project) { d.IPC.Publish("projects.changed", map[string]string{"projectId": p.ID}) }

	handle(d, "projects.list", func(ctx context.Context, _ none) ([]projects.Project, error) {
		return d.Projects.List(ctx)
	})
	handle(d, "projects.add", func(ctx context.Context, p struct {
		Path string `json:"path"`
	}) (projects.Project, error) {
		pr, err := d.Projects.Add(ctx, p.Path)
		if err != nil {
			d.audit("project.add", p.Path, "failed", err.Error())
			return pr, mapProjectErr(err)
		}
		d.audit("project.add", pr.ID, "allowed", pr.Path)
		changed(pr)
		projectCreated(d.Events, pr, events.ChainFrom(ctx))
		return pr, nil
	})
	handle(d, "projects.create", func(ctx context.Context, p struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}) (projects.Project, error) {
		pr, err := d.Projects.Create(ctx, p.Name, p.Path)
		if err != nil {
			d.audit("project.create", p.Name, "failed", err.Error())
			return pr, mapProjectErr(err)
		}
		d.audit("project.create", pr.ID, "allowed", pr.Path)
		changed(pr)
		projectCreated(d.Events, pr, events.ChainFrom(ctx))
		return pr, nil
	})
	handle(d, "projects.remove", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (none, error) {
		if err := d.Projects.Remove(ctx, p.ID); err != nil {
			return none{}, mapProjectErr(err)
		}
		d.audit("project.remove", p.ID, "allowed", "")
		d.IPC.Publish("projects.changed", map[string]string{"projectId": p.ID})
		d.IPC.Publish("worktrees.changed", map[string]string{"projectId": p.ID})
		return none{}, nil
	})
	handle(d, "projects.open", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (none, error) {
		return none{}, mapProjectErr(d.Projects.MarkOpened(ctx, p.ID))
	})
	handle(d, "projects.setWorktreeRoot", func(ctx context.Context, p struct {
		ID   string `json:"id"`
		Root string `json:"root"`
	}) (projects.Project, error) {
		pr, err := d.Projects.SetWorktreeRoot(ctx, p.ID, p.Root)
		if err == nil {
			d.audit("project.worktreeRoot", p.ID, "allowed", p.Root)
			changed(pr)
		}
		return pr, mapProjectErr(err)
	})

	handle(d, "git.status", func(ctx context.Context, t gitTarget) (git.Status, error) {
		dir, err := d.gitDir(ctx, t)
		if err != nil {
			return git.Status{}, err
		}
		st, err := git.GetStatus(ctx, dir)
		return st, mapProjectErr(err)
	})
	handle(d, "git.diff", func(ctx context.Context, p struct {
		gitTarget
		git.DiffOptions
	}) (git.DiffResult, error) {
		dir, err := d.gitDir(ctx, p.gitTarget)
		if err != nil {
			return git.DiffResult{}, err
		}
		r, err := git.Diff(ctx, dir, p.DiffOptions)
		return r, mapProjectErr(err)
	})
	handle(d, "git.log", func(ctx context.Context, p struct {
		gitTarget
		Ref   string `json:"ref"`
		Limit int    `json:"limit"`
	}) ([]git.Commit, error) {
		dir, err := d.gitDir(ctx, p.gitTarget)
		if err != nil {
			return nil, err
		}
		r, err := git.Log(ctx, dir, p.Ref, p.Limit)
		return r, mapProjectErr(err)
	})
	handle(d, "git.branches", func(ctx context.Context, t gitTarget) ([]git.Branch, error) {
		dir, err := d.gitDir(ctx, t)
		if err != nil {
			return nil, err
		}
		r, err := git.Branches(ctx, dir)
		return r, mapProjectErr(err)
	})
	handle(d, "git.createBranch", func(ctx context.Context, p struct {
		gitTarget
		Name  string `json:"name"`
		Start string `json:"start"`
	}) (none, error) {
		dir, err := d.gitDir(ctx, p.gitTarget)
		if err != nil {
			return none{}, err
		}
		if err := git.CreateBranch(ctx, dir, p.Name, p.Start); err != nil {
			d.audit("git.createBranch", p.Name, "failed", err.Error())
			return none{}, mapProjectErr(err)
		}
		d.audit("git.createBranch", p.Name, "allowed", dir)
		d.IPC.Publish("worktrees.changed", map[string]string{"projectId": p.ProjectID})
		return none{}, nil
	})

	// worktrees.list is the fast path (no per-worktree git status); worktrees.status adds ahead/behind and dirty counts.
	listWorktrees := func(ctx context.Context, projectID string, withStatus bool) ([]projects.Worktree, error) {
		ids := []string{projectID}
		if projectID == "" {
			all, err := d.Projects.List(ctx)
			if err != nil {
				return nil, err
			}
			ids = ids[:0]
			for _, pr := range all {
				if !pr.Missing && pr.IsGit {
					ids = append(ids, pr.ID)
				}
			}
		}
		out := []projects.Worktree{}
		for _, id := range ids {
			var wts []projects.Worktree
			var err error
			if withStatus {
				wts, err = d.Projects.Worktrees.ListStatus(ctx, id)
			} else {
				wts, err = d.Projects.Worktrees.List(ctx, id)
			}
			if err != nil {
				if projectID == "" {
					d.Log.Warn("listing worktrees", "project", id, "err", err)
					continue
				}
				return nil, mapProjectErr(err)
			}
			out = append(out, wts...)
		}
		return out, nil
	}
	type worktreesQuery struct {
		ProjectID string `json:"projectId"` // empty = every project
	}
	handle(d, "worktrees.list", func(ctx context.Context, p worktreesQuery) ([]projects.Worktree, error) {
		return listWorktrees(ctx, p.ProjectID, false)
	})
	handle(d, "worktrees.status", func(ctx context.Context, p worktreesQuery) ([]projects.Worktree, error) {
		return listWorktrees(ctx, p.ProjectID, true)
	})
	handle(d, "worktrees.create", func(ctx context.Context, p struct {
		ProjectID string `json:"projectId"`
		Name      string `json:"name"`
		BaseRef   string `json:"baseRef"`
	}) (projects.Worktree, error) {
		wt, err := d.Projects.Worktrees.Create(ctx, p.ProjectID, p.Name, p.BaseRef)
		if err != nil {
			d.audit("worktree.create", p.ProjectID, "failed", err.Error())
			return wt, mapProjectErr(err)
		}
		d.audit("worktree.create", wt.ID, "allowed", wt.Path)
		d.IPC.Publish("worktrees.changed", map[string]string{"projectId": wt.ProjectID})
		return wt, nil
	})
	handle(d, "worktrees.remove", func(ctx context.Context, p struct {
		ID    string `json:"id"`
		Force bool   `json:"force"`
	}) (none, error) {
		wt, err := d.Projects.Worktrees.Get(ctx, p.ID)
		if err != nil {
			return none{}, mapProjectErr(err)
		}
		if err := d.Projects.Worktrees.Remove(ctx, p.ID, p.Force); err != nil {
			d.audit("worktree.remove", p.ID, "failed", err.Error())
			return none{}, mapProjectErr(err)
		}
		d.audit("worktree.remove", p.ID, "allowed", wt.Path)
		d.IPC.Publish("worktrees.changed", map[string]string{"projectId": wt.ProjectID})
		return none{}, nil
	})
}

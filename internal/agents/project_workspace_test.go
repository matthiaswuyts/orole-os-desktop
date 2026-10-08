package agents_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/agents/orchestrate"
	"github.com/rayorole/orole-os-desktop/internal/projects"
)

func TestFolderlessProjectTaskLifecycle(t *testing.T) {
	e := newEnv(t)
	e.m.Shutdown()
	registry := projects.New(e.db)
	p, err := registry.Create(ctx10(t), "Research", "")
	if err != nil {
		t.Fatal(err)
	}
	var authorizedDir string
	e.m = agents.NewManager(agents.Config{
		Store:    e.st,
		Host:     &agents.Host{Terminals: e.term, DataDir: e.data},
		Adapters: []agents.Adapter{agents.Claude{Bin: fakeBin}},
		ProjectDirectory: func(ctx context.Context, id, machine, dir string) (string, error) {
			return registry.TaskDirectory(ctx, id, machine, dir, filepath.Join(e.data, "project-workspaces"))
		},
		Authorize: func(_ context.Context, action, resource, _ string) error {
			if action == "agent.start" {
				authorizedDir = resource
			}
			return nil
		},
	})
	spec := agents.StartSpec{Kind: "claude", ProjectID: p.ID, Prompt: "Research the topic", Key: "folderless-task"}
	s, err := e.m.Start(ctx10(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	if s.ProjectID != p.ID || s.Dir == "" || s.Project != s.Dir || authorizedDir != "local:"+s.Dir {
		t.Fatalf("project workspace not resolved before approval: %+v, resource=%q", s, authorizedDir)
	}
	again, err := e.m.Start(ctx10(t), spec)
	if err != nil || again.ID != s.ID || again.ProjectID != p.ID || again.Dir != s.Dir {
		t.Fatalf("retry changed association: %+v, %v", again, err)
	}
	stored, ok, err := e.st.SessionByKey(spec.Key)
	if err != nil || !ok || stored.ProjectID != p.ID || stored.Dir != s.Dir {
		t.Fatalf("persisted association: %+v, %v", stored, err)
	}
	e.wait(t, s.ID, "done", status(agents.StatusDone))
	restarted, err := e.m.Restart(ctx10(t), s.ID, false, "Continue research")
	if err != nil || restarted.ProjectID != p.ID || restarted.Dir != s.Dir {
		t.Fatalf("restart changed association: %+v, %v", restarted, err)
	}
	e.wait(t, s.ID, "done after restart", status(agents.StatusDone))
	orch := orchestrate.New(e.m, nil)
	defer orch.Shutdown()
	child, err := orch.HandOff(ctx10(t), "folderless-handoff", orchestrate.HandOffParams{From: s.ID, Kind: "claude"})
	if err != nil || child.ProjectID != p.ID || child.Dir != s.Dir || child.ParentID != s.ID {
		t.Fatalf("handoff changed association: %+v, %v", child, err)
	}
	current, err := registry.Get(ctx10(t), p.ID)
	if err != nil || current.Path != "" {
		t.Fatalf("launch attached a folder to project: %+v, %v", current, err)
	}
}

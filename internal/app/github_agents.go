package app

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/projects"
)

// IssueAgentProjects lists registered local projects whose live origin matches repo.
func (g GitHub) IssueAgentProjects(ctx context.Context, repo string) (out []projects.Project, err error) {
	err = g.L.call(ctx, "github.issue.agent.projects", map[string]string{"repo": repo}, &out)
	return
}

// StartIssueAgent launches only the explicitly reviewed task, using the central manager.
func (g GitHub) StartIssueAgent(ctx context.Context, spec daemon.IssueAgentStart) (out daemon.IssueAgentRun, err error) {
	err = g.L.call(ctx, "github.issue.agent.start", spec, &out)
	return
}

// IssueAgentRuns returns durable issue-to-session/worktree/PR provenance.
func (g GitHub) IssueAgentRuns(ctx context.Context, repo string, number int) (out []daemon.IssueAgentRun, err error) {
	err = g.L.call(ctx, "github.issue.agent.runs", map[string]any{"repo": repo, "number": number}, &out)
	return
}

// PrepareIssuePR inspects a stopped session's clean committed branch; it never pushes.
func (g GitHub) PrepareIssuePR(ctx context.Context, id, title, body, base string) (out daemon.IssuePRPreparation, err error) {
	err = g.L.call(ctx, "github.issue.pr.prepare", map[string]string{"id": id, "title": title, "body": body, "base": base}, &out)
	return
}

// PublishIssuePR publishes the exact reviewed snapshot after Go policy and confirmation gates.
func (g GitHub) PublishIssuePR(ctx context.Context, id, token string, confirmed bool) (out daemon.IssueAgentRun, err error) {
	err = g.L.call(ctx, "github.issue.pr.publish", map[string]any{"id": id, "token": token, "confirmed": confirmed}, &out)
	return
}

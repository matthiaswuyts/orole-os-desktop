package daemon

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/agents"
)

// registerAgentsChat uses the same manager, policy and typed IPC decoder as the
// existing agent methods; no UI-owned queue or permission bypass is introduced.
func (d *Daemon) registerAgentsChat(mapErr func(error) error) {
	m := d.Agents
	handle(d, "agents.queuePrompt", func(ctx context.Context, p struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}) (agents.Session, error) {
		s, err := m.QueuePrompt(ctx, p.ID, p.Text)
		return s, mapErr(err)
	})
	handle(d, "agents.cancelQueuedPrompt", func(ctx context.Context, p struct {
		ID      string `json:"id"`
		QueueID string `json:"queueId"`
	}) (agents.Session, error) {
		s, err := m.CancelQueuedPrompt(ctx, p.ID, p.QueueID)
		return s, mapErr(err)
	})
	handle(d, "agents.resumeQueue", func(ctx context.Context, p struct {
		ID string `json:"id"`
	}) (agents.Session, error) {
		s, err := m.ResumeQueue(ctx, p.ID)
		return s, mapErr(err)
	})
	handle(d, "agents.answerInput", func(ctx context.Context, p struct {
		ID        string                  `json:"id"`
		RequestID string                  `json:"requestId"`
		Answers   []agents.QuestionAnswer `json:"answers"`
		Cancel    bool                    `json:"cancel"`
	}) (agents.Session, error) {
		s, err := m.AnswerInput(ctx, p.ID, p.RequestID, p.Answers, p.Cancel)
		return s, mapErr(err)
	})
	handle(d, "agents.taskFile", func(ctx context.Context, p struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}) (agents.TaskFilePreview, error) {
		file, err := m.TaskFile(ctx, p.ID, p.Path)
		return file, mapErr(err)
	})
}

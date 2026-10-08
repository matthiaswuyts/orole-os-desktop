package app

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/agents"
)

// QueuePrompt persists a follow-up in the daemon without steering a busy turn.
func (a Agents) QueuePrompt(ctx context.Context, id, text string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.queuePrompt", map[string]string{"id": id, "text": text}, &out)
	return out, err
}

// CancelQueuedPrompt removes an undelivered or uncertain queued follow-up.
func (a Agents) CancelQueuedPrompt(ctx context.Context, id, queueId string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.cancelQueuedPrompt", map[string]string{"id": id, "queueId": queueId}, &out)
	return out, err
}

// ResumeQueue explicitly releases a paused queue; it never restarts a process.
func (a Agents) ResumeQueue(ctx context.Context, id string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.resumeQueue", map[string]string{"id": id}, &out)
	return out, err
}

// AnswerInput relays typed answers; only the daemon validates pending requests.
func (a Agents) AnswerInput(ctx context.Context, id, requestId string, answers []agents.QuestionAnswer, cancel bool) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.answerInput", map[string]any{"id": id, "requestId": requestId, "answers": answers, "cancel": cancel}, &out)
	return out, err
}

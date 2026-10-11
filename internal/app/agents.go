package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/agents/orchestrate"
)

// Events for the Agent Dashboard and Review views.
var (
	// AgentUpdated fires when a session changes in a way the UI must replace wholesale (status,
	// requests, queue, review, errors). Carries the full session.
	AgentUpdated = mygo.NewEvent[agents.Session]("agents:updated")
	// AgentDelta fires for the frequent display-only changes of a live session (activity, tail,
	// last message, usage, diff counts); merge it into the held session.
	AgentDelta = mygo.NewEvent[agents.SessionDelta]("agents:delta")
	// AgentEvent carries one new entry of a session's history (message, tool use, permission request...).
	AgentEvent = mygo.NewEvent[agents.Event]("agents:event")
	// AgentRemoved fires when a session is deleted.
	AgentRemoved = mygo.NewEvent[AgentRef]("agents:removed")
	// AgentRunUpdated fires when an orchestration run (loop-until, try-N) changes.
	AgentRunUpdated = mygo.NewEvent[agents.Run]("agents:run")
)

// AgentRef identifies a session.
type AgentRef struct {
	ID string `json:"id"`
}

// forwardAgents relays agent events from the daemon; it reports whether the event was handled.
func forwardAgents(event string, data json.RawMessage) bool {
	switch event {
	case "agents.updated":
		var s agents.Session
		if json.Unmarshal(data, &s) == nil {
			AgentUpdated.Broadcast(s)
		}
	case "agents.delta":
		var d agents.SessionDelta
		if json.Unmarshal(data, &d) == nil {
			AgentDelta.Broadcast(d)
		}
	case "agents.event":
		var e agents.Event
		if json.Unmarshal(data, &e) == nil {
			AgentEvent.Broadcast(e)
		}
	case "agents.removed":
		var r AgentRef
		if json.Unmarshal(data, &r) == nil {
			AgentRemoved.Broadcast(r)
		}
	case "agents.run":
		var r agents.Run
		if json.Unmarshal(data, &r) == nil {
			AgentRunUpdated.Broadcast(r)
		}
	default:
		return false
	}
	return true
}

// Agents is the stateless bound service for daemon-owned coding-agent sessions.
// Remote sessions report lost transport honestly: it does not prove the remote
// process or its descendants stopped.
type Agents struct{ L *Link }

// Detect reports which agent CLIs are installed (cached for a minute unless refresh).
func (a Agents) Detect(ctx context.Context, refresh bool) ([]agents.Detection, error) {
	var out []agents.Detection
	err := a.L.call(ctx, "agents.detect", map[string]bool{"refresh": refresh}, &out)
	return out, err
}

// Options discovers harnesses and approved working directories on the selected box.
func (a Agents) Options(ctx context.Context, machine string) (agents.LaunchOptions, error) {
	var out agents.LaunchOptions
	err := a.L.call(ctx, "agents.options", map[string]string{"machine": machine}, &out)
	return out, err
}

// agentListPage is how many sessions one agents.list call fetches.
const agentListPage = 200

// List returns every session, newest first, fetched a page at a time so no single IPC reply
// or daemon query grows with the number of sessions.
func (a Agents) List(ctx context.Context) ([]agents.Session, error) {
	out := []agents.Session{}
	seen := map[string]bool{}
	for offset := 0; ; {
		var page []agents.Session
		if err := a.L.call(ctx, "agents.list", map[string]int{"limit": agentListPage, "offset": offset}, &page); err != nil {
			return nil, err
		}
		for _, s := range page { // a session created meanwhile shifts the next page by one
			if !seen[s.ID] {
				seen[s.ID] = true
				out = append(out, s)
			}
		}
		if len(page) < agentListPage {
			return out, nil
		}
		offset += len(page)
	}
}

// Get returns one session.
func (a Agents) Get(ctx context.Context, id string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.get", map[string]string{"id": id}, &out)
	return out, err
}

// Start launches an agent. spec.Key makes the request idempotent.
func (a Agents) Start(ctx context.Context, spec agents.StartSpec) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.start", spec, &out)
	return out, err
}

// UploadAttachment stages a file in daemon-owned storage for a subsequent task.
func (a Agents) UploadAttachment(ctx context.Context, spec agents.UploadAttachmentSpec) (agents.Attachment, error) {
	var out agents.Attachment
	err := a.L.call(ctx, "agents.uploadAttachment", spec, &out)
	return out, err
}

// DiscardAttachment removes a staged upload, never a launched session's file.
func (a Agents) DiscardAttachment(ctx context.Context, id string) error {
	return a.L.call(ctx, "agents.discardAttachment", map[string]string{"id": id}, nil)
}

// Stop ends a running agent (its record stays).
func (a Agents) Stop(ctx context.Context, id string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.stop", map[string]string{"id": id}, &out)
	return out, err
}

// Restart relaunches an ended session: resumed in its CLI session when fresh is false and the
// CLI supports it, otherwise started over from its task. prompt may be empty.
func (a Agents) Restart(ctx context.Context, id string, fresh bool, prompt string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.restart", map[string]any{"id": id, "fresh": fresh, "prompt": prompt}, &out)
	return out, err
}

// Prompt sends a message to an agent; an ended session is resumed first.
func (a Agents) Prompt(ctx context.Context, id, text string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.prompt", map[string]string{"id": id, "text": text}, &out)
	return out, err
}

// SetPermissionMode switches a task to manual, auto or yolo. confirmYolo must be true for yolo; the
// daemon enforces it and the setting that can turn YOLO off. Claude Code switches live, Codex from
// its next turn, Oh My Pi by resuming its conversation while idle; terminal and remote tasks refuse.
func (a Agents) SetPermissionMode(ctx context.Context, id, mode string, confirmYolo bool) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.setPermissionMode", map[string]any{"id": id, "mode": mode, "confirmYolo": confirmYolo}, &out)
	return out, err
}

// Rename sets a task's title. The name is the user's: automatic naming never replaces it.
func (a Agents) Rename(ctx context.Context, id, title string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.rename", map[string]any{"id": id, "title": title}, &out)
	return out, err
}

// SetPinned keeps a task above the others in its project's list.
func (a Agents) SetPinned(ctx context.Context, id string, pinned bool) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.setPinned", map[string]any{"id": id, "pinned": pinned}, &out)
	return out, err
}

// Approve answers a pending permission request. The daemon decides and audits; this only relays.
func (a Agents) Approve(ctx context.Context, id, requestID string, allow bool, reason string) error {
	return a.L.call(ctx, "agents.approve", map[string]any{"id": id, "requestId": requestID, "allow": allow, "reason": reason}, nil)
}

// Events returns history entries with id greater than after (0 = from the start).
func (a Agents) Events(ctx context.Context, id string, after int64, limit int) ([]agents.Event, error) {
	var out []agents.Event
	err := a.L.call(ctx, "agents.events", map[string]any{"id": id, "after": after, "limit": limit}, &out)
	return out, err
}

// Transcript returns the newest `limit` conversation entries (prompts, messages, tool use,
// permission requests and decisions), oldest first.
func (a Agents) Transcript(ctx context.Context, id string, limit int) ([]agents.Event, error) {
	var out []agents.Event
	err := a.L.call(ctx, "agents.transcript", map[string]any{"id": id, "limit": limit}, &out)
	return out, err
}

// Remove deletes a session that is not running.
func (a Agents) Remove(ctx context.Context, id string) error {
	return a.L.call(ctx, "agents.remove", map[string]string{"id": id}, nil)
}

// Diff returns the changes a session made relative to the commit it started from.
func (a Agents) Diff(ctx context.Context, id string) (agents.ReviewDiff, error) {
	var out agents.ReviewDiff
	err := a.L.call(ctx, "agents.diff", map[string]string{"id": id}, &out)
	return out, err
}

// Review records a decision: "approve", "send-back" (note is the feedback) or "discard".
func (a Agents) Review(ctx context.Context, id, decision, note string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.review", map[string]string{"id": id, "decision": decision, "note": note}, &out)
	return out, err
}

// Check runs a command in the session's directory in the background; the result arrives as
// the session's checkState (passing/failing).
func (a Agents) Check(ctx context.Context, id, command string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.check", map[string]string{"id": id, "command": command}, &out)
	return out, err
}

// HandOff starts a second agent (kind) in the session's directory, briefed with its summary.
func (a Agents) HandOff(ctx context.Context, p orchestrate.HandOffParams, key string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.handoff", handOffReq{p, key}, &out)
	return out, err
}

// ReviewWith starts a second agent that reviews the session's diff.
func (a Agents) ReviewWith(ctx context.Context, p orchestrate.ReviewParams, key string) (agents.Session, error) {
	var out agents.Session
	err := a.L.call(ctx, "agents.reviewWith", reviewReq{p, key}, &out)
	return out, err
}

// LoopUntil runs a command until it passes, asking the agent to fix failures (up to maxRounds).
func (a Agents) LoopUntil(ctx context.Context, p orchestrate.LoopParams, key string) (agents.Run, error) {
	var out agents.Run
	err := a.L.call(ctx, "agents.loopUntil", loopReq{p, key}, &out)
	return out, err
}

// TryN starts N agents on the same task in N fresh worktrees.
func (a Agents) TryN(ctx context.Context, p orchestrate.TryParams, key string) (orchestrate.TryResult, error) {
	var out orchestrate.TryResult
	err := a.L.call(ctx, "agents.tryN", tryReq{p, key}, &out)
	return out, err
}

// Runs lists recent orchestration runs.
func (a Agents) Runs(ctx context.Context) ([]agents.Run, error) {
	var out []agents.Run
	err := a.L.call(ctx, "agents.runs", struct{}{}, &out)
	return out, err
}

// CancelRun stops a running orchestration run.
func (a Agents) CancelRun(ctx context.Context, id string) error {
	return a.L.call(ctx, "agents.cancelRun", map[string]string{"id": id}, nil)
}

// Request shapes: the orchestration params are flattened next to the idempotency key.
type handOffReq struct {
	orchestrate.HandOffParams
	Key string `json:"key"`
}
type reviewReq struct {
	orchestrate.ReviewParams
	Key string `json:"key"`
}
type loopReq struct {
	orchestrate.LoopParams
	Key string `json:"key"`
}
type tryReq struct {
	orchestrate.TryParams
	Key string `json:"key"`
}

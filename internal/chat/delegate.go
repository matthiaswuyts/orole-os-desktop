package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Ask implements Delegator: it hands question to the callee as a real turn of the callee's own
// conversation. The question is stored there as a user message authored by the caller
// ("bot:<callerID>"); the callee answers through its normal queue (one run per bot, behind anything
// it is already doing) with its own instructions, memory and tools, and its reply is stored and
// streamed like any other, so the user can watch the work in the callee's chat. Ask returns the
// callee's final text.
//
// When ctx ends first (the asking run was stopped, or its time ran out) the delegated turn is
// withdrawn: dropped from the callee's queue, or canceled if it is the active turn. No other turn of
// the callee is touched. Ask then returns ctx.Err(), so a timeout is reported as one.
//
// Only executive assistants may delegate: the caller must be flagged Executive in its stored
// configuration, and chains are depth-limited. from.Chain must hold the delegating run's chain
// plus the caller.
func (m *Manager) Ask(ctx context.Context, from RunContext, calleeID, question string) (string, error) {
	if from.BotID == "" {
		return "", errors.New("delegation is not available in previews")
	}
	if from.Depth >= maxDelegationDepth {
		return "", fmt.Errorf("delegation is limited to %d levels", maxDelegationDepth)
	}
	if strings.TrimSpace(question) == "" || len(question) > MaxMessageTextLen || !utf8.ValidString(question) {
		return "", errors.New("the question is empty or too long")
	}
	caller, err := m.store.getBot(ctx, from.BotID, false)
	if err != nil {
		return "", err
	}
	if calleeID == caller.ID || !caller.Config.Executive {
		return "", fmt.Errorf("%s is not allowed to ask that bot", caller.Name)
	}
	if slices.Contains(from.Chain, calleeID) {
		return "", errors.New("asking that bot would create a delegation cycle")
	}
	callee, err := m.store.getBot(ctx, calleeID, false)
	if err != nil {
		return "", fmt.Errorf("the bot to ask no longer exists")
	}

	result := make(chan handoffResult, 1)
	t := turn{
		depth:  from.Depth + 1,
		chain:  slices.Clone(from.Chain),
		result: result,
		extra: fmt.Sprintf("You are carrying out a task another bot (%q) handed off on the user's behalf. "+
			"Do the work, then finish with a concise, self-contained result; you cannot ask follow-up questions.", caller.Name),
	}
	msg, err := m.submitDelegation(ctx, caller, callee, strings.TrimRight(question, " \t\r\n"), t)
	if err != nil {
		return "", err
	}
	defer m.stopWaiting(caller.ID, callee.ID)

	select {
	case r := <-result:
		return delegationAnswer(ctx, callee, r)
	case <-ctx.Done():
		m.abandonDelegated(context.WithoutCancel(ctx), callee.ID, msg.ID)
		return "", ctx.Err()
	}
}

// submitDelegation stores the question in the callee's conversation and queues (or starts) its turn.
// It refuses to queue behind a callee that is itself blocked, directly or through other
// delegations, waiting for the caller: neither run could ever finish.
//
// The wait edge is recorded in the same critical section as the cycle check, before the message is
// stored, so two asks that would deadlock each other cannot both pass the check; the edge is taken
// back when storing fails. The callee's bot lock serialises the store and queue step.
func (m *Manager) submitDelegation(ctx context.Context, caller, callee Bot, question string, t turn) (Message, error) {
	unlock := m.lockBot(callee.ID)
	defer unlock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Message{}, ErrClosed
	}
	if m.waitsOnLocked(callee.ID, caller.ID) {
		m.mu.Unlock()
		return Message{}, fmt.Errorf("%s is waiting for %s to finish, so asking it now would deadlock", callee.Name, caller.Name)
	}
	m.waits[caller.ID] = append(m.waits[caller.ID], callee.ID)
	m.mu.Unlock()
	msg, err := m.submit(ctx, Message{BotID: callee.ID, Role: "user", Author: BotPrefix + caller.ID}, question, nil, t)
	if err != nil {
		m.stopWaiting(caller.ID, callee.ID)
		return Message{}, err
	}
	return msg, nil
}

// waitsOnLocked reports whether bot id is blocked waiting for the answer of target, directly or
// through a chain of delegations. The caller holds m.mu.
func (m *Manager) waitsOnLocked(id, target string) bool {
	seen := map[string]bool{}
	for stack := []string{id}; len(stack) > 0; {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == target {
			return true
		}
		if !seen[cur] {
			seen[cur] = true
			stack = append(stack, m.waits[cur]...)
		}
	}
	return false
}

// stopWaiting records that callerID no longer waits for calleeID.
func (m *Manager) stopWaiting(callerID, calleeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.waits[callerID]
	if i := slices.Index(w, calleeID); i >= 0 {
		w = slices.Delete(w, i, i+1)
	}
	if len(w) == 0 {
		delete(m.waits, callerID)
	} else {
		m.waits[callerID] = w
	}
}

// delegationAnswer turns the outcome of a delegated turn into the value Ask returns.
func delegationAnswer(ctx context.Context, callee Bot, r handoffResult) (string, error) {
	switch r.status {
	case StatusComplete:
		out := strings.TrimSpace(r.text)
		if out == "" {
			return "", fmt.Errorf("%s did not answer", callee.Name)
		}
		return out, nil
	case StatusCanceled, StatusIncomplete:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if r.errText != "" {
			return "", errors.New(r.errText)
		}
		return "", fmt.Errorf("%s was stopped before it finished", callee.Name)
	default:
		if r.errText == "" {
			return "", fmt.Errorf("%s failed", callee.Name)
		}
		return "", errors.New(r.errText)
	}
}

var _ Delegator = (*Manager)(nil)

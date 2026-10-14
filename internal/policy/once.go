package policy

import (
	"context"
	"errors"
)

// RequireOnce honors stored denials but never treats stored allows as consent
// to disclose a content snapshot. Approval cannot be remembered or reused.
func (e *Engine) RequireOnce(ctx context.Context, req Request) error {
	req = normalize(req)
	req.Effect = EffectDestructive
	ev, err := e.Decide(ctx, req)
	if err != nil {
		return err
	}
	if ev.Decision == Deny {
		e.audit(req, "denied", ev.Source)
		return ErrDenied
	}
	decision, err := e.await(ctx, req, Evaluation{Decision: Ask, Source: "one-shot", Reason: "Explicit approval is required for this exact content disclosure; stored allows do not apply."})
	if err != nil {
		return err
	}
	if decision != Allow {
		return ErrDenied
	}
	return nil
}

type noApprovalsKey struct{}

// ErrApprovalUnavailable is returned instead of asking when nobody can answer an approval in time (a phone call).
var ErrApprovalUnavailable = errors.New("Needs your approval in the app — not available on a call.")

// WithoutApprovals marks ctx as unable to wait for the user: a decision of "ask" fails at once with
// ErrApprovalUnavailable instead of parking the request. Phone calls use it, because a turn cannot wait minutes.
func WithoutApprovals(ctx context.Context) context.Context {
	return context.WithValue(ctx, noApprovalsKey{}, true)
}

// ApprovalsUnavailable reports whether ctx was marked by WithoutApprovals.
func ApprovalsUnavailable(ctx context.Context) bool {
	v, _ := ctx.Value(noApprovalsKey{}).(bool)
	return v
}

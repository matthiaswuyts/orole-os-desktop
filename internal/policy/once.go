package policy

import "context"

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

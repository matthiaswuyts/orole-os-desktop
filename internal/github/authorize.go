package github

import (
	"context"
	"errors"
)

// ErrConfirmationRequired is returned by [RequireConfirmation] when the caller
// did not mark the request as explicitly confirmed by the user.
var ErrConfirmationRequired = errors.New("github: this action needs explicit user confirmation")

type confirmedKey struct{}

// WithConfirmation marks ctx as carrying an explicit user confirmation (set by
// trusted daemon code from the IPC `confirmed` flag, never by agents).
func WithConfirmation(ctx context.Context) context.Context {
	return context.WithValue(ctx, confirmedKey{}, true)
}

// Confirmed reports whether ctx carries a user confirmation.
func Confirmed(ctx context.Context) bool {
	v, _ := ctx.Value(confirmedKey{}).(bool)
	return v
}

// RequireConfirmation returns an Authorize hook that allows an action only when
// ctx carries a confirmation. audit (optional) is told every decision; outcome
// is "allowed" or "denied". This is the interim gate until internal/policy exists.
func RequireConfirmation(audit func(action, detail, outcome string)) Authorize {
	return func(ctx context.Context, action, detail string) error {
		if !Confirmed(ctx) {
			if audit != nil {
				audit(action, detail, "denied")
			}
			return ErrConfirmationRequired
		}
		if audit != nil {
			audit(action, detail, "allowed")
		}
		return nil
	}
}

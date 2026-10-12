package agents

import "context"

type actorKey struct{}

// WithActor marks ctx as acting for a non-user principal ("bot:<id>"). Authorize hooks use
// it as the policy actor, Start records it as the session's Owner, and YOLO is refused.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFrom returns the principal ctx acts for, "" for the user.
func ActorFrom(ctx context.Context) string {
	a, _ := ctx.Value(actorKey{}).(string)
	return a
}

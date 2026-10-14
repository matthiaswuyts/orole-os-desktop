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

type callKey struct{}

// WithCall marks ctx as acting on the phone call callID (a bot starting a task while it talks to the owner). Start
// records it as the session's CallID so the task's reports reach that call's thread. "" leaves ctx unchanged.
func WithCall(ctx context.Context, callID string) context.Context {
	if callID == "" {
		return ctx
	}
	return context.WithValue(ctx, callKey{}, callID)
}

// CallFrom returns the phone call ctx acts on, "" for none.
func CallFrom(ctx context.Context) string {
	c, _ := ctx.Value(callKey{}).(string)
	return c
}

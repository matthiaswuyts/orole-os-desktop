package automations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/untrusted"
)

// authorizationError is terminal for this step, even with a repeatable strategy:
// denying an approval must not immediately prompt again through automatic retry.
type authorizationError struct{ cause error }

func (e *authorizationError) Error() string { return e.cause.Error() }
func (e *authorizationError) Unwrap() error { return e.cause }

func nativeEffect(kind string) bool {
	return kind == "command.run" || kind == "test.run" || kind == "http.request"
}

func (e *Engine) authorizeCommand(ctx context.Context, actor, action string, command CommandRequest) error {
	// Test runners and general interpreters execute arbitrary user code. Neither
	// the executable allowlist nor the name "test" establishes read-only behavior.
	return e.requireEffect(ctx, policy.Request{
		Actor: actor, Action: action, Resource: "command/" + command.Executable,
		Effect: policy.EffectDestructive,
	}, map[string]any{
		"executable": command.Executable, "argv": command.Args,
		"dir": command.Dir, "env": command.Env,
	})
}

func (e *Engine) authorizeHTTP(ctx context.Context, actor string, request httpRequestSnapshot) error {
	// JSON replaces invalid UTF-8. Reject it rather than authorize text that
	// differs from the exact bytes sent on the wire.
	if !utf8.ValidString(request.Method) || !utf8.ValidString(request.URL) || !utf8.ValidString(request.Body) {
		return &authorizationError{cause: errors.New("HTTP authority detail cannot be displayed in full")}
	}
	for name, values := range request.Headers {
		if !utf8.ValidString(name) || e.redact.Text(name) != name {
			return &authorizationError{cause: errors.New("HTTP authority header cannot be displayed in full")}
		}
		for _, value := range values {
			if !utf8.ValidString(value) {
				return &authorizationError{cause: errors.New("HTTP authority detail cannot be displayed in full")}
			}
		}
	}
	// URL escaping must not turn a known credential into visible approval text.
	decoded, err := url.QueryUnescape(request.URL)
	if err != nil || e.redact.Text(decoded) != decoded {
		return &authorizationError{cause: errors.New("HTTP authority target contains a secret or invalid escaping")}
	}
	effect := policy.EffectDestructive
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		effect = policy.EffectRead
	}
	return e.requireEffect(ctx, policy.Request{
		Actor: actor, Action: "http.request", Resource: "http/" + request.Method + " " + request.URL,
		Effect: effect,
	}, map[string]any{"method": request.Method, "url": request.URL, "headers": request.Headers, "body": request.Body})
}

func (e *Engine) requireEffect(ctx context.Context, request policy.Request, detail any) error {
	fail := func(err error) error { return &authorizationError{cause: err} }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if e.authorize == nil {
		return fail(errors.New("automation policy authorizer is unavailable"))
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fail(errors.New("cannot describe automation effect for authorization"))
	}
	// Native steps have no credential-reference resolver. Never show or approve
	// a concealed executable/argument/target: integrations must reference their
	// own credential identifiers instead of passing secret values here.
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return fail(errors.New("cannot describe automation effect for authorization"))
	}
	redacted, err := json.Marshal(e.redact.value(document))
	if err != nil || string(redacted) != string(encoded) || e.redact.Text(request.Resource) != request.Resource || e.redact.Text(request.Actor) != request.Actor {
		return fail(errors.New("automation authority detail contains a secret; use integration credential identifiers, not secret values"))
	}
	request.Detail = string(encoded)
	// Match the shared policy's display bounds and sanitizer. Refuse loss of
	// authority context rather than asking the user to approve a truncated or
	// invisibly altered command/target. These bounds may be tightened safely.
	if untrusted.Text(request.Detail, 2000) != request.Detail ||
		untrusted.Line(request.Resource, 512) != request.Resource ||
		untrusted.Line(request.Actor, 128) != request.Actor {
		return fail(errors.New("automation authority detail cannot be displayed in full"))
	}
	if err := e.authorize(ctx, request); err != nil {
		// An adapter error can contain sensitive backend context. The daemon's
		// shared policy already audits its decision; persist only scrubbed text.
		return fail(fmt.Errorf("automation authorization failed: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return nil
}

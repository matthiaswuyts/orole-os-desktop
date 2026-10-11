package llm

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

// RetryPolicy governs how DoRetry re-sends a request whose response was a transient failure.
type RetryPolicy struct {
	// Attempts is the total number of tries including the first; values <= 1 disable retrying.
	Attempts int
	// Base is the backoff before the second try; it doubles per attempt up to Cap. Each wait is
	// jittered into [d/2, d] so concurrent runs do not retry in lockstep.
	Base, Cap time.Duration
	// MaxRetryAfter is the longest provider-requested Retry-After that is waited out. A longer
	// request is returned to the caller as is, so the user sees "try again in N seconds".
	MaxRetryAfter time.Duration
}

// DefaultRetry is used when the request context carries no policy.
var DefaultRetry = RetryPolicy{Attempts: 3, Base: 500 * time.Millisecond, Cap: 8 * time.Second, MaxRetryAfter: 5 * time.Second}

// NoRetry sends a request exactly once.
var NoRetry = RetryPolicy{Attempts: 1}

type retryKey struct{}

// WithRetry returns a context whose requests use p instead of DefaultRetry.
func WithRetry(ctx context.Context, p RetryPolicy) context.Context {
	return context.WithValue(ctx, retryKey{}, p)
}

func retryPolicy(ctx context.Context) RetryPolicy {
	if p, ok := ctx.Value(retryKey{}).(RetryPolicy); ok {
		return p
	}
	return DefaultRetry
}

// retryable reports whether err is a transient HTTP failure: 429 or a gateway/server error. Quota,
// auth, context-length and every other 4xx are final. Transport failures are not retried: the
// request may already have reached the provider.
func retryable(err error) (*Error, bool) {
	var le *Error
	if !errors.As(err, &le) {
		return nil, false
	}
	switch le.Status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return le, le.Code == ErrRateLimit || le.Code == ErrServer
	}
	return le, false
}

// wait returns how long to sleep before try number attempt+1, or false to give up.
func (p RetryPolicy) wait(attempt int, le *Error) (time.Duration, bool) {
	if attempt >= p.Attempts {
		return 0, false
	}
	if le.RetryAfter > p.MaxRetryAfter {
		return 0, false
	}
	d := p.Base << min(attempt-1, 16)
	if p.Cap > 0 && d > p.Cap {
		d = p.Cap
	}
	if d > 1 {
		d = d/2 + rand.N(d/2+1)
	}
	return max(d, le.RetryAfter), true
}

// DoRetry is Do for opening a streaming request: a 429 or 5xx answer is retried with jittered
// exponential backoff, waiting out a short Retry-After. It only ever retries while no response body
// has been handed to the caller, so a stream that has started delivering data is never replayed.
// The request body must be replayable (http.NewRequest sets GetBody for bytes and strings).
func DoRetry(doer HTTPDoer, req *http.Request, secrets ...string) (*http.Response, error) {
	ctx := req.Context()
	p := retryPolicy(ctx)
	for attempt := 1; ; attempt++ {
		resp, err := Do(doer, req, secrets...)
		if err == nil {
			return resp, nil
		}
		le, ok := retryable(err)
		if !ok || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
			return nil, err
		}
		d, ok := p.wait(attempt, le)
		if !ok {
			return nil, err
		}
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, NetworkError(ctx.Err())
		case <-t.C:
		}
		next := req.Clone(ctx)
		if req.GetBody != nil {
			body, berr := req.GetBody()
			if berr != nil {
				return nil, err
			}
			next.Body = body
		}
		req = next
	}
}

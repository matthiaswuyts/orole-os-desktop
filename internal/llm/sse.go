package llm

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// DefaultMaxSSELine bounds one SSE line. Provider chunks are small; the largest are
// Responses API completion events that repeat the whole output.
const DefaultMaxSSELine = 4 << 20

// DefaultSSEIdle is how long a stream may stay completely silent before ReadSSE gives up.
// Providers send keep-alive comments or deltas far more often; a longer silence means a dead
// connection that TCP would take many minutes to notice.
const DefaultSSEIdle = 90 * time.Second

type sseIdleKey struct{}

// WithSSEIdle returns a context whose streams use idle instead of DefaultSSEIdle (<= 0 disables the watchdog).
func WithSSEIdle(ctx context.Context, idle time.Duration) context.Context {
	return context.WithValue(ctx, sseIdleKey{}, idle)
}

// ErrSSEDone is returned by an SSE callback to end the stream successfully.
var ErrSSEDone = errors.New("sse: done")

// SSEEvent is one dispatched server-sent event. Data is only valid during the callback.
type SSEEvent struct {
	Event string
	Data  []byte
}

// ReadSSE parses a text/event-stream body and calls fn per event. Comment lines and
// unknown fields are ignored. Cancelling ctx closes body, which unblocks the read, so
// no goroutine can outlive the context. ReadSSE returns nil at EOF or when fn returns
// ErrSSEDone, ctx.Err() on cancellation, and fn's or the scanner's error otherwise
// (bufio.ErrTooLong when a line exceeds maxLine). A stream that delivers no byte for
// DefaultSSEIdle (see WithSSEIdle) is closed and reported as an ErrNetwork *Error.
func ReadSSE(ctx context.Context, body io.ReadCloser, maxLine int, fn func(SSEEvent) error) error {
	idle := DefaultSSEIdle
	if v, ok := ctx.Value(sseIdleKey{}).(time.Duration); ok {
		idle = v
	}
	return ReadSSEIdle(ctx, body, maxLine, idle, fn)
}

// ReadSSEIdle is ReadSSE with an explicit idle limit (<= 0 disables the watchdog). The limit
// applies to waiting for the server only: time spent inside fn does not count, so a slow
// consumer never makes a healthy stream look dead.
func ReadSSEIdle(ctx context.Context, body io.ReadCloser, maxLine int, idle time.Duration, fn func(SSEEvent) error) error {
	if maxLine <= 0 {
		maxLine = DefaultMaxSSELine
	}
	stop := context.AfterFunc(ctx, func() { body.Close() })
	defer stop()

	var stalled atomic.Bool
	var src io.Reader = body
	if idle > 0 {
		t := time.AfterFunc(idle, func() {
			stalled.Store(true)
			body.Close()
		})
		t.Stop()
		defer t.Stop()
		src = &idleReader{r: body, t: t, d: idle}
	}
	sc := bufio.NewScanner(src)
	// The initial buffer must not exceed maxLine: Scanner only enforces the limit once the buffer is full.
	sc.Buffer(make([]byte, 0, min(32<<10, maxLine)), maxLine)
	var (
		event   string
		data    []byte
		hasData bool
	)
	dispatch := func() error {
		if !hasData {
			event = ""
			return nil
		}
		ev := SSEEvent{Event: event, Data: data}
		event, data, hasData = "", data[:0], false
		return fn(ev)
	}
	finish := func(err error) error {
		if errors.Is(err, ErrSSEDone) {
			return nil
		}
		return err
	}
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			if err := dispatch(); err != nil {
				return finish(err)
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		name, value, _ := bytes.Cut(line, []byte{':'})
		value = bytes.TrimPrefix(value, []byte{' '})
		switch string(name) {
		case "data":
			if hasData {
				data = append(data, '\n')
			}
			data = append(data, value...)
			hasData = true
		case "event":
			event = string(value)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sc.Err(); err != nil {
		if stalled.Load() {
			return &Error{Code: ErrNetwork, Message: fmt.Sprintf("the provider stopped responding: no data for %s", idle.Round(time.Second))}
		}
		return err
	}
	// Some servers omit the final blank line; deliver what was received.
	return finish(dispatch())
}

// idleReader arms t around every Read of the underlying body, so only time spent waiting for the
// server counts as idle.
type idleReader struct {
	r io.Reader
	t *time.Timer
	d time.Duration
}

func (r *idleReader) Read(p []byte) (int, error) {
	r.t.Reset(r.d)
	n, err := r.r.Read(p)
	r.t.Stop()
	return n, err
}

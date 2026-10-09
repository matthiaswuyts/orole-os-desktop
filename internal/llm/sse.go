package llm

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
)

// DefaultMaxSSELine bounds one SSE line. Provider chunks are small; the largest are
// Responses API completion events that repeat the whole output.
const DefaultMaxSSELine = 4 << 20

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
// (bufio.ErrTooLong when a line exceeds maxLine).
func ReadSSE(ctx context.Context, body io.ReadCloser, maxLine int, fn func(SSEEvent) error) error {
	if maxLine <= 0 {
		maxLine = DefaultMaxSSELine
	}
	stop := context.AfterFunc(ctx, func() { body.Close() })
	defer stop()

	sc := bufio.NewScanner(body)
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
		return err
	}
	// Some servers omit the final blank line; deliver what was received.
	return finish(dispatch())
}

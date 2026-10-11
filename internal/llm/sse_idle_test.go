package llm

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReadSSEIdleCancelsAStalledStream(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	defer pw.Close()
	go pw.Write([]byte("data: one\n\n")) // then silence: the connection is dead but never closes
	ctx := context.Background()
	var got []string
	start := time.Now()
	err := ReadSSEIdle(ctx, pr, 0, 100*time.Millisecond, func(e SSEEvent) error { got = append(got, string(e.Data)); return nil })
	var le *Error
	if !errors.As(err, &le) || le.Code != ErrNetwork || !strings.Contains(le.Message, "no data for") {
		t.Fatalf("err = %v, want an ErrNetwork stall error", err)
	}
	if ctx.Err() != nil {
		t.Fatal("the caller's context must stay live: only the stream is closed")
	}
	if len(got) != 1 || got[0] != "one" {
		t.Fatalf("events = %v", got)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v to give up on a 100ms idle limit", d)
	}
}

func TestReadSSEIdleKeepAlivesAndSlowConsumersAreNotStalls(t *testing.T) {
	t.Parallel()
	t.Run("keep-alive comments reset the timer", func(t *testing.T) {
		t.Parallel()
		pr, pw := io.Pipe()
		go func() {
			for range 8 {
				pw.Write([]byte(": keep-alive\n\n"))
				time.Sleep(40 * time.Millisecond)
			}
			pw.Write([]byte("data: done\n\n"))
			pw.Close()
		}()
		var n int
		if err := ReadSSEIdle(context.Background(), pr, 0, 150*time.Millisecond, func(SSEEvent) error { n++; return nil }); err != nil || n != 1 {
			t.Fatalf("err %v events %d: total time exceeded the idle limit but data kept flowing", err, n)
		}
	})
	t.Run("time inside the callback does not count", func(t *testing.T) {
		t.Parallel()
		pr, pw := io.Pipe()
		go func() {
			for _, ev := range []string{"data: 1\n\n", "data: 2\n\n", "data: 3\n\n"} {
				pw.Write([]byte(ev)) // blocks until the reader asks for more, like a socket under backpressure
			}
			pw.Close()
		}()
		body := io.ReadCloser(pr)
		var n int
		err := ReadSSEIdle(context.Background(), body, 0, 50*time.Millisecond, func(SSEEvent) error {
			n++
			time.Sleep(120 * time.Millisecond)
			return nil
		})
		if err != nil || n != 3 {
			t.Fatalf("err %v events %d", err, n)
		}
	})
}

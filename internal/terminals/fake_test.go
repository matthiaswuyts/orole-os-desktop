package terminals_test

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

// fakeProc is a scripted pty: it returns reads chunks of chunkSize bytes as fast
// as the consumer asks, then blocks until it is closed or told to exit.
type fakeProc struct {
	size      int
	cycle     []byte // patternByte repeated for size+251 bytes; the pattern has period 251
	off       int    // stream offset of the next byte; only the pump's reader touches it
	remaining atomic.Int64
	exit      chan int
	closed    chan struct{}
	once      sync.Once
	pid       int
}

// patternByte is the byte a fakeProc emits at stream offset off.
func patternByte(off int) byte { return byte(off % 251) }

func newFakeProc(chunkSize, reads int) *fakeProc {
	cycle := make([]byte, chunkSize+251)
	for i := range cycle {
		cycle[i] = patternByte(i)
	}
	p := &fakeProc{size: chunkSize, cycle: cycle, exit: make(chan int, 1), closed: make(chan struct{}), pid: 4242}
	p.remaining.Store(int64(reads))
	return p
}

func (p *fakeProc) Read(b []byte) (int, error) {
	if p.remaining.Add(-1) >= 0 {
		n := min(len(b), p.size)
		k := p.off % 251
		copy(b[:n], p.cycle[k:k+n])
		p.off += n
		return n, nil
	}
	<-p.closed
	return 0, io.EOF
}
func (p *fakeProc) Write(b []byte) (int, error) { return len(b), nil }
func (p *fakeProc) Resize(int, int) error       { return nil }
func (p *fakeProc) Pid() int                    { return p.pid }
func (p *fakeProc) Close() error                { p.once.Do(func() { close(p.closed) }); return nil }
func (p *fakeProc) Wait(ctx context.Context) (int, error) {
	select {
	case c := <-p.exit:
		return c, nil
	case <-p.closed:
		return -1, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

type fakeBackend struct {
	mu    sync.Mutex
	procs []*fakeProc
	next  func() *fakeProc
}

func (b *fakeBackend) Start(terminals.StartSpec) (terminals.Process, error) {
	p := b.next()
	b.mu.Lock()
	b.procs = append(b.procs, p)
	b.mu.Unlock()
	return p, nil
}
func (b *fakeBackend) Shells() []terminals.Shell {
	return []terminals.Shell{{ID: "fake", Name: "fake", Command: "fake"}}
}

// countSink records what the daemon would publish.
type countSink struct {
	mu      sync.Mutex
	events  int
	bytes   int64
	maxData int
	lastSeq uint64
	gaps    int // events whose start offset is past the previous event's end
	changed []terminals.Info
	removed []string
}

func (s *countSink) Output(_ string, seq uint64, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if start := seq - uint64(len(data)); start > s.lastSeq {
		s.gaps++
	}
	s.events++
	s.bytes += int64(len(data))
	if len(data) > s.maxData {
		s.maxData = len(data)
	}
	s.lastSeq = seq
}
func (s *countSink) Changed(i terminals.Info) {
	s.mu.Lock()
	s.changed = append(s.changed, i)
	s.mu.Unlock()
}
func (s *countSink) Removed(id string) { s.mu.Lock(); s.removed = append(s.removed, id); s.mu.Unlock() }
func (s *countSink) changedExited() []terminals.Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []terminals.Info
	for _, i := range s.changed {
		if i.State == terminals.Exited {
			out = append(out, i)
		}
	}
	return out
}
func (s *countSink) snapshot() (events int, bytes int64, maxData int, lastSeq uint64, gaps int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events, s.bytes, s.maxData, s.lastSeq, s.gaps
}

func eventually(timeout time.Duration, f func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return f()
}

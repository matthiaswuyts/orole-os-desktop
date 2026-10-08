//go:build windows

package performance

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

type sink struct {
	mu  sync.Mutex
	buf strings.Builder
	ch  chan struct{}
}

func (s *sink) Output(_ string, _ uint64, d []byte) {
	s.mu.Lock()
	s.buf.Write(d)
	s.mu.Unlock()
	select {
	case s.ch <- struct{}{}:
	default:
	}
}
func (s *sink) Changed(terminals.Info) {}
func (s *sink) has(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Contains(s.buf.String(), sub)
}

// Time from Write(keystrokes) to the pty echoing them back through ConPTY.
// This is the floor of perceived typing latency inside the daemon; it excludes
// IPC and xterm.js rendering, which the UI adds.
func BenchmarkConPTYEcho(b *testing.B) {
	s := &sink{ch: make(chan struct{}, 1)}
	m := terminals.NewManager(terminals.NewBackend(), s)
	defer m.Shutdown()
	info, err := m.Create(terminals.CreateOptions{Shell: "cmd", Cols: 120, Rows: 30})
	if err != nil {
		b.Skip("no cmd terminal:", err)
	}
	// Wait for the prompt so we measure echo, not shell start-up.
	deadline := time.Now().Add(10 * time.Second)
	for !s.has(">") {
		if time.Now().After(deadline) {
			b.Fatal("shell never printed a prompt")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var total time.Duration
	i := 0
	for b.Loop() {
		i++
		tok := "q" + strings.Repeat("z", i%7) + string(rune('a'+i%26))
		start := time.Now()
		if err := m.Write(info.ID, []byte(tok)); err != nil {
			b.Fatal(err)
		}
		for !s.has(tok) {
			select {
			case <-s.ch:
			case <-time.After(5 * time.Second):
				b.Fatal("echo timeout for ", tok)
			}
		}
		total += time.Since(start)
		// Erase the typed token so the line buffer stays short.
		m.Write(info.ID, []byte(strings.Repeat("\b", len(tok))))
		s.mu.Lock()
		s.buf.Reset()
		s.mu.Unlock()
		time.Sleep(15 * time.Millisecond) // let the erase echo drain
		s.mu.Lock()
		s.buf.Reset()
		s.mu.Unlock()
	}
	b.ReportMetric(float64(total.Microseconds())/float64(i), "echo-µs/op")
}

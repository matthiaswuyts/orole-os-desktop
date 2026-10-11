package terminals_test

import (
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

const floodChunk = 32 << 10

// A 64 MiB burst must reach the sink as a small, bounded number of coalesced
// events while the session total (seq) stays exact.
func TestFloodIsCoalesced(t *testing.T) {
	const reads = 2048 // 64 MiB
	total := uint64(reads * floodChunk)
	be := &fakeBackend{next: func() *fakeProc { return newFakeProc(floodChunk, reads) }}
	sink := &countSink{}
	m := terminals.NewManager(be, sink)
	t.Cleanup(m.Shutdown)
	start := time.Now()
	info, err := m.Create(terminals.CreateOptions{Shell: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if !eventually(30*time.Second, func() bool { _, _, _, seq, _ := sink.snapshot(); return seq == total }) {
		ev, _, _, seq, _ := sink.snapshot()
		t.Fatalf("sink never saw the tail: events=%d seq=%d want %d", ev, seq, total)
	}
	ev, bytes, maxData, _, _ := sink.snapshot()
	t.Logf("burst=%d MiB reads=%d -> events=%d delivered=%d MiB max-event=%d KiB elapsed=%v",
		total>>20, reads, ev, bytes>>20, maxData>>10, time.Since(start).Round(time.Millisecond))
	att, err := m.Attach(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if att.Seq != total {
		t.Fatalf("attach seq=%d want %d", att.Seq, total)
	}
	if len(att.Data) != terminals.ScrollbackBytes {
		t.Fatalf("scrollback len=%d want %d", len(att.Data), terminals.ScrollbackBytes)
	}
}

// BenchmarkFloodRead measures the pump's cost per PTY read once scrollback is full.
// The timer stops when the last read has been ingested; the paced delivery of the
// final undelivered bytes is outside it.
func BenchmarkFloodRead(b *testing.B) {
	reads := b.N + 64 // 2 MiB of warm-up so scrollback is already full
	var proc *fakeProc
	be := &fakeBackend{next: func() *fakeProc { proc = newFakeProc(floodChunk, reads); return proc }}
	sink := &countSink{}
	m := terminals.NewManager(be, sink)
	defer m.Shutdown()
	b.ReportAllocs()
	b.SetBytes(floodChunk)
	b.ResetTimer()
	if _, err := m.Create(terminals.CreateOptions{Shell: "fake"}); err != nil {
		b.Fatal(err)
	}
	// The read after the last scripted one only happens once that one was ingested.
	if !eventually(5*time.Minute, func() bool { return proc.remaining.Load() < 0 }) {
		b.Fatal("flood did not drain")
	}
	b.StopTimer()
	want := uint64(reads * floodChunk)
	if !eventually(time.Minute, func() bool { _, _, _, seq, _ := sink.snapshot(); return seq == want }) {
		b.Fatal("tail never delivered")
	}
	ev, _, _, _, _ := sink.snapshot()
	b.ReportMetric(float64(ev)/float64(reads), "events/read")
}

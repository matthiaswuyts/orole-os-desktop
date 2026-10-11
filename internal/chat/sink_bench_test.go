package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const benchReply = 100 << 10

func benchText() string {
	return strings.Repeat("The quick brown fox jumps over the lazy dog. ", benchReply/45+1)[:benchReply]
}

// replyChunks splits a 100 KiB reply into 170-byte provider chunks, one per publish interval:
// about 30 seconds of streaming at 50 ms per tick.
func replyChunks() []string {
	text := benchText()
	var out []string
	for len(text) > 0 {
		n := min(170, len(text))
		out = append(out, text[:n])
		text = text[n:]
	}
	return out
}

// BenchmarkSinkStream100KB streams a 100 KiB reply through the sink and measures what the daemon
// puts on the event bus (JSON bytes and events) plus CPU and allocations of sink and marshalling.
func BenchmarkSinkStream100KB(b *testing.B) {
	chunks := replyChunks()
	var wire, events int64
	count := func(v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			b.Fatal(err)
		}
		wire += int64(len(raw))
		events++
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		clock := time.Unix(1, 0)
		s := newSink(Message{ID: "m", BotID: "b", Role: "assistant", Status: StatusRunning}, nil,
			func(m Message) { count(m) }, func(d MessageDelta) { count(d) })
		s.now = func() time.Time { return clock }
		for _, c := range chunks {
			clock = clock.Add(publishInterval)
			s.appendText(PartText, c)
		}
		if err := s.finish(StatusComplete, ""); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(wire)/float64(b.N), "wire-B/op")
	b.ReportMetric(float64(events)/float64(b.N), "events/op")
}

// BenchmarkSinkAppend100KB feeds the same reply as 4-byte token deltas without publishing: the
// cost of building the text part alone.
func BenchmarkSinkAppend100KB(b *testing.B) {
	text := benchText()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s := newSink(Message{ID: "m", BotID: "b"}, nil, nil, nil)
		for i := 0; i < len(text); i += 4 {
			s.appendText(PartText, text[i:min(i+4, len(text))])
		}
	}
}

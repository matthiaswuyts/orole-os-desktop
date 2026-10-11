package terminals

import (
	"bytes"
	"math/rand/v2"
	"sync"
	"testing"
)

func pattern(from, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((from + i) * 7 % 251)
	}
	return b
}

func read(b *scrollback, from, to uint64) ([]byte, uint64) {
	segs, start := b.view(nil, from, to)
	out := copySegments(segs)
	b.unpin()
	return out, start
}

func TestScrollbackKeepsNewestBytes(t *testing.T) {
	tests := []struct {
		name   string
		max    int
		writes []int
	}{
		{"under the limit", 1000, []int{10, 20, 30}},
		{"exactly full", 256, []int{100, 156}},
		{"one big write", 300, []int{5000}},
		{"many small writes", 1 << 20, repeat(100, 13_000)},
		{"page-sized writes", 3 * scrollbackPage, repeat(scrollbackPage, 10)},
		{"odd sizes across pages", 2*scrollbackPage + 17, repeat(scrollbackPage-1, 9)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &scrollback{max: tt.max}
			total := 0
			for _, n := range tt.writes {
				b.write(pattern(total, n))
				total += n
			}
			want := min(total, tt.max)
			if b.size != want || b.end != uint64(total) || b.start() != uint64(total-want) {
				t.Fatalf("size=%d end=%d start=%d, want size=%d end=%d", b.size, b.end, b.start(), want, total)
			}
			got, start := read(b, 0, b.end)
			if start != uint64(total-want) || !bytes.Equal(got, pattern(total-want, want)) {
				t.Fatalf("retained bytes are not the newest %d (start %d)", want, start)
			}
			// Sub-ranges, including ones that start before the retained window.
			rng := rand.New(rand.NewPCG(1, 2))
			for range 50 {
				from, to := uint64(rng.IntN(total+1)), uint64(rng.IntN(total+1))
				if from > to {
					from, to = to, from
				}
				got, start := read(b, from, to)
				lo := max(int(from), total-want)
				if int(to) <= lo {
					if len(got) != 0 {
						t.Fatalf("[%d,%d): got %d bytes, want none", from, to, len(got))
					}
					continue
				}
				if int(start) != lo || !bytes.Equal(got, pattern(lo, int(to)-lo)) {
					t.Fatalf("[%d,%d): start=%d len=%d", from, to, start, len(got))
				}
			}
		})
	}
}

func repeat(n, times int) []int {
	out := make([]int, times)
	for i := range out {
		out[i] = n
	}
	return out
}

// A full scrollback reuses its pages instead of allocating, but never one a
// reader still holds a view of.
func TestScrollbackReusesPagesOnlyWhenUnpinned(t *testing.T) {
	b := &scrollback{max: 2 * scrollbackPage}
	chunk := pattern(0, 32<<10)
	for range 8 {
		b.write(chunk)
	}
	if allocs := testing.AllocsPerRun(200, func() { b.write(chunk) }); allocs != 0 {
		t.Fatalf("steady-state write allocates %v times", allocs)
	}

	segs, _ := b.view(nil, b.start(), b.end) // pinned
	want := copySegments(segs)
	for range 64 { // flood: every held page is dropped
		b.write(chunk)
	}
	if got := copySegments(segs); !bytes.Equal(got, want) {
		t.Fatal("a pinned view was overwritten by later writes")
	}
	b.unpin()
	if b.pinned != 0 {
		t.Fatalf("pinned=%d", b.pinned)
	}
}

// Readers copying outside the lock must see stable bytes while the writer
// floods (run with -race).
func TestScrollbackConcurrentViews(t *testing.T) {
	b := &scrollback{max: 4 * scrollbackPage}
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 4000 {
			mu.Lock()
			b.write(pattern(i*1000, 1000))
			mu.Unlock()
		}
	}()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 300 {
				mu.Lock()
				segs, start := b.view(nil, 0, b.end)
				mu.Unlock()
				got := copySegments(segs)
				mu.Lock()
				b.unpin()
				mu.Unlock()
				if len(got) > 0 && !bytes.Equal(got, pattern(int(start), len(got))) {
					t.Errorf("torn view at start %d (%d bytes)", start, len(got))
					return
				}
			}
		}()
	}
	wg.Wait()
}

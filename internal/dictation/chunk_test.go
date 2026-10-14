package dictation

import (
	"math"
	"testing"
)

// speech builds a recording at rate: loud tone everywhere except the given silent spans (in seconds).
func speech(rate int, seconds float64, silences ...[2]float64) []float32 {
	s := make([]float32, int(seconds*float64(rate)))
	for i := range s {
		s[i] = float32(0.5 * math.Sin(float64(i)/3))
	}
	for _, sp := range silences {
		for i := int(sp[0] * float64(rate)); i < int(sp[1]*float64(rate)) && i < len(s); i++ {
			s[i] = 0
		}
	}
	return s
}

func TestChunks(t *testing.T) {
	const rate = 1000
	cases := []struct {
		name       string
		samples    []float32
		maxSeconds int
		// cutsIn lists, per cut, the span (seconds) the cut must land in. nil = no cut expected.
		cutsIn [][2]float64
	}{
		{"no limit", speech(rate, 100), 0, nil},
		{"negative limit", speech(rate, 100), -5, nil},
		{"fits exactly", speech(rate, 28), 28, nil},
		{"one silence", speech(rate, 40, [2]float64{24, 24.5}), 28, [][2]float64{{24, 24.5}}},
		{"silence before the search window is ignored", speech(rate, 40, [2]float64{10, 11}, [2]float64{25, 25.3}), 28, [][2]float64{{25, 25.3}}},
		{"two cuts", speech(rate, 70, [2]float64{22, 22.4}, [2]float64{45, 45.5}), 28, [][2]float64{{22, 22.4}, {45, 45.5}}},
		{"no silence still bounded", speech(rate, 90), 28, nil},
		{"one sample over", speech(rate, 28.001), 28, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Chunks(c.samples, rate, c.maxSeconds)
			total := 0
			for i, ch := range got {
				if len(ch) == 0 {
					t.Fatalf("chunk %d is empty", i)
				}
				if c.maxSeconds > 0 && len(ch) > c.maxSeconds*rate {
					t.Fatalf("chunk %d has %d samples, max %d", i, len(ch), c.maxSeconds*rate)
				}
				// Chunks are consecutive views: nothing lost, duplicated or reordered.
				if &ch[0] != &c.samples[total] {
					t.Fatalf("chunk %d does not start at sample %d", i, total)
				}
				total += len(ch)
			}
			if total != len(c.samples) {
				t.Fatalf("chunks cover %d samples of %d", total, len(c.samples))
			}
			if c.maxSeconds <= 0 || len(c.samples) <= c.maxSeconds*rate {
				if len(got) != 1 {
					t.Fatalf("%d chunks, want 1", len(got))
				}
				return
			}
			if c.cutsIn == nil {
				return
			}
			if len(got) != len(c.cutsIn)+1 {
				t.Fatalf("%d chunks, want %d", len(got), len(c.cutsIn)+1)
			}
			at := 0
			for i, span := range c.cutsIn {
				at += len(got[i])
				sec := float64(at) / rate
				if sec < span[0] || sec > span[1] {
					t.Fatalf("cut %d at %.3f s, want within %v", i, sec, span)
				}
			}
		})
	}
	if got := Chunks(nil, rate, 28); len(got) != 0 {
		t.Fatalf("no audio: %d chunks", len(got))
	}
}

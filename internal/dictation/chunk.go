package dictation

// Chunks splits samples (mono, at rate) into pieces of at most maxSeconds each, for models that read a fixed
// window (Whisper reads 30 s). It returns one chunk when maxSeconds <= 0 or the audio fits. Otherwise each cut
// lands in the quietest 100 ms window (lowest RMS) within the last cutSearch seconds before the limit, so words are
// rarely split. Chunks share the backing array of samples, cover it exactly once in order and are never empty.
func Chunks(samples []float32, rate, maxSeconds int) [][]float32 {
	if len(samples) == 0 {
		return nil
	}
	if maxSeconds <= 0 || rate <= 0 {
		return [][]float32{samples}
	}
	limit := rate * maxSeconds
	win := rate / 10 // 100 ms
	if win < 1 {
		win = 1
	}
	search := rate * cutSearch
	if search > limit {
		search = limit
	}
	var out [][]float32
	rest := samples
	for len(rest) > limit {
		cut := quietestCut(rest, limit-search, limit, win)
		out = append(out, rest[:cut:cut])
		rest = rest[cut:]
	}
	return append(out, rest)
}

// cutSearch is how far before the limit (in seconds) Chunks looks for a quiet place to cut.
const cutSearch = 8

// quietestCut returns the middle of the lowest-energy window of win samples starting in [from, to-win], so
// the cut is in (0, to]. Ties keep the latest window (longer chunks, fewer of them).
func quietestCut(s []float32, from, to, win int) int {
	if win > to {
		win = to
	}
	if from < 0 {
		from = 0
	}
	last := to - win
	if from > last {
		from = last
	}
	// Running sum of squares over the window.
	var sum float64
	for _, v := range s[from : from+win] {
		sum += float64(v) * float64(v)
	}
	best, bestStart := sum, from
	for start := from + 1; start <= last; start++ {
		out, in := float64(s[start-1]), float64(s[start+win-1])
		sum += in*in - out*out
		if sum <= best+1e-12 {
			best, bestStart = sum, start
		}
	}
	cut := bestStart + win/2
	if cut < 1 {
		cut = 1
	}
	return cut
}

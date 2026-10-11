package terminals

// scrollbackPage is the allocation unit of a scrollback.
const scrollbackPage = 64 << 10

// page is one fixed-size block. Live bytes are buf[off:n]; bytes below n are
// never modified while the page is live, so a reader that took a view under
// the session lock may copy it after releasing the lock while the PTY reader
// keeps appending at n and dropping whole pages off the front.
type page struct {
	buf    []byte // len == scrollbackPage
	off, n int
}

// scrollback keeps the most recent `max` bytes of a session's output as a
// bounded ring of pages and addresses them by stream offset (the number of
// bytes the session had produced before them). Pages that fall off the front
// are reused for new output, so a full scrollback allocates nothing. It is
// not safe for concurrent use; the owning Session guards it with its mutex.
type scrollback struct {
	max    int
	pages  []page
	size   int    // bytes retained
	end    uint64 // stream offset one past the newest byte
	spare  [][]byte
	pinned int // outstanding views; while > 0 dropped pages are not reused
}

func (b *scrollback) start() uint64 { return b.end - uint64(b.size) }

func (b *scrollback) newPage() page {
	if n := len(b.spare); n > 0 {
		buf := b.spare[n-1]
		b.spare = b.spare[:n-1]
		return page{buf: buf}
	}
	return page{buf: make([]byte, scrollbackPage)}
}

// write appends p, discarding the oldest bytes beyond max.
func (b *scrollback) write(p []byte) {
	b.end += uint64(len(p))
	for len(p) > 0 {
		if n := len(b.pages); n == 0 || b.pages[n-1].n == scrollbackPage {
			b.pages = append(b.pages, b.newPage())
		}
		t := &b.pages[len(b.pages)-1]
		k := copy(t.buf[t.n:], p)
		t.n += k
		b.size += k
		p = p[k:]
	}
	for drop := b.size - b.max; drop > 0; {
		head := &b.pages[0]
		live := head.n - head.off
		if live > drop {
			head.off += drop
			b.size -= drop
			return
		}
		drop -= live
		b.size -= live
		if b.pinned == 0 && len(b.spare) < 2 {
			b.spare = append(b.spare, head.buf)
		}
		last := len(b.pages) - 1
		copy(b.pages, b.pages[1:]) // a handful of headers; keeps the backing array from creeping
		b.pages[last] = page{}
		b.pages = b.pages[:last]
	}
}

// view pins the scrollback and appends to dst slice headers for the retained
// bytes in stream range [from, to), clamped to what is still held. It returns
// the extended dst and the clamped start offset. The caller must read the
// bytes with copySegments and then call unpin, both of which are safe without
// the session lock; view and unpin themselves need it.
func (b *scrollback) view(dst [][]byte, from, to uint64) ([][]byte, uint64) {
	b.pinned++
	start := b.start()
	if from < start {
		from = start
	}
	if to > b.end {
		to = b.end
	}
	if to <= from {
		return dst, from
	}
	skip := int(from - start)
	want := int(to - from)
	for _, pg := range b.pages {
		if want == 0 {
			break
		}
		live := pg.buf[pg.off:pg.n]
		if skip >= len(live) {
			skip -= len(live)
			continue
		}
		live = live[skip:]
		skip = 0
		if len(live) > want {
			live = live[:want]
		}
		dst = append(dst, live)
		want -= len(live)
	}
	return dst, from
}

func (b *scrollback) unpin() { b.pinned-- }

// segmentsLen is the total byte length of segs.
func segmentsLen(segs [][]byte) int {
	n := 0
	for _, s := range segs {
		n += len(s)
	}
	return n
}

// copySegments returns a fresh contiguous copy of segs.
func copySegments(segs [][]byte) []byte {
	out := make([]byte, 0, segmentsLen(segs))
	for _, s := range segs {
		out = append(out, s...)
	}
	return out
}

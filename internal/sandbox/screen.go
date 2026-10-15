package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ScreenChunk is live screen output for the UI, published as the sandbox.screen.data event. The last chunk of a
// stream has Closed set; Error says why when it did not end because the user closed it.
type ScreenChunk struct {
	ID     string `json:"id"`
	Data   []byte `json:"data,omitempty"` // base64 in JSON
	Closed bool   `json:"closed,omitempty"`
	Error  string `json:"error,omitempty"`
}

var (
	// ErrNoScreen is returned for input to a screen that is not open (closed, evicted or never opened).
	ErrNoScreen = errors.New("sandbox: no such screen")
	// ErrScreenInputTooLarge is returned for one Input larger than ScreenChunkMax; the screen is closed, since the
	// dropped bytes would desynchronise the RFB stream. The UI splits larger writes (a big clipboard) itself.
	ErrScreenInputTooLarge = errors.New("sandbox: screen input too large")
)

const (
	// ScreenChunkMax bounds one sandbox.screen.data event and one Input. RFB is client-pull (the guest sends an
	// update only after the viewer asks for one), so a stream cannot flood the IPC queue.
	ScreenChunkMax = 128 << 10
	// ScreenOpenTimeout bounds opening a guest screen stream in Connect; the open stream lives until closed.
	ScreenOpenTimeout = 30 * time.Second
	screensPerBot     = 2
	// screenReleaseTimeout bounds how long ending a screen waits for the guest to take the key and button releases
	// (and for an Input write already in progress); then the stream is closed regardless.
	screenReleaseTimeout = 2 * time.Second
)

// ScreenHub relays the guest's VNC stream for open Computer tabs. Guest bytes go out as events; user bytes come in
// through Input and pass the RFB filter, which lets keyboard and mouse through only while the user holds the
// takeover lock.
type ScreenHub struct {
	publish        func(ScreenChunk)
	takenOver      func(botID string) bool
	releaseTimeout time.Duration // screenReleaseTimeout; shorter in tests

	mu      sync.Mutex
	screens map[string]*screenStream
	order   []string // open ids, oldest first
}

type screenStream struct {
	id, botID string
	rwc       io.ReadWriteCloser

	// inMu serialises everything that touches filter or writes to rwc: Input, TakeoverEnded and the final release.
	// The filter has no lock of its own, and release bytes must fall between complete forwarded messages.
	inMu   sync.Mutex
	filter *rfbFilter
	ended  bool // under inMu: the final release went out and rwc is closed

	finishOnce sync.Once

	pubMu  sync.Mutex // no data chunk is published after the closed chunk
	closed bool
}

// NewScreenHub creates a hub. publish must not block for long; takenOver is Manager.TakenOver.
func NewScreenHub(publish func(ScreenChunk), takenOver func(botID string) bool) *ScreenHub {
	return &ScreenHub{publish: publish, takenOver: takenOver, releaseTimeout: screenReleaseTimeout, screens: map[string]*screenStream{}}
}

// Connect opens a guest screen stream with open (Guest.OpenScreen) and starts relaying it. ctx bounded by
// ScreenOpenTimeout limits only the opening: the stream itself lives until it is closed.
func (h *ScreenHub) Connect(ctx context.Context, botID string, open func(context.Context) (io.ReadWriteCloser, error)) (string, error) {
	octx, cancel := context.WithTimeout(ctx, ScreenOpenTimeout)
	defer cancel()
	stream, err := open(octx)
	if err != nil {
		return "", err
	}
	return h.Open(botID, stream), nil
}

// Open starts relaying stream (from Guest.OpenScreen) and returns the screen id. A bot keeps at most two open
// screens; the oldest is closed when a third opens.
func (h *ScreenHub) Open(botID string, stream io.ReadWriteCloser) string {
	s := &screenStream{id: uuid.NewString(), botID: botID, rwc: stream}
	s.filter = newRFBFilter(func() bool { return h.takenOver(botID) })
	var evict []*screenStream
	h.mu.Lock()
	h.screens[s.id] = s
	h.order = append(h.order, s.id)
	count := 0
	for i := len(h.order) - 1; i >= 0; i-- { // newest first
		o := h.screens[h.order[i]]
		if o.botID != botID {
			continue
		}
		if count++; count > screensPerBot {
			evict = append(evict, o)
		}
	}
	for _, o := range evict {
		h.removeLocked(o.id)
	}
	h.mu.Unlock()
	for _, o := range evict {
		h.finish(o, "the screen was opened somewhere else")
	}
	go h.pump(s)
	return s.id
}

// Input passes user bytes (at most ScreenChunkMax) through the RFB filter to the guest.
func (h *ScreenHub) Input(id string, data []byte) error {
	h.mu.Lock()
	s := h.screens[id]
	h.mu.Unlock()
	if s == nil {
		return ErrNoScreen
	}
	if len(data) > ScreenChunkMax {
		h.drop(s, "the screen received unexpected input")
		return ErrScreenInputTooLarge
	}
	s.inMu.Lock()
	if s.ended {
		s.inMu.Unlock()
		return ErrNoScreen
	}
	forward, err := s.filter.Feed(data)
	if err != nil {
		s.inMu.Unlock()
		h.drop(s, "the screen received unexpected input")
		return err
	}
	if len(forward) > 0 {
		_, err = s.rwc.Write(forward)
	}
	s.inMu.Unlock() // before drop: finish takes inMu for the final release
	if err != nil {
		h.drop(s, "the screen stream ended unexpectedly")
		return err
	}
	return nil
}

// TakeoverEnded releases, in the guest, every key and pointer button the user still holds down on botID's screens,
// so control goes back to the bot with nothing stuck. Call it when Manager reports the takeover lock released; it
// does nothing while the user still holds the lock. It does not block on the guest.
func (h *ScreenHub) TakeoverEnded(botID string) {
	h.mu.Lock()
	var mine []*screenStream
	for _, id := range h.order {
		if s := h.screens[id]; s.botID == botID {
			mine = append(mine, s)
		}
	}
	h.mu.Unlock()
	for _, s := range mine {
		go func() {
			s.inMu.Lock()
			var err error
			if !s.ended && !h.takenOver(botID) {
				if rel := s.filter.Release(); len(rel) > 0 {
					_, err = s.rwc.Write(rel)
				}
			}
			s.inMu.Unlock()
			if err != nil {
				h.drop(s, "the screen stream ended unexpectedly")
			}
		}()
	}
}

// Close ends a screen the user closed. Unknown ids are ignored.
func (h *ScreenHub) Close(id string) {
	h.mu.Lock()
	s := h.screens[id]
	if s != nil {
		h.removeLocked(id)
	}
	h.mu.Unlock()
	if s != nil {
		h.finish(s, "")
	}
}

// CloseBot ends every screen of a bot whose computer stopped.
func (h *ScreenHub) CloseBot(botID string) {
	h.closeWhere(func(s *screenStream) bool { return s.botID == botID }, "the computer stopped")
}

// CloseAll ends every screen (daemon shutdown).
func (h *ScreenHub) CloseAll() {
	h.closeWhere(func(*screenStream) bool { return true }, "the computer stopped")
}

func (h *ScreenHub) closeWhere(match func(*screenStream) bool, reason string) {
	var gone []*screenStream
	h.mu.Lock()
	for _, id := range append([]string(nil), h.order...) {
		if s := h.screens[id]; match(s) {
			gone = append(gone, s)
			h.removeLocked(id)
		}
	}
	h.mu.Unlock()
	for _, s := range gone {
		h.finish(s, reason)
	}
}

func (h *ScreenHub) pump(s *screenStream) {
	buf := make([]byte, ScreenChunkMax)
	for {
		n, err := s.rwc.Read(buf)
		if n > 0 {
			s.emit(h, ScreenChunk{ID: s.id, Data: bytes.Clone(buf[:n])}, false)
		}
		if err != nil {
			reason := ""
			if !errors.Is(err, io.EOF) {
				reason = "the screen stream ended unexpectedly"
			}
			h.drop(s, reason)
			return
		}
	}
}

// drop removes s if it is still registered and ends it.
func (h *ScreenHub) drop(s *screenStream, reason string) {
	h.mu.Lock()
	if h.screens[s.id] == s {
		h.removeLocked(s.id)
	}
	h.mu.Unlock()
	h.finish(s, reason)
}

// finish publishes the closed chunk (once), releases in the guest whatever the user still holds down, and closes
// the guest stream. It never waits on the guest for longer than releaseTimeout: the stream is closed then,
// which also fails an Input write stuck on it.
func (h *ScreenHub) finish(s *screenStream, reason string) {
	s.emit(h, ScreenChunk{ID: s.id, Closed: true, Error: reason}, true)
	s.finishOnce.Do(func() {
		force := time.AfterFunc(h.releaseTimeout, func() { _ = s.rwc.Close() })
		defer force.Stop()
		s.inMu.Lock()
		defer s.inMu.Unlock()
		if rel := s.filter.Release(); len(rel) > 0 { // best effort: a failed stream just refuses it
			_, _ = s.rwc.Write(rel)
		}
		_ = s.rwc.Close() // under inMu, so no Input can slip a key down in after the release
		s.ended = true
	})
}

func (s *screenStream) emit(h *ScreenHub, c ScreenChunk, final bool) {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	if s.closed {
		return
	}
	if final {
		s.closed = true
	}
	h.publish(c)
}

// removeLocked forgets id. h.mu must be held.
func (h *ScreenHub) removeLocked(id string) {
	delete(h.screens, id)
	for i, o := range h.order {
		if o == id {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
}

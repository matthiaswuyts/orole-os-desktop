package sandbox

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Handoff timeouts. DefaultHandoffAsk matches policy.DefaultApprovalTimeout: a takeover request waits as long as an
// approval for the user to press Take over; after that the bot waits until Done, at most DefaultHandoffHold.
const (
	DefaultHandoffAsk  = 5 * time.Minute
	DefaultHandoffHold = 30 * time.Minute
)

var (
	ErrHandoffTimeout     = errors.New("sandbox: the user did not take over in time")
	ErrHandoffHoldTimeout = errors.New("sandbox: the user did not hand the computer back in time")
	ErrHandoffBusy        = errors.New("sandbox: already waiting for the user to take over")
)

// HandoffState is the sandbox.handoff event payload and the sandbox.handoff method's answer.
type HandoffState struct {
	BotID   string `json:"botId"`
	Pending bool   `json:"pending"`
	Reason  string `json:"reason,omitempty"`
	Since   int64  `json:"since,omitempty"` // unix ms
}

// Handoffs tracks bots that asked the user to take over their computer (ask_user_to_take_over). A wait ends when
// the user presses Done (Finish), when nobody took over within AskTimeout, when the user took over but did not
// finish within HoldTimeout, or when the bot's turn ends. It holds no VM state: the lock itself is Manager.TakeOver.
type Handoffs struct {
	AskTimeout  time.Duration
	HoldTimeout time.Duration

	onChange func(HandoffState)
	mu       sync.Mutex
	waits    map[string]*handoff
}

type handoff struct {
	reason  string
	since   time.Time
	started chan struct{} // closed when the user takes over
	took    bool
	done    chan string // the user's note, buffered 1
}

// NewHandoffs returns Handoffs with the default timeouts. onChange (may be nil) is called without locks held
// whenever a wait starts or ends.
func NewHandoffs(onChange func(HandoffState)) *Handoffs {
	return &Handoffs{AskTimeout: DefaultHandoffAsk, HoldTimeout: DefaultHandoffHold, onChange: onChange, waits: map[string]*handoff{}}
}

// Ask blocks until the user hands the bot's computer back and returns their note. takenOver says the user already
// holds the computer, so the hold timeout applies at once.
func (h *Handoffs) Ask(ctx context.Context, botID, reason string, takenOver bool) (string, error) {
	w := &handoff{reason: reason, since: time.Now(), started: make(chan struct{}), done: make(chan string, 1)}
	if takenOver {
		w.took = true
		close(w.started)
	}
	h.mu.Lock()
	if _, busy := h.waits[botID]; busy {
		h.mu.Unlock()
		return "", ErrHandoffBusy
	}
	h.waits[botID] = w
	h.mu.Unlock()
	h.changed(botID)
	defer func() {
		h.mu.Lock()
		if h.waits[botID] == w {
			delete(h.waits, botID)
		}
		h.mu.Unlock()
		h.changed(botID)
	}()

	limit, started := h.AskTimeout, (<-chan struct{})(w.started)
	if takenOver {
		limit, started = h.HoldTimeout, nil
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	for {
		select {
		case note := <-w.done:
			return note, nil
		case <-started:
			started, takenOver = nil, true
			timer.Reset(h.HoldTimeout) // Go 1.23+ timers: Reset never delivers a stale tick
		case <-timer.C:
			if takenOver {
				return "", ErrHandoffHoldTimeout
			}
			return "", ErrHandoffTimeout
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// Started records that the user pressed Take over. It does nothing when the bot is not waiting.
func (h *Handoffs) Started(botID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w := h.waits[botID]; w != nil && !w.took {
		w.took = true
		close(w.started)
	}
}

// Finish hands the computer back with the user's note. It does nothing when the bot is not waiting.
func (h *Handoffs) Finish(botID, note string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w := h.waits[botID]; w != nil {
		select {
		case w.done <- note:
		default:
		}
	}
}

// State reports whether the bot waits for the user and why.
func (h *Handoffs) State(botID string) HandoffState {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w := h.waits[botID]; w != nil {
		return HandoffState{BotID: botID, Pending: true, Reason: w.reason, Since: w.since.UnixMilli()}
	}
	return HandoffState{BotID: botID}
}

func (h *Handoffs) changed(botID string) {
	if h.onChange != nil {
		h.onChange(h.State(botID))
	}
}

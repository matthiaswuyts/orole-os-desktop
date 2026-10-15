package sandbox

import (
	"sync"
	"time"
)

// ViewerLeases keeps Manager's viewer flag honest when the GUI goes away without saying so: the Computer tab renews
// its lease every minute, and a lease not renewed within ttl clears the flag, so the idle stop still happens.
type ViewerLeases struct {
	set func(botID string, watching bool)
	ttl time.Duration

	mu       sync.Mutex
	timers   map[string]*time.Timer
	onExpire func(botID string)
	closed   bool
}

// NewViewerLeases creates leases over set (Manager.SetViewer).
func NewViewerLeases(set func(botID string, watching bool), ttl time.Duration) *ViewerLeases {
	return &ViewerLeases{set: set, ttl: ttl, timers: map[string]*time.Timer{}}
}

// OnExpire registers fn, called (without locks held, after the flag is cleared) when botID's lease runs out without
// renewal. An explicit Unwatch is not an expiry. The daemon passes ReleaseAbandonedTakeover.
func (v *ViewerLeases) OnExpire(fn func(botID string)) {
	v.mu.Lock()
	v.onExpire = fn
	v.mu.Unlock()
}

// Watch starts or renews botID's lease.
func (v *ViewerLeases) Watch(botID string) {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	if t := v.timers[botID]; t != nil {
		t.Stop()
	}
	var t *time.Timer
	t = time.AfterFunc(v.ttl, func() {
		v.mu.Lock()
		mine := v.timers[botID] == t && !v.closed
		if mine {
			delete(v.timers, botID)
		}
		expire := v.onExpire
		v.mu.Unlock()
		if mine {
			v.set(botID, false)
			if expire != nil {
				expire(botID)
			}
		}
	})
	v.timers[botID] = t
	v.mu.Unlock()
	v.set(botID, true)
}

// Unwatch ends botID's lease now.
func (v *ViewerLeases) Unwatch(botID string) {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	if t := v.timers[botID]; t != nil {
		t.Stop()
		delete(v.timers, botID)
	}
	v.mu.Unlock()
	v.set(botID, false)
}

// Close stops every timer without touching the manager (daemon shutdown stops the VMs anyway).
func (v *ViewerLeases) Close() {
	v.mu.Lock()
	v.closed = true
	for id, t := range v.timers {
		t.Stop()
		delete(v.timers, id)
	}
	v.mu.Unlock()
}

// ReleaseAbandonedTakeover returns an OnExpire callback: when nobody has watched a bot's computer for a whole lease
// while the user holds the takeover lock and no handoff is pending, the lock goes back to the bot (release is
// Manager.Release). A takeover the bot asked for (a pending handoff) is left alone: that wait has its own timeouts.
func ReleaseAbandonedTakeover(takenOver, handoffPending func(botID string) bool, release func(botID string)) func(botID string) {
	return func(botID string) {
		if takenOver(botID) && !handoffPending(botID) {
			release(botID)
		}
	}
}

package sandbox

import (
	"sync"
	"time"
)

// ViewerLeases keeps Manager's viewer flag honest when the GUI goes away without saying so: the Computer tab renews
// its lease every minute, and a lease not renewed within ttl clears the flag, so the idle stop still happens.
//
// set and the OnExpire callback run with the lease lock held, so a renewal can never interleave between an expiry's
// check and its effects; they must not call back into ViewerLeases.
type ViewerLeases struct {
	set       func(botID string, watching bool)
	ttl       time.Duration
	afterFunc func(time.Duration, func()) *time.Timer // time.AfterFunc; replaced in tests

	mu       sync.Mutex
	leases   map[string]lease
	gens     map[string]uint64 // per bot, bumped by every Watch and Unwatch
	onExpire func(botID string)
	closed   bool
}

type lease struct {
	timer *time.Timer
	gen   uint64
}

// NewViewerLeases creates leases over set (Manager.SetViewer).
func NewViewerLeases(set func(botID string, watching bool), ttl time.Duration) *ViewerLeases {
	return &ViewerLeases{set: set, ttl: ttl, afterFunc: time.AfterFunc, leases: map[string]lease{}, gens: map[string]uint64{}}
}

// OnExpire registers fn, called after the flag is cleared when botID's lease runs out without renewal. An explicit
// Unwatch is not an expiry. The daemon passes ReleaseAbandonedTakeover.
func (v *ViewerLeases) OnExpire(fn func(botID string)) {
	v.mu.Lock()
	v.onExpire = fn
	v.mu.Unlock()
}

// Watch starts or renews botID's lease.
func (v *ViewerLeases) Watch(botID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return
	}
	if l, ok := v.leases[botID]; ok {
		l.timer.Stop()
	}
	v.gens[botID]++
	gen := v.gens[botID]
	v.leases[botID] = lease{timer: v.afterFunc(v.ttl, func() { v.expire(botID, gen) }), gen: gen}
	v.set(botID, true)
}

// expire ends lease gen of botID unless it was renewed, unwatched or closed since.
func (v *ViewerLeases) expire(botID string, gen uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed || v.gens[botID] != gen {
		return
	}
	delete(v.leases, botID)
	v.set(botID, false)
	if v.onExpire != nil {
		v.onExpire(botID)
	}
}

// Unwatch ends botID's lease now.
func (v *ViewerLeases) Unwatch(botID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return
	}
	if l, ok := v.leases[botID]; ok {
		l.timer.Stop()
		delete(v.leases, botID)
	}
	v.gens[botID]++
	v.set(botID, false)
}

// Close stops every timer without touching the manager (daemon shutdown stops the VMs anyway).
func (v *ViewerLeases) Close() {
	v.mu.Lock()
	v.closed = true
	for id, l := range v.leases {
		l.timer.Stop()
		delete(v.leases, id)
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

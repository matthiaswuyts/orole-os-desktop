package sandbox

import (
	"sync"
	"testing"
	"time"
)

type viewerLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *viewerLog) set(botID string, watching bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if watching {
		l.calls = append(l.calls, botID+":on")
	} else {
		l.calls = append(l.calls, botID+":off")
	}
}

func (l *viewerLog) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.calls) == 0 {
		return ""
	}
	return l.calls[len(l.calls)-1]
}

func TestViewerLeaseExpiresWithoutRenewal(t *testing.T) {
	log := &viewerLog{}
	v := NewViewerLeases(log.set, 40*time.Millisecond)
	t.Cleanup(v.Close)
	v.Watch("b1")
	if log.last() != "b1:on" {
		t.Fatalf("watch: %q", log.last())
	}
	for range 4 { // renewals keep it alive past several TTLs
		time.Sleep(20 * time.Millisecond)
		v.Watch("b1")
	}
	if log.last() != "b1:on" {
		t.Fatalf("renewed lease expired: %q", log.last())
	}
	deadline := time.Now().Add(2 * time.Second)
	for log.last() != "b1:off" {
		if time.Now().After(deadline) {
			t.Fatal("lease never expired")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestViewerUnwatchClearsAtOnceAndCloseStopsTimers(t *testing.T) {
	log := &viewerLog{}
	v := NewViewerLeases(log.set, time.Hour)
	v.Watch("b1")
	v.Unwatch("b1")
	if log.last() != "b1:off" {
		t.Fatalf("unwatch: %q", log.last())
	}
	v.Watch("b2")
	v.Close()
	before := log.last()
	v.Watch("b3") // after Close: ignored
	if log.last() != before {
		t.Fatal("watch after Close reached the manager")
	}
}

func TestViewerLeaseExpiryCallsOnExpireOnlyOnExpiry(t *testing.T) {
	log := &viewerLog{}
	expired := make(chan string, 4)
	v := NewViewerLeases(log.set, 20*time.Millisecond)
	t.Cleanup(v.Close)
	v.OnExpire(func(botID string) { expired <- botID })
	v.Watch("b1")
	v.Watch("b2")
	v.Unwatch("b2") // an explicit unwatch is not an expiry
	select {
	case id := <-expired:
		if id != "b1" {
			t.Fatalf("expired %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no expiry")
	}
	if log.last() != "b1:off" {
		t.Fatalf("flag not cleared before the expiry callback: %q", log.last())
	}
	select {
	case id := <-expired:
		t.Fatalf("unexpected expiry of %q", id)
	case <-time.After(60 * time.Millisecond):
	}
}

func TestReleaseAbandonedTakeover(t *testing.T) {
	tests := []struct {
		name           string
		taken, pending bool
		wantRelease    bool
	}{
		{"taken over, no handoff: released", true, false, true},
		{"taken over for a pending handoff: kept", true, true, false},
		{"not taken over", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var released []string
			fn := ReleaseAbandonedTakeover(
				func(string) bool { return tt.taken },
				func(string) bool { return tt.pending },
				func(botID string) { released = append(released, botID) },
			)
			fn("b1")
			if got := len(released) == 1 && released[0] == "b1"; got != tt.wantRelease {
				t.Fatalf("released = %v", released)
			}
		})
	}
}

func TestViewerLeaseExpiryReleasesAnAbandonedTakeover(t *testing.T) {
	released := make(chan string, 1)
	v := NewViewerLeases(func(string, bool) {}, 20*time.Millisecond)
	t.Cleanup(v.Close)
	v.OnExpire(ReleaseAbandonedTakeover(
		func(string) bool { return true },
		func(string) bool { return false },
		func(botID string) { released <- botID },
	))
	v.Watch("b1")
	select {
	case id := <-released:
		if id != "b1" {
			t.Fatalf("released %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("takeover not released on lease expiry")
	}
}

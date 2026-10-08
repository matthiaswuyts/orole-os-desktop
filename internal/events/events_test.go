package events

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		pat, topic string
		want       bool
	}{
		{"*", "a.b", true},
		{"agent.*", "agent.finished", true},
		{"agent.*", "agent", true},
		{"agent.*", "agents.finished", false},
		{"agent.finished", "agent.finished", true},
		{"agent.finished", "agent.finished.x", false},
		{"github.*", "agent.x", false},
	} {
		if got := Match(c.pat, c.topic); got != c.want {
			t.Errorf("Match(%q,%q)=%v want %v", c.pat, c.topic, got, c.want)
		}
	}
}

func TestValidPattern(t *testing.T) {
	for p, want := range map[string]bool{"*": true, "a.*": true, "a.b": true, "": false, "a*": false, "a b": false, "*.a": false, ".*": false} {
		if got := ValidPattern(p); got != want {
			t.Errorf("ValidPattern(%q)=%v want %v", p, got, want)
		}
	}
}

func TestPublishDeliversAndBounds(t *testing.T) {
	b := NewBus()
	s := b.Subscribe("agent.*", 2)
	other := b.Subscribe("github.*", 2)
	defer s.Close()
	defer other.Close()
	for i := 0; i < 5; i++ {
		b.Publish(Event{Topic: "agent.finished", Data: map[string]any{"i": i}})
	}
	if len(s.C) != 2 {
		t.Fatalf("queue len %d, want bound 2", len(s.C))
	}
	if s.Dropped() != 3 {
		t.Fatalf("dropped %d, want 3", s.Dropped())
	}
	if len(other.C) != 0 {
		t.Fatal("non-matching subscriber received an event")
	}
	ev := <-s.C
	if ev.Time.IsZero() || ev.Data["i"] != 0 {
		t.Fatalf("unexpected first event %+v", ev)
	}
}

func TestPublishNeverBlocksOnSlowSubscriber(t *testing.T) {
	b := NewBus()
	s := b.Subscribe("*", 1)
	defer s.Close()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			b.Publish(Event{Topic: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked")
	}
}

func TestCloseRacesWithPublish(t *testing.T) {
	b := NewBus()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				b.Publish(Event{Topic: "t"})
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s := b.Subscribe("*", 4)
				s.Close()
				s.Close() // idempotent
			}
		}()
	}
	wg.Wait()
}

func TestChainContext(t *testing.T) {
	ctx := WithChain(context.Background(), []string{"a", "b"})
	got := ChainFrom(ctx)
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("chain %v", got)
	}
	got[0] = "x"
	if ChainFrom(ctx)[0] != "a" {
		t.Fatal("ChainFrom must return a copy")
	}
	if len(ChainFrom(context.Background())) != 0 {
		t.Fatal("empty context should have empty chain")
	}
}

package files

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitStatusCacheSingleFlightAndTTL(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	c := newGitStatusCache(func(ctx context.Context, dir string) (map[string]string, error) {
		calls.Add(1)
		<-release
		return map[string]string{"a.txt": " M"}, nil
	}, 50*time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if st, ok := c.get(context.Background(), "/repo"); !ok || st["a.txt"] != " M" {
				t.Errorf("get = %v, %v", st, ok)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("concurrent listings ran status %d times, want 1", n)
	}
	if _, ok := c.get(context.Background(), "/repo"); !ok || calls.Load() != 1 {
		t.Fatalf("fresh result was not reused (calls %d)", calls.Load())
	}
	if _, ok := c.get(context.Background(), "/other"); !ok || calls.Load() != 2 {
		t.Fatalf("a different directory must run its own status (calls %d)", calls.Load())
	}
	time.Sleep(80 * time.Millisecond)
	if _, ok := c.get(context.Background(), "/repo"); !ok || calls.Load() != 3 {
		t.Fatalf("expired result was not refreshed (calls %d)", calls.Load())
	}
}

func TestGitStatusCacheFailureAndCancel(t *testing.T) {
	boom := errors.New("boom")
	c := newGitStatusCache(func(ctx context.Context, dir string) (map[string]string, error) { return nil, boom }, time.Minute)
	if st, ok := c.get(context.Background(), "/repo"); ok || st != nil {
		t.Fatalf("failed status = %v, %v; want unavailable", st, ok)
	}

	block := make(chan struct{})
	defer close(block)
	slow := newGitStatusCache(func(ctx context.Context, dir string) (map[string]string, error) {
		<-block
		return nil, nil
	}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := slow.get(ctx, "/repo"); ok {
		t.Fatal("a cancelled caller must not wait for status")
	}
}

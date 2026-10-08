package systemmetrics

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

func TestCPUUtilization(t *testing.T) {
	max := uint64(math.MaxUint64)
	for _, tc := range []struct {
		name              string
		previous, current cpuCounters
		percent           float64
		ready             bool
	}{
		{"first empty interval", cpuCounters{}, cpuCounters{}, 0, false},
		{"idle", cpuCounters{}, cpuCounters{idle: 100, kernel: 100}, 0, true},
		{"busy", cpuCounters{}, cpuCounters{kernel: 60, user: 40}, 100, true},
		{"kernel includes idle", cpuCounters{10, 30, 20}, cpuCounters{60, 90, 60}, 50, true},
		{"fractional", cpuCounters{}, cpuCounters{idle: 1, kernel: 2, user: 1}, 200.0 / 3.0, true},
		{"idle rollback", cpuCounters{10, 20, 30}, cpuCounters{9, 25, 35}, 0, false},
		{"kernel rollback", cpuCounters{10, 20, 30}, cpuCounters{11, 19, 35}, 0, false},
		{"user rollback", cpuCounters{10, 20, 30}, cpuCounters{11, 25, 29}, 0, false},
		{"idle exceeds kernel delta", cpuCounters{}, cpuCounters{idle: 11, kernel: 10, user: 20}, 0, false},
		{"interval sum overflow", cpuCounters{}, cpuCounters{kernel: max, user: 1}, 0, false},
		{"large lifetime sums", cpuCounters{max - 100, max - 100, max - 100}, cpuCounters{max - 75, max - 50, max - 50}, 75, true},
		{"largest valid interval", cpuCounters{}, cpuCounters{kernel: max}, 100, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			percent, ready := cpuUtilization(tc.previous, tc.current)
			if ready != tc.ready || math.Abs(percent-tc.percent) > 1e-10 {
				t.Fatalf("got (%v, %v), want (%v, %v)", percent, ready, tc.percent, tc.ready)
			}
		})
	}
}

func TestSnapshotCacheAndInterval(t *testing.T) {
	s := NewSampler(t.TempDir())
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	calls := 0
	s.read = func(path string) (hostReading, error) {
		calls++
		if path != s.dataDir {
			t.Fatalf("sampled %q instead of configured %q", path, s.dataDir)
		}
		return hostReading{metrics: HostMetrics{MemoryTotalBytes: 1000}, cpu: cpuCounters{uint64(10 * calls), uint64(20 * calls), uint64(20 * calls)}}, nil
	}
	first, err := s.Snapshot(context.Background())
	if err != nil || first.CPUReady || first.CPUPercent != 0 || first.SampledAt != now || first.DiskPath != s.dataDir {
		t.Fatalf("first sample: %+v, %v", first, err)
	}
	now = now.Add(minimumInterval - time.Nanosecond)
	cached, err := s.Snapshot(context.Background())
	if err != nil || cached != first || calls != 1 {
		t.Fatalf("cache: %+v, %v; calls=%d", cached, err, calls)
	}
	now = now.Add(time.Nanosecond)
	second, err := s.Snapshot(context.Background())
	if err != nil || !second.CPUReady || second.CPUPercent != 75 || second.SampledAt != now || calls != 2 {
		t.Fatalf("second sample: %+v, %v; calls=%d", second, err, calls)
	}
}

func TestSnapshotConcurrentRequestsShareRead(t *testing.T) {
	s := NewSampler(t.TempDir())
	now := time.Now()
	s.now = func() time.Time { return now }
	calls := 0 // The sampler must serialize this reader.
	s.read = func(string) (hostReading, error) {
		calls++
		return hostReading{}, nil
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Snapshot(context.Background()); err != nil {
				t.Errorf("snapshot: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("concurrent requests performed %d reads", calls)
	}
}

func TestSnapshotFailureIsNotStaleSuccess(t *testing.T) {
	s := NewSampler(t.TempDir())
	now := time.Now()
	s.now = func() time.Time { return now }
	readErr := errors.New("native read failed")
	calls := 0
	s.read = func(string) (hostReading, error) {
		calls++
		if calls == 2 {
			return hostReading{}, readErr
		}
		return hostReading{metrics: HostMetrics{MemoryTotalBytes: 123}, cpu: cpuCounters{0, uint64(calls), uint64(calls)}}, nil
	}
	if _, err := s.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(minimumInterval)
	for range 2 {
		got, err := s.Snapshot(context.Background())
		if !errors.Is(err, readErr) || got != (HostMetrics{}) {
			t.Fatalf("failure returned stale or fabricated metrics: %+v, %v", got, err)
		}
	}
	if calls != 2 {
		t.Fatalf("failure was not cached: %d reads", calls)
	}
	now = now.Add(minimumInterval)
	got, err := s.Snapshot(context.Background())
	if err != nil || got.CPUReady || got.MemoryTotalBytes != 123 {
		t.Fatalf("recovery must establish a fresh CPU baseline: %+v, %v", got, err)
	}
}

func TestSnapshotCounterRollbackRecovers(t *testing.T) {
	s := NewSampler(t.TempDir())
	now := time.Now()
	s.now = func() time.Time { return now }
	counter := uint64(100)
	s.read = func(string) (hostReading, error) {
		return hostReading{cpu: cpuCounters{0, counter, counter}}, nil
	}
	for i, value := range []uint64{100, 10, 20} {
		counter = value
		got, err := s.Snapshot(context.Background())
		if err != nil || got.CPUReady != (i == 2) {
			t.Fatalf("sample %d: %+v, %v", i, got, err)
		}
		now = now.Add(minimumInterval)
	}
}

func TestSnapshotCancellationDoesNotPublishSample(t *testing.T) {
	s := NewSampler(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	s.read = func(string) (hostReading, error) {
		calls++
		cancel()
		return hostReading{}, nil
	}
	if got, err := s.Snapshot(ctx); !errors.Is(err, context.Canceled) || got != (HostMetrics{}) || s.attempted || s.haveCPU {
		t.Fatalf("canceled read was published: %+v, %v", got, err)
	}
	if _, err := s.Snapshot(ctx); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("already-canceled request reached native reader: %v, calls=%d", err, calls)
	}
}

func TestSnapshotRequiresConfiguredDirectory(t *testing.T) {
	s := NewSampler("")
	s.read = func(string) (hostReading, error) {
		t.Fatal("empty configuration reached native reader")
		return hostReading{}, nil
	}
	if got, err := s.Snapshot(context.Background()); err == nil || got != (HostMetrics{}) {
		t.Fatalf("empty data directory silently accepted: %+v, %v", got, err)
	}
}

func TestSnapshotClockRollbackDoesNotPinCache(t *testing.T) {
	s := NewSampler(t.TempDir())
	now := time.Now()
	s.now = func() time.Time { return now }
	calls := 0
	s.read = func(string) (hostReading, error) {
		calls++
		return hostReading{}, nil
	}
	if _, err := s.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(-time.Minute)
	if _, err := s.Snapshot(context.Background()); err != nil || calls != 2 {
		t.Fatalf("clock rollback pinned cache: %v, calls=%d", err, calls)
	}
}

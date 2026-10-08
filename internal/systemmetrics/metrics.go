// Package systemmetrics samples the host, not the daemon process. Sampling is
// demand-driven; a Sampler owns no background goroutines or operating-system handles.
package systemmetrics

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"path/filepath"
	"sync"
	"time"
)

// HostMetrics is one real host observation. CPUPercent is meaningful only when
// CPUReady is true: CPU utilization requires two distinct counter observations.
// DiskPath is the daemon-configured data directory, never a client-supplied path.
type HostMetrics struct {
	SampledAt            time.Time `json:"sampledAt"`
	CPUPercent           float64   `json:"cpuPercent"`
	CPUReady             bool      `json:"cpuReady"`
	LogicalCPUs          int       `json:"logicalCPUs"`
	MemoryTotalBytes     uint64    `json:"memoryTotalBytes"`
	MemoryAvailableBytes uint64    `json:"memoryAvailableBytes"`
	UptimeSeconds        uint64    `json:"uptimeSeconds"`
	DiskTotalBytes       uint64    `json:"diskTotalBytes"`
	DiskAvailableBytes   uint64    `json:"diskAvailableBytes"`
	DiskPath             string    `json:"diskPath"`
}

const minimumInterval = time.Second

// ErrUnsupported identifies a platform that cannot provide the requested host metrics.
var ErrUnsupported = errors.New("host metrics unsupported")

type cpuCounters struct {
	idle, kernel, user uint64 // Windows kernel time includes idle time.
}

type hostReading struct {
	metrics HostMetrics
	cpu     cpuCounters
}

// Sampler serializes native reads and caches both results and read failures for
// one second. It does not return stale successful metrics after a failed read.
type Sampler struct {
	mu          sync.Mutex
	dataDir     string
	configErr   error
	read        func(string) (hostReading, error)
	now         func() time.Time
	attempted   bool
	lastAttempt time.Time
	cached      HostMetrics
	cachedErr   error
	previous    cpuCounters
	haveCPU     bool
}

// NewSampler binds disk sampling to the daemon's configured data directory.
// Invalid configuration is reported by Snapshot, not replaced with the working directory.
func NewSampler(dataDir string) *Sampler {
	s := &Sampler{read: readHost, now: time.Now}
	if dataDir == "" {
		s.configErr = errors.New("host metrics: daemon data directory is empty")
		return s
	}
	path, err := filepath.Abs(dataDir)
	if err != nil {
		s.configErr = fmt.Errorf("host metrics: resolve daemon data directory: %w", err)
		return s
	}
	s.dataDir = path
	return s
}

// Snapshot returns a cached observation or takes a new native sample. Native
// calls are synchronous; cancellation is checked before and after those calls.
func (s *Sampler) Snapshot(ctx context.Context) (HostMetrics, error) {
	if err := ctx.Err(); err != nil {
		return HostMetrics{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return HostMetrics{}, err
	}
	if s.configErr != nil {
		return HostMetrics{}, s.configErr
	}
	now := s.now()
	if s.attempted && now.Sub(s.lastAttempt) >= 0 && now.Sub(s.lastAttempt) < minimumInterval {
		return s.cached, s.cachedErr
	}
	reading, err := s.read(s.dataDir)
	if canceled := ctx.Err(); canceled != nil {
		return HostMetrics{}, canceled
	}
	s.attempted, s.lastAttempt = true, now
	if err != nil {
		s.cached, s.cachedErr = HostMetrics{}, err
		s.haveCPU = false
		return s.cached, s.cachedErr
	}
	reading.metrics.SampledAt = now.UTC()
	reading.metrics.DiskPath = s.dataDir
	if s.haveCPU {
		reading.metrics.CPUPercent, reading.metrics.CPUReady = cpuUtilization(s.previous, reading.cpu)
	}
	s.previous, s.haveCPU = reading.cpu, true
	s.cached, s.cachedErr = reading.metrics, nil
	return s.cached, nil
}

// cpuUtilization subtracts before summing, avoiding overflow of lifetime totals.
// Counter resets, impossible idle deltas, and empty/overflowing intervals must
// start a fresh baseline instead of creating a misleading utilization spike.
func cpuUtilization(previous, current cpuCounters) (float64, bool) {
	if current.idle < previous.idle || current.kernel < previous.kernel || current.user < previous.user {
		return 0, false
	}
	idle := current.idle - previous.idle
	kernel := current.kernel - previous.kernel
	user := current.user - previous.user
	total, carry := bits.Add64(kernel, user, 0)
	if carry != 0 || total == 0 || idle > kernel {
		return 0, false
	}
	return float64(total-idle) / float64(total) * 100, true
}

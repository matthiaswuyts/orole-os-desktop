//go:build windows

package systemmetrics

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNativeSnapshotSanity(t *testing.T) {
	s := NewSampler(t.TempDir())
	before := time.Now().UTC()
	got, err := s.Snapshot(context.Background())
	if windows.GetActiveProcessorCount(0xffff) > 64 {
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("multi-group host must not report partial CPU metrics: %+v, %v", got, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if got.CPUReady || got.CPUPercent != 0 {
		t.Fatalf("initial CPU sample must not pretend to be an interval: %+v", got)
	}
	if got.LogicalCPUs != int(windows.GetActiveProcessorCount(0xffff)) {
		t.Fatalf("logical CPU count is not the host count: %d", got.LogicalCPUs)
	}
	if got.MemoryTotalBytes == 0 || got.MemoryAvailableBytes > got.MemoryTotalBytes {
		t.Fatalf("invalid physical memory sample: %+v", got)
	}
	if got.DiskTotalBytes == 0 || got.DiskAvailableBytes > got.DiskTotalBytes || got.DiskPath != s.dataDir {
		t.Fatalf("invalid daemon data volume sample: %+v", got)
	}
	if got.SampledAt.Before(before) || got.SampledAt.After(time.Now().UTC()) {
		t.Fatalf("sample timestamp outside native read interval: %v", got.SampledAt)
	}
	low, high, _ := getTickCount64.Call()
	millis := uint64(low)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		millis |= uint64(high) << 32
	}
	if seconds := millis / 1000; got.UptimeSeconds > seconds || seconds-got.UptimeSeconds > 5 {
		t.Fatalf("uptime %d does not match native boot clock %d", got.UptimeSeconds, seconds)
	}
}

func TestNativeMissingDataDirectoryFails(t *testing.T) {
	if windows.GetActiveProcessorCount(0xffff) > 64 {
		t.Skip("host metrics unsupported on multiple processor groups")
	}
	s := NewSampler(filepath.Join(t.TempDir(), "does-not-exist"))
	got, err := s.Snapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "GetDiskFreeSpaceEx") || got != (HostMetrics{}) {
		t.Fatalf("missing directory returned fabricated disk metrics: %+v, %v", got, err)
	}
}

func TestNativeCounterRepresentation(t *testing.T) {
	if size := unsafe.Sizeof(memoryStatusEx{}); size != 64 {
		t.Fatalf("MEMORYSTATUSEX layout has size %d, want 64", size)
	}
	value := windows.Filetime{HighDateTime: 0xfedcba98, LowDateTime: 0x76543210}
	if got := filetimeTicks(value); got != 0xfedcba9876543210 {
		t.Fatalf("FILETIME lost unsigned counter bits: %x", got)
	}
}

func TestNativeErrorWithoutLastErrorIsHonest(t *testing.T) {
	for _, err := range []error{nil, syscall.Errno(0)} {
		got := nativeReadError("GetSystemTimes", err)
		if !strings.Contains(got.Error(), "failed without an error code") {
			t.Fatalf("failure misrepresented as success: %v", got)
		}
	}
	if err := nativeReadError("GetSystemTimes", windows.ERROR_ACCESS_DENIED); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("native error identity lost: %v", err)
	}
}

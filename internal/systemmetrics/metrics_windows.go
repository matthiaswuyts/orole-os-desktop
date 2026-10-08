//go:build windows

package systemmetrics

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	getSystemTimes       = kernel32.NewProc("GetSystemTimes")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	getTickCount64       = kernel32.NewProc("GetTickCount64")
)

// memoryStatusEx matches MEMORYSTATUSEX, including the reserved final field.
type memoryStatusEx struct {
	length                   uint32
	memoryLoad               uint32
	totalPhysical            uint64
	availablePhysical        uint64
	totalPageFile            uint64
	availablePageFile        uint64
	totalVirtual             uint64
	availableVirtual         uint64
	availableExtendedVirtual uint64
}

func readHost(dataDir string) (hostReading, error) {
	for _, proc := range [...]*windows.LazyProc{getSystemTimes, globalMemoryStatusEx, getTickCount64} {
		if err := proc.Find(); err != nil {
			return hostReading{}, fmt.Errorf("host metrics: load %s: %w", proc.Name, err)
		}
	}

	// GetSystemTimes only covers the calling thread's primary processor group
	// on hosts with more than 64 processors. Never label a partial group as host CPU.
	logicalCPUs := windows.GetActiveProcessorCount(0xffff) // ALL_PROCESSOR_GROUPS
	if logicalCPUs == 0 {
		return hostReading{}, errors.New("host metrics: GetActiveProcessorCount returned no processors")
	}
	if logicalCPUs > 64 {
		return hostReading{}, fmt.Errorf("%w: GetSystemTimes cannot sample all processor groups on a %d-processor host", ErrUnsupported, logicalCPUs)
	}

	var idle, kernel, user windows.Filetime
	ok, _, err := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return hostReading{}, nativeReadError("GetSystemTimes", err)
	}
	memory := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	ok, _, err = globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&memory)))
	if ok == 0 {
		return hostReading{}, nativeReadError("GlobalMemoryStatusEx", err)
	}
	if memory.totalPhysical == 0 || memory.availablePhysical > memory.totalPhysical {
		return hostReading{}, errors.New("host metrics: GlobalMemoryStatusEx returned invalid physical memory counters")
	}

	path, err := windows.UTF16PtrFromString(dataDir)
	if err != nil {
		return hostReading{}, fmt.Errorf("host metrics: encode daemon data directory: %w", err)
	}
	var available, total uint64
	if err := windows.GetDiskFreeSpaceEx(path, &available, &total, nil); err != nil {
		return hostReading{}, fmt.Errorf("host metrics: GetDiskFreeSpaceEx for daemon data volume: %w", err)
	}
	if total == 0 || available > total {
		return hostReading{}, errors.New("host metrics: GetDiskFreeSpaceEx returned invalid data volume counters")
	}

	// Unlike BOOL APIs, GetTickCount64 has no failure return or last-error contract.
	low, high, _ := getTickCount64.Call()
	uptimeMillis := uint64(low)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		uptimeMillis |= uint64(high) << 32
	}
	return hostReading{
		metrics: HostMetrics{
			LogicalCPUs:          int(logicalCPUs),
			MemoryTotalBytes:     memory.totalPhysical,
			MemoryAvailableBytes: memory.availablePhysical,
			UptimeSeconds:        uptimeMillis / 1000,
			DiskTotalBytes:       total,
			DiskAvailableBytes:   available,
		},
		cpu: cpuCounters{idle: filetimeTicks(idle), kernel: filetimeTicks(kernel), user: filetimeTicks(user)},
	}, nil
}

func filetimeTicks(value windows.Filetime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}

func nativeReadError(api string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("host metrics: %s failed without an error code", api)
	}
	return fmt.Errorf("host metrics: %s: %w", api, err)
}

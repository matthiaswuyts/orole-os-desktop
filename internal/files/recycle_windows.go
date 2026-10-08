package files

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	recycleClass      = windows.GUID{Data1: 0x3ad05575, Data2: 0x8857, Data3: 0x4850, Data4: [8]byte{0x92, 0x77, 0x11, 0xb8, 0x5b, 0xdb, 0x8e, 0x09}}
	fileOperationIID  = windows.GUID{Data1: 0x947aab5f, Data2: 0x0a5c, Data3: 0x4c13, Data4: [8]byte{0xb4, 0xd6, 0x4b, 0xf7, 0x83, 0x6f, 0xc9, 0xf8}}
	shellItemIID      = windows.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee, Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
	coCreate          = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")
	shellItemFromPath = windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")
)

type shellCOMObject struct{ vtable *[23]uintptr }

func (o *shellCOMObject) invoke(index int, args ...uintptr) uintptr {
	var values [4]uintptr
	values[0] = uintptr(unsafe.Pointer(o))
	copy(values[1:], args)
	result, _, _ := syscall.SyscallN(o.vtable[index], values[:len(args)+1]...)
	return result
}
func shellResult(hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("%w: Windows shell HRESULT %#x", ErrNoRecycleBin, uint32(hr))
	}
	return nil
}

// recycle uses IFileOperation's Windows 8+ RECYCLEONDELETE flag rather than
// SHFileOperation's best-effort ALLOWUNDO, which may permanently delete items.
// No failure path retries with permanent deletion. Network/removable volumes
// require a separately confirmed permanent delete.
func recycle(path string) error {
	volume := filepathVolumeRoot(path)
	if volume == "" {
		return fmt.Errorf("%w: path is not on a local fixed drive", ErrNoRecycleBin)
	}
	drive, err := windows.UTF16PtrFromString(volume)
	if err != nil {
		return err
	}
	if windows.GetDriveType(drive) != driveFixed {
		return fmt.Errorf("%w: volume has no supported Recycle Bin", ErrNoRecycleBin)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if err != nil && err != syscall.Errno(1) {
		return fmt.Errorf("initialize recycling: %w", err)
	}
	defer windows.CoUninitialize()
	var operation *shellCOMObject
	hr, _, _ := coCreate.Call(uintptr(unsafe.Pointer(&recycleClass)), 0, 1, uintptr(unsafe.Pointer(&fileOperationIID)), uintptr(unsafe.Pointer(&operation)))
	if err := shellResult(hr); err != nil {
		return err
	}
	defer operation.invoke(2)
	// IFileOperation vtable indices: SetOperationFlags=5, DeleteItem=18,
	// PerformOperations=21, GetAnyOperationsAborted=22 (IUnknown occupies 0..2).
	const flags = 0x00080000 | 0x20000000 | 0x00100000 | 0x00800000 | 0x0400 | 0x0010 | 0x0004
	if err := shellResult(operation.invoke(5, flags)); err != nil {
		return err
	}
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var item *shellCOMObject
	hr, _, _ = shellItemFromPath.Call(uintptr(unsafe.Pointer(wide)), 0, uintptr(unsafe.Pointer(&shellItemIID)), uintptr(unsafe.Pointer(&item)))
	if err := shellResult(hr); err != nil {
		return err
	}
	defer item.invoke(2)
	if err := shellResult(operation.invoke(18, uintptr(unsafe.Pointer(item)), 0)); err != nil {
		return err
	}
	if err := shellResult(operation.invoke(21)); err != nil {
		return err
	}
	var aborted int32
	if err := shellResult(operation.invoke(22, uintptr(unsafe.Pointer(&aborted)))); err != nil {
		return err
	}
	if aborted != 0 {
		return fmt.Errorf("%w: recycle operation was aborted", ErrNoRecycleBin)
	}
	return nil
}

//go:build windows

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/setupcode"
)

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
	swHide                = 0
)

// shellExecuteInfo is SHELLEXECUTEINFOW (112 bytes on 64-bit Windows).
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.Handle
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

// runElevatedFn is swapped by tests so no UAC prompt is ever raised from a unit test.
var runElevatedFn = runElevated

// EnableWHP runs orole-sandbox-setup.exe elevated (one UAC prompt) and waits for it. When Windows must
// restart, it writes the reboot marker so Availability says "reboot-required" until it has.
func EnableWHP(ctx context.Context, helper string, p Paths) (SetupResult, error) {
	if helper == "" {
		return SetupResult{}, ErrSetupHelperMissing
	}
	if _, err := os.Stat(helper); err != nil {
		return SetupResult{}, ErrSetupHelperMissing
	}
	code, err := runElevatedFn(ctx, helper, setupcode.EnableWHPArg)
	if err != nil {
		return SetupResult{}, err
	}
	res, err := setupOutcome(code)
	if err == nil && res.RebootRequired {
		if mkErr := os.MkdirAll(p.Root, 0o700); mkErr == nil {
			_ = os.WriteFile(rebootMarkerPath(p), rebootMarker(bootTime()), 0o600)
		}
	}
	return res, err
}

func runElevated(ctx context.Context, exe, params string) (uint32, error) {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	args, _ := windows.UTF16PtrFromString(params)
	info := shellExecuteInfo{fMask: seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI, lpVerb: verb, lpFile: file, lpParameters: args, nShow: swHide}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, ErrSetupDeclined
		}
		return 0, fmt.Errorf("sandbox: could not start the setup helper: %w", callErr)
	}
	if info.hProcess == 0 {
		return 0, errors.New("sandbox: the setup helper started without a process handle")
	}
	type result struct {
		code uint32
		err  error
	}
	res := make(chan result, 1)
	go func(h windows.Handle) {
		defer windows.CloseHandle(h)
		if _, err := windows.WaitForSingleObject(h, windows.INFINITE); err != nil {
			res <- result{err: err}
			return
		}
		var code uint32
		err := windows.GetExitCodeProcess(h, &code)
		res <- result{code, err}
	}(info.hProcess)
	select {
	case r := <-res:
		return r.code, r.err
	case <-ctx.Done():
		return 0, ctx.Err() // the helper keeps running; the next availability check sees its result
	}
}

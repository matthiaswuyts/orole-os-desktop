//go:build windows

package sandbox

import (
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var procGetKeyboardLayoutNameW = windows.NewLazySystemDLL("user32.dll").NewProc("GetKeyboardLayoutNameW")

// HostKeymap is the XKB layout(variant) for the user's keyboard layout, so the bot's computer types what the user's
// keys say. A daemon has no window and no foreground thread, so it reads the user's default input layout
// (HKCU\Keyboard Layout\Preload\1, resolved through Substitutes, no admin needed) and falls back to the layout
// Windows gave its own thread, which starts as that same default.
func HostKeymap() string {
	if klid := preloadKLID(); klid != "" {
		return KeymapForKLID(klid)
	}
	return KeymapForKLID(threadKLID())
}

// preloadKLID reads the first preloaded layout, replaced by its substitute when one is set.
func preloadKLID() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Keyboard Layout\Preload`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	klid, _, err := k.GetStringValue("1")
	k.Close()
	if err != nil || klid == "" {
		return ""
	}
	if s, err := registry.OpenKey(registry.CURRENT_USER, `Keyboard Layout\Substitutes`, registry.QUERY_VALUE); err == nil {
		if sub, _, err := s.GetStringValue(klid); err == nil && sub != "" {
			klid = sub
		}
		s.Close()
	}
	return klid
}

// threadKLID is GetKeyboardLayoutName for the calling thread (KL_NAMELENGTH is 9 characters).
func threadKLID() string {
	var buf [9]uint16
	if r, _, _ := procGetKeyboardLayoutNameW.Call(uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:])
}

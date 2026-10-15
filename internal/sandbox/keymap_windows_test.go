//go:build windows

package sandbox

import "testing"

func TestHostKeymapReadsTheUsersLayout(t *testing.T) {
	k := HostKeymap()
	t.Logf("preload %q, thread %q -> %q", preloadKLID(), threadKLID(), k)
	if !ValidKeymap(k) {
		t.Fatalf("HostKeymap() = %q, not a valid keymap", k)
	}
	if threadKLID() == "" && preloadKLID() == "" {
		t.Fatal("neither the registry nor GetKeyboardLayoutName gave a layout")
	}
}

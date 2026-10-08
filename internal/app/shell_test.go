package app

import "testing"

func TestExternalURLPolicy(t *testing.T) {
	for _, raw := range []string{"https://example.com/path?q=1", "http://localhost:5173/", "mailto:owner@example.com?subject=Hello"} {
		if err := validateExternalURL(raw); err != nil {
			t.Errorf("rejected %q: %v", raw, err)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "file:///C:/Windows/notepad.exe", "shell:AppsFolder", "data:text/html,test", "C:\\Windows\\notepad.exe", "https:///path", "https://user:secret@example.com", "https://example.com\n.exe", "mailto:"} {
		if err := validateExternalURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

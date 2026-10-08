//go:build !windows

package platform

// WebView2Version is Windows-only; other platforms report "".
func WebView2Version() string { return "" }

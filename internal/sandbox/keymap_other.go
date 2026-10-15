//go:build !windows

package sandbox

// HostKeymap is DefaultKeymap off Windows, where there is no bot computer backend.
func HostKeymap() string { return DefaultKeymap }

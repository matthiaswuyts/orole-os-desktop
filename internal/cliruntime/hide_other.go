//go:build !windows

package cliruntime

import "os/exec"

// HideWindow keeps a console window from flashing up for a background CLI call (Windows only).
func HideWindow(*exec.Cmd) {}

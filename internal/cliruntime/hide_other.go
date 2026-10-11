//go:build !windows

package cliruntime

import "os/exec"

func hide(*exec.Cmd) {}

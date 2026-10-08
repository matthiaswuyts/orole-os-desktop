//go:build !windows

package agents_test

import "syscall"

func pidAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func checkFileCommand() string { return "test -f ok.txt" }
func longCheckCommand() string { return "sleep 30" }

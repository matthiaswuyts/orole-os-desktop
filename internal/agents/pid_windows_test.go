//go:build windows

package agents_test

import "golang.org/x/sys/windows"

// pidAlive reports whether a process with this pid is still running.
func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false // gone (or never ours)
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}

func checkFileCommand() string { return "if exist ok.txt (exit 0) else (exit 1)" }
func longCheckCommand() string { return "ping -n 30 127.0.0.1" }

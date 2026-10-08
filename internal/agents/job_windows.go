//go:build windows

package agents

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// job is a Windows job object with KILL_ON_JOB_CLOSE: when the daemon exits (or crashes) the
// OS closes the handle and kills every process in it, so agents never outlive the daemon.
type job struct {
	mu sync.Mutex
	h  windows.Handle
}

func newJob() (*job, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &job{h: h}, nil
}

// assign puts the process with this pid (and its future children) into the job.
func (j *job) assign(pid int) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h == 0 {
		return errors.New("job closed")
	}
	const access = windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE
	ph, err := windows.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(j.h, ph)
}

// kill terminates every process in the job.
func (j *job) kill() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h != 0 {
		_ = windows.TerminateJobObject(j.h, 1)
	}
}

// close releases the handle; with KILL_ON_JOB_CLOSE this also kills remaining processes.
func (j *job) close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h != 0 {
		windows.CloseHandle(j.h)
		j.h = 0
	}
}

// shellWrap turns a user-written command line into a CreateProcess command line.
func shellWrap(cmd string) string { return `cmd.exe /d /s /c "` + cmd + `"` }

// shellCommand builds a command that runs a user-written command line through cmd.exe.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: `cmd.exe /d /s /c "` + command + `"`, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return cmd
}

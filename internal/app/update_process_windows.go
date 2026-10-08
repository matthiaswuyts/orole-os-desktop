package app

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
)

// Hold the original process handle before requesting shutdown. Pipe closure does
// not prove Windows has released the daemon executable for replacement.
type updateProcess windows.Handle

func trackUpdateProcess(pid int) (updateProcess, error) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	return updateProcess(h), err
}

func (p updateProcess) Close() { _ = windows.CloseHandle(windows.Handle(p)) }

func (p updateProcess) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state, err := windows.WaitForSingleObject(windows.Handle(p), 100)
		if err != nil {
			return err
		}
		switch state {
		case windows.WAIT_OBJECT_0:
			return nil
		case uint32(windows.WAIT_TIMEOUT):
		default:
			return fmt.Errorf("unexpected daemon wait result: %d", state)
		}
	}
}

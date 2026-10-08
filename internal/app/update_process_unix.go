//go:build !windows

package app

import (
	"context"
	"errors"
	"syscall"
	"time"
)

type updateProcess int

func trackUpdateProcess(pid int) (updateProcess, error) {
	if err := syscall.Kill(pid, 0); err != nil {
		return 0, err
	}
	return updateProcess(pid), nil
}

func (p updateProcess) Close() {}

func (p updateProcess) Wait(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Kill(int(p), 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

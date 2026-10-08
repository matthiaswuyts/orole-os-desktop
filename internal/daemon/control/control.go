// Package control lets the desktop app and CLI find, start, stop and
// diagnose orole-daemon.
package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

// NewClient builds a client for the daemon in dataDir. It reads the token on
// every dial so it keeps working across daemon restarts (new token each run).
func NewClient(dataDir, name string) *ipc.Client {
	return ipc.NewClientFunc(ipc.Dial(ipc.Endpoint(dataDir)), func() string { t, _ := daemon.ReadToken(dataDir); return t }, name)
}

// Running reports whether a daemon answers hello and returns its health.
func Running(ctx context.Context, dataDir string) (*daemon.Health, error) {
	c := NewClient(dataDir, "probe")
	defer c.Close()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		return nil, err
	}
	var h daemon.Health
	if err := c.Call(ctx, "daemon.health", struct{}{}, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// Ensure returns a connected client, starting the daemon (found next to the
// current executable, or via daemonExe) if it is not running.
func Ensure(ctx context.Context, dataDir, daemonExe, name string) (*ipc.Client, error) {
	if _, err := Running(ctx, dataDir); err != nil {
		if daemonExe == "" {
			daemonExe = FindDaemonExe()
		}
		if err := StartDetached(daemonExe, dataDir); err != nil {
			return nil, fmt.Errorf("start daemon: %w", err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := Running(ctx, dataDir); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return nil, errors.New("daemon did not become ready within 10s (see logs/daemon.log)")
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(150 * time.Millisecond):
			}
		}
	}
	c := NewClient(dataDir, name)
	return c, c.Connect(ctx)
}

// FindDaemonExe locates orole-daemon next to the running executable, else on PATH.
func FindDaemonExe() string {
	name := "orole-daemon"
	if exe := exeSuffix(); exe != "" {
		name += exe
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

// Stop asks a running daemon to exit and waits for the endpoint to close.
func Stop(ctx context.Context, dataDir string) error {
	c := NewClient(dataDir, "cli")
	defer c.Close()
	if err := c.Connect(ctx); err != nil {
		return err
	}
	if err := c.Call(ctx, "daemon.shutdown", struct{}{}, nil); err != nil {
		return err
	}
	for i := 0; i < 50; i++ {
		if _, err := Running(ctx, dataDir); err != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("daemon still running after 5s")
}

// NewClientEndpoint returns the endpoint name used for dataDir.
func NewClientEndpoint(dataDir string) string { return ipc.Endpoint(dataDir) }

//go:build !windows

package ipc

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

// Endpoint returns the per-user unix socket path inside dataDir (mode 0700).
func Endpoint(dataDir string) string { return filepath.Join(dataDir, "daemon.sock") }

// Listen creates a unix socket readable only by the current user.
func Listen(endpoint string) (net.Listener, error) {
	_ = os.Remove(endpoint)
	ln, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	return ln, os.Chmod(endpoint, 0o600)
}

// Dial connects to the socket.
func Dial(endpoint string) Dialer {
	return func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", endpoint)
	}
}

//go:build windows

package ipc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/Microsoft/go-winio"
)

// Endpoint returns the per-user pipe name.
func Endpoint(dataDir string) string {
	u, err := user.Current()
	name := "user"
	if err == nil {
		name = u.Uid // SID: unique per user, no path-unsafe characters
	}
	if absolute, err := filepath.Abs(dataDir); err == nil {
		dataDir = absolute
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(dataDir))))
	// SID isolates users; the dataDir hash lets one user run isolated instances (dev, tests).
	return `\\.\pipe\orole-daemon-` + name + "-" + hex.EncodeToString(sum[:6])
}

// Listen creates a named pipe whose DACL grants access only to the current
// user (and rejects remote clients), then returns its listener.
func Listen(endpoint string) (net.Listener, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	// D:P = protected DACL (no inheritance); single ACE: generic-all for the current user SID.
	sddl := fmt.Sprintf("D:P(A;;GA;;;%s)", u.Uid)
	return winio.ListenPipe(endpoint, &winio.PipeConfig{SecurityDescriptor: sddl, MessageMode: false, InputBufferSize: 64 << 10, OutputBufferSize: 64 << 10})
}

// Dial connects to the pipe.
func Dial(endpoint string) Dialer {
	return func(ctx context.Context) (net.Conn, error) { return winio.DialPipeContext(ctx, endpoint) }
}

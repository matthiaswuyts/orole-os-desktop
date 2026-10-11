// Package api holds the daemon's wire-level types and token location. It is a
// leaf package (stdlib only) so clients such as orole-cli and diagnostics can
// use it without linking the daemon and its sqlite/ssh/MCP dependencies.
package api

import (
	"os"
	"path/filepath"
	"time"
)

// Health is the daemon's status snapshot.
type Health struct {
	Version     string    `json:"version"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"startedAt"`
	UptimeSec   int64     `json:"uptimeSec"`
	DBVersion   int       `json:"dbVersion"`
	Platform    string    `json:"platform"`
	GoVersion   string    `json:"goVersion"`
	Goroutines  int       `json:"goroutines"`
	MemAllocMB  float64   `json:"memAllocMB"`
	Terminals   int       `json:"terminals"`
	ProtocolVer int       `json:"protocolVersion"`
}

// TokenPath returns where the IPC auth token lives.
func TokenPath(dataDir string) string { return filepath.Join(dataDir, "ipc.token") }

// ReadToken loads the token written by a running daemon.
func ReadToken(dataDir string) (string, error) {
	b, err := os.ReadFile(TokenPath(dataDir))
	return string(b), err
}

// Package diagnostics gathers the support snapshot printed by `orole daemon
// diagnostics` and shown in Settings > Diagnostics. It never reads secret
// values: the credential check writes, reads and deletes a throwaway key.
package diagnostics

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/daemon/api"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/platform"
)

// Report is the diagnostics snapshot.
type Report struct {
	AppVersion      string                   `json:"appVersion"`
	GoVersion       string                   `json:"goVersion"`
	Platform        string                   `json:"platform"`
	DataDir         string                   `json:"dataDir"`
	Endpoint        string                   `json:"endpoint"`
	LogPath         string                   `json:"logPath"`
	DaemonExe       string                   `json:"daemonExe"`
	Daemon          *api.Health              `json:"daemon,omitempty"`
	DaemonError     string                   `json:"daemonError,omitempty"`
	DBSchemaVersion int                      `json:"dbSchemaVersion"` // -1 = unknown
	DBSource        string                   `json:"dbSource"`
	CredentialStore Check                    `json:"credentialStore"`
	WebView2        string                   `json:"webview2"`
	Autostart       platform.AutostartStatus `json:"autostart"`
	LogTail         []string                 `json:"logTail"`
}

// Check is a pass/fail probe with detail.
type Check struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Collect builds a Report for dataDir. logLines is the number of log lines to include.
// readSchema reads the schema version straight from orole.db when the daemon is down;
// nil (the CLI, which must not link sqlite) reports the version as unavailable.
func Collect(ctx context.Context, appVersion, dataDir, daemonExe string, logLines int, readSchema func(path string) (int, error)) Report {
	r := Report{
		AppVersion: appVersion, GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
		DataDir: dataDir, Endpoint: control.NewClientEndpoint(dataDir), DaemonExe: daemonExe,
		LogPath: filepath.Join(dataDir, "logs", "daemon.log"), DBSchemaVersion: -1,
		CredentialStore: CheckCredentials(), WebView2: platform.WebView2Version(),
	}
	if r.WebView2 == "" {
		r.WebView2 = "not found"
	}
	if st, err := platform.NewAutostart().Status(ctx); err == nil {
		r.Autostart = st
	} else {
		r.Autostart = platform.AutostartStatus{Supported: true, Detail: err.Error()}
	}
	if h, err := control.Running(ctx, dataDir); err == nil {
		r.Daemon = h
		r.DBSchemaVersion, r.DBSource = h.DBVersion, "daemon"
	} else {
		r.DaemonError = err.Error()
		if readSchema == nil {
			r.DBSource = "unavailable (daemon not running)"
		} else if v, err := readSchema(filepath.Join(dataDir, "orole.db")); err == nil {
			r.DBSchemaVersion, r.DBSource = v, "orole.db (read-only)"
		} else {
			r.DBSource = err.Error()
		}
	}
	r.LogTail, _ = TailFile(r.LogPath, logLines)
	return r
}

// CheckCredentials round-trips a throwaway secret through the OS credential store.
func CheckCredentials() Check {
	s := credentials.New()
	const key, val = "diagnostics/probe", "ok"
	if err := s.Set(key, []byte(val)); err != nil {
		return Check{Detail: err.Error()}
	}
	defer s.Delete(key)
	got, err := s.Get(key)
	if err != nil {
		return Check{Detail: err.Error()}
	}
	if string(got) != val {
		return Check{Detail: "read back a different value"}
	}
	return Check{OK: true, Detail: "write/read/delete round trip succeeded"}
}

// TailFile returns the last n lines of path without loading huge files whole.
func TailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const window = 256 << 10
	off := st.Size() - window
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if off > 0 { // drop the partial first line
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, sc.Err()
}

// Text renders the report for terminals and support requests.
func (r Report) Text() string {
	var b strings.Builder
	w := func(k string, v any) { fmt.Fprintf(&b, "%-18s %v\n", k+":", v) }
	w("orole", r.AppVersion)
	w("platform", r.Platform)
	w("go", r.GoVersion)
	w("data dir", r.DataDir)
	w("pipe", r.Endpoint)
	w("daemon exe", r.DaemonExe)
	if r.Daemon != nil {
		w("daemon", fmt.Sprintf("running v%s pid %d up %ds, %.1f MB, %d goroutines, %d terminals, protocol %d",
			r.Daemon.Version, r.Daemon.PID, r.Daemon.UptimeSec, r.Daemon.MemAllocMB, r.Daemon.Goroutines, r.Daemon.Terminals, r.Daemon.ProtocolVer))
	} else {
		w("daemon", "not running ("+r.DaemonError+")")
	}
	if r.DBSchemaVersion >= 0 {
		w("db schema", fmt.Sprintf("%d (from %s)", r.DBSchemaVersion, r.DBSource))
	} else {
		w("db schema", "unknown ("+r.DBSource+")")
	}
	if r.CredentialStore.OK {
		w("credential store", "ok: "+r.CredentialStore.Detail)
	} else {
		w("credential store", "FAILED: "+r.CredentialStore.Detail)
	}
	w("webview2", r.WebView2)
	w("autostart", fmt.Sprintf("enabled=%v via %s", r.Autostart.Enabled, r.Autostart.Mechanism))
	fmt.Fprintf(&b, "log (%s), last %d lines:\n", r.LogPath, len(r.LogTail))
	for _, l := range r.LogTail {
		fmt.Fprintln(&b, "  "+l)
	}
	return b.String()
}

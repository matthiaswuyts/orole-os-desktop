package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	oroleos "github.com/rayorole/orole-os-desktop"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/diagnostics"
	"github.com/rayorole/orole-os-desktop/internal/platform"
	"github.com/rayorole/orole-os-desktop/internal/systemmetrics"
)

// System binds desktop OS integration and stateless access to daemon host metrics.
// Autostart and diagnostics remain desktop concerns; metrics come from the daemon.
type System struct{ L *Link }

// AutostartStatus reports whether the daemon starts when the user signs in.
func (s System) AutostartStatus(ctx context.Context) (platform.AutostartStatus, error) {
	return platform.NewAutostart().Status(ctx)
}

// EnableAutostart registers the packaged orole-daemon to start at logon (no admin rights).
func (s System) EnableAutostart(ctx context.Context) (platform.AutostartStatus, error) {
	exe := s.L.DaemonExe
	if exe == "" {
		return platform.AutostartStatus{}, errors.New("daemon executable location unknown (dev build); install the packaged app or run `orole daemon install`")
	}
	exe, err := filepath.Abs(exe)
	if err != nil {
		return platform.AutostartStatus{}, err
	}
	if _, err := os.Stat(exe); err != nil {
		return platform.AutostartStatus{}, err
	}
	a := platform.NewAutostart()
	if err := a.Enable(ctx, exe); err != nil {
		return platform.AutostartStatus{}, err
	}
	return a.Status(ctx)
}

// DisableAutostart removes the logon registration.
func (s System) DisableAutostart(ctx context.Context) (platform.AutostartStatus, error) {
	a := platform.NewAutostart()
	if err := a.Disable(ctx); err != nil {
		return platform.AutostartStatus{}, err
	}
	return a.Status(ctx)
}

// Diagnostics returns the support snapshot (versions, paths, daemon, credential store, WebView2, log tail).
func (s System) Diagnostics(ctx context.Context) (diagnostics.Report, error) {
	return diagnostics.Collect(ctx, oroleos.Version, s.L.DataDir, s.L.DaemonExe, 30, database.ReadSchemaVersion), nil
}

// Metrics returns a real host observation sampled and cached by the daemon.
func (s System) Metrics(ctx context.Context) (systemmetrics.HostMetrics, error) {
	var out systemmetrics.HostMetrics
	err := s.L.call(ctx, "system.metrics", struct{}{}, &out)
	return out, err
}

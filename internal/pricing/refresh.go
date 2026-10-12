package pricing

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// RefreshInterval is the least time between two models.dev downloads.
const RefreshInterval = 24 * time.Hour

const (
	cacheFile   = "models-dev.json" // trimmed snapshot of the last successful download
	attemptFile = "last-attempt"    // its modification time is the last download attempt
	maxDocument = 64 << 20
)

// Refresher keeps a Catalog current from models.dev. It never downloads more than once per
// RefreshInterval (attempts included, across restarts) and keeps the newest catalogue it has
// (cache file or embedded snapshot) whenever a download, parse or write fails.
type Refresher struct {
	Catalog *Catalog
	Dir     string        // cache directory, created on demand
	URL     string        // default SourceURL
	Client  *http.Client  // default: a client with Timeout
	Timeout time.Duration // per download; default 60 s
	Delay   time.Duration // before the first download check
	Log     *slog.Logger
}

// LoadCache swaps in the cached download when it is newer than the catalogue in use.
func (r *Refresher) LoadCache() {
	data, err := os.ReadFile(filepath.Join(r.Dir, cacheFile))
	if err != nil {
		return
	}
	if _, err := r.Catalog.replace(data); err != nil {
		r.log().Warn("cached model prices are unreadable; using the built-in snapshot", "err", err)
	}
}

// Run checks hourly whether a download is due until ctx ends.
func (r *Refresher) Run(ctx context.Context) {
	wait := r.Delay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if r.due(time.Now()) {
			if err := r.Refresh(ctx); err != nil && ctx.Err() == nil {
				r.log().Warn("model price refresh failed; keeping the current catalogue", "err", err)
			}
		}
		wait = time.Hour
	}
}

// due reports whether neither the catalogue nor the last attempt is younger than RefreshInterval.
func (r *Refresher) due(now time.Time) bool {
	if now.Sub(r.Catalog.UpdatedAt()) < RefreshInterval {
		return false
	}
	if st, err := os.Stat(filepath.Join(r.Dir, attemptFile)); err == nil && now.Sub(st.ModTime()) < RefreshInterval {
		return false
	}
	return true
}

// Refresh downloads the catalogue once, records the attempt, and caches and swaps in the result.
func (r *Refresher) Refresh(ctx context.Context) error {
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Dir, attemptFile), []byte(time.Now().UTC().Format(time.RFC3339)), 0o600); err != nil {
		return err
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	url := r.URL
	if url == "" {
		url = SourceURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models.dev: %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDocument))
	if err != nil {
		return err
	}
	trimmed, err := Trim(raw, time.Now())
	if err != nil {
		return err
	}
	if _, err := r.Catalog.replace(trimmed); err != nil {
		return err
	}
	tmp := filepath.Join(r.Dir, cacheFile+".tmp")
	if err := os.WriteFile(tmp, trimmed, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(r.Dir, cacheFile))
}

func (r *Refresher) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

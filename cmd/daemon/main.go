// Command orole-daemon is the headless per-user background controller.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/paths"
)

func main() {
	var verbose bool
	var stderr bool
	flag.BoolVar(&verbose, "v", false, "debug logging")
	flag.BoolVar(&stderr, "stderr", false, "also log to stderr")
	flag.Parse()

	dir, err := paths.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "orole-daemon:", err)
		os.Exit(1)
	}
	logDir := filepath.Join(dir, "logs")
	_ = os.MkdirAll(logDir, 0o700)
	lf, err := os.OpenFile(filepath.Join(logDir, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "orole-daemon:", err)
		os.Exit(1)
	}
	defer lf.Close()
	var w io.Writer = lf
	if stderr {
		w = io.MultiWriter(lf, os.Stderr)
	}
	lvl := slog.LevelInfo
	if verbose {
		lvl = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl}))

	d, err := daemon.New(dir, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		fmt.Fprintln(os.Stderr, "orole-daemon:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := d.Run(ctx); err != nil {
		log.Error("daemon exited with error", "err", err)
		fmt.Fprintln(os.Stderr, "orole-daemon:", err)
		os.Exit(1)
	}
}

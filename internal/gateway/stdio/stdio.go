// Package stdio is `orole-daemon mcp-gateway`: a stdio MCP server a harness launches. It connects
// to the running daemon over the authenticated per-user pipe (the same client the CLI and hooks
// use), serves the central skills and proxies the daemon's MCP tools. The daemon decides every
// tool call (policy, approval, audit); this process holds no secrets and no authority.
//
// OROLE_HOME selects the data directory like it does for the daemon; OROLE_GATEWAY_SESSION names
// the agent session the harness belongs to, so calls run as actor agent:<session>. stdout carries
// only MCP protocol; diagnostics go to stderr.
package stdio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/gateway"
	"github.com/rayorole/orole-os-desktop/internal/paths"
)

// Main handles `mcp-gateway` (args exclude the subcommand name) and returns the exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	dir, err := paths.DataDir()
	if err != nil {
		fmt.Fprintln(stderr, "orole-daemon mcp-gateway:", err)
		return 1
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c := control.NewClient(dir, "mcp-gateway")
	defer c.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	refresh := gateway.WatchEvents(ctx, c)

	srv := gateway.NewServer(gateway.NewIPCBackend(c, os.Getenv(gateway.EnvSession)), daemon.Version, log)
	t := &sdk.IOTransport{Reader: io.NopCloser(stdin), Writer: nopWriteCloser{stdout}}
	if err := srv.Run(ctx, t, refresh); err != nil && ctx.Err() == nil {
		fmt.Fprintln(stderr, "orole-daemon mcp-gateway:", err)
		return 1
	}
	return 0
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

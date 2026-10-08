// Package hook is the tiny client Claude Code hooks run: `orole-daemon agent-hook <session>
// <Event>` reads the hook's JSON from stdin and relays it to the daemon over authenticated
// IPC. It prints nothing and always exits 0, so a missing or busy daemon can never change
// how Claude Code behaves.
package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/paths"
)

// Main handles `agent-hook <sessionID> <Event>` (args exclude the subcommand name).
func Main(args []string, stdin io.Reader) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "orole-daemon agent-hook: usage: agent-hook <session-id> <event>")
		return 0
	}
	dir, err := paths.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "orole-daemon agent-hook:", err)
		return 0
	}
	payload, _ := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if !json.Valid(payload) {
		payload = []byte("null")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := control.NewClient(dir, "agent-hook")
	defer c.Close()
	if err := c.Connect(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "orole-daemon agent-hook: daemon unreachable:", err)
		return 0
	}
	err = c.Call(ctx, "agents.hook", map[string]any{"sessionId": args[0], "event": args[1], "payload": json.RawMessage(payload)}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "orole-daemon agent-hook:", err)
	}
	return 0
}

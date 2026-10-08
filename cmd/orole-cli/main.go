// Command orole is the Orole OS command-line client for orole-daemon.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/diagnostics"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/paths"
	"github.com/rayorole/orole-os-desktop/internal/platform"
	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const usage = `orole - control the Orole OS daemon

usage:
  orole daemon start|stop|restart|status
  orole daemon logs [-n lines] [-f]
  orole daemon diagnostics [-json]
  orole daemon install       start the daemon at logon (Task Scheduler, no admin)
  orole daemon uninstall     remove the logon task
  orole terminal list
  orole terminal create [-tmux] [-shell id] [-cwd directory]
  orole terminal persistent
  orole terminal restore <persistent-id>
  orole terminal close <terminal-id>
  orole settings get [key]
  orole settings set <key> <json-or-text>
  orole version

environment:
  OROLE_HOME         data directory override (isolated dev instance)
  OROLE_DAEMON_EXE   path to orole-daemon (default: next to orole, then PATH)
`

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, errw io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(out, usage)
		return 0
	}
	dir, err := paths.DataDir()
	if err != nil {
		fmt.Fprintln(errw, "orole:", err)
		return 1
	}
	c := &cli{ctx: ctx, dir: dir, out: out, err: errw}
	var e error
	switch args[0] {
	case "version":
		fmt.Fprintln(out, "orole", version)
	case "daemon":
		e = c.daemon(args[1:])
	case "terminal":
		e = c.terminal(args[1:])
	case "settings":
		e = c.settings(args[1:])
	default:
		fmt.Fprintf(errw, "orole: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if e != nil {
		var u usageError
		if errors.As(e, &u) {
			fmt.Fprintf(errw, "orole: %s\n\n%s", u, usage)
			return 2
		}
		fmt.Fprintln(errw, "orole:", e)
		return 1
	}
	return 0
}

type usageError string

func (u usageError) Error() string { return string(u) }

type cli struct {
	ctx      context.Context
	dir      string
	out, err io.Writer
}

func (c *cli) daemonExe() string {
	if p := os.Getenv("OROLE_DAEMON_EXE"); p != "" {
		return p
	}
	return control.FindDaemonExe()
}

func (c *cli) daemon(args []string) error {
	if len(args) == 0 {
		return usageError("daemon needs a subcommand")
	}
	rest := args[1:]
	switch args[0] {
	case "start":
		return c.start()
	case "stop":
		return c.stop()
	case "restart":
		if err := c.stop(); err != nil {
			return err
		}
		return c.start()
	case "status":
		return c.status()
	case "logs":
		return c.logs(rest)
	case "diagnostics":
		return c.diagnostics(rest)
	case "install":
		return c.install()
	case "uninstall":
		return c.uninstall()
	}
	return usageError("unknown daemon subcommand " + args[0])
}

func (c *cli) start() error {
	if h, err := control.Running(c.ctx, c.dir); err == nil {
		fmt.Fprintf(c.out, "daemon already running (pid %d, v%s)\n", h.PID, h.Version)
		return nil
	}
	cl, err := control.Ensure(c.ctx, c.dir, c.daemonExe(), "cli")
	if err != nil {
		return err
	}
	cl.Close()
	h, err := control.Running(c.ctx, c.dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "daemon started (pid %d, v%s)\n", h.PID, h.Version)
	return nil
}

func (c *cli) stop() error {
	if _, err := control.Running(c.ctx, c.dir); err != nil {
		fmt.Fprintln(c.out, "daemon not running")
		return nil
	}
	if err := control.Stop(c.ctx, c.dir); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "daemon stopped (terminals owned by the daemon ended)")
	return nil
}

func (c *cli) status() error {
	h, err := control.Running(c.ctx, c.dir)
	if err != nil {
		fmt.Fprintf(c.out, "daemon: not running\n  data dir: %s\n  reason:   %v\n", c.dir, err)
		return nil
	}
	fmt.Fprintf(c.out, "daemon: running\n  version:   %s (protocol %d)\n  pid:       %d\n  started:   %s (up %ds)\n  db schema: %d\n  platform:  %s, %s\n  memory:    %.1f MB heap, %d goroutines\n  terminals: %d\n  pipe:      %s\n  data dir:  %s\n",
		h.Version, h.ProtocolVer, h.PID, h.StartedAt.Local().Format(time.RFC3339), h.UptimeSec, h.DBVersion, h.Platform, h.GoVersion,
		h.MemAllocMB, h.Goroutines, h.Terminals, control.NewClientEndpoint(c.dir), c.dir)
	return nil
}

func (c *cli) logs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(c.err)
	n := fs.Int("n", 50, "lines to show")
	follow := fs.Bool("f", false, "follow new output")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	path := filepath.Join(c.dir, "logs", "daemon.log")
	lines, err := diagnostics.TailFile(path, *n)
	if err != nil {
		return err
	}
	for _, l := range lines {
		fmt.Fprintln(c.out, l)
	}
	if !*follow {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	off, _ := f.Seek(0, io.SeekEnd)
	for {
		select {
		case <-c.ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		if st.Size() < off { // rotated: reopen from the start
			f.Close()
			if f, err = os.Open(path); err != nil {
				return err
			}
			off = 0
		}
		n, _ := io.Copy(c.out, f)
		off += n
	}
}

func (c *cli) diagnostics(args []string) error {
	fs := flag.NewFlagSet("diagnostics", flag.ContinueOnError)
	fs.SetOutput(c.err)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	r := diagnostics.Collect(c.ctx, version, c.dir, c.daemonExe(), 20)
	if *asJSON {
		enc := json.NewEncoder(c.out)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	fmt.Fprint(c.out, r.Text())
	return nil
}

func (c *cli) install() error {
	exe, err := filepath.Abs(c.daemonExe())
	if err != nil {
		return err
	}
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("daemon executable not found at %s (set OROLE_DAEMON_EXE)", exe)
	}
	a := platform.NewAutostart()
	if err := a.Enable(c.ctx, exe); err != nil {
		return err
	}
	st, _ := a.Status(c.ctx)
	fmt.Fprintf(c.out, "autostart enabled via %s: %s\n  runs: %s\n", st.Mechanism, st.Detail, exe)
	return nil
}

func (c *cli) uninstall() error {
	a := platform.NewAutostart()
	if err := a.Disable(c.ctx); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "autostart removed")
	return nil
}

func (c *cli) connect() (*ipc.Client, error) {
	if _, err := control.Running(c.ctx, c.dir); err != nil {
		return nil, errors.New("daemon not running (try: orole daemon start)")
	}
	cl := control.NewClient(c.dir, "cli")
	if err := cl.Connect(c.ctx); err != nil {
		return nil, err
	}
	return cl, nil
}

func (c *cli) terminal(args []string) error {
	if len(args) == 0 {
		return usageError("terminal supports: list, create, persistent, restore, close")
	}
	var options terminals.CreateOptions
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("terminal create", flag.ContinueOnError)
		fs.SetOutput(c.err)
		tmux := fs.Bool("tmux", false, "use Linux tmux persistence")
		fs.StringVar(&options.Shell, "shell", "", "shell ID")
		fs.StringVar(&options.Cwd, "cwd", "", "working directory")
		if err := fs.Parse(args[1:]); err != nil {
			return usageError(err.Error())
		}
		if fs.NArg() != 0 {
			return usageError("terminal create accepts flags only")
		}
		if *tmux {
			options.Persistence = "tmux"
		}
	case "list", "persistent":
		if len(args) != 1 {
			return usageError("terminal " + args[0] + " accepts no arguments")
		}
	case "restore", "close":
		if len(args) != 2 {
			return usageError("terminal " + args[0] + " requires a session ID")
		}
	default:
		return usageError("terminal supports: list, create, persistent, restore, close")
	}
	cl, err := c.connect()
	if err != nil {
		return err
	}
	defer cl.Close()
	switch args[0] {
	case "create":
		var info terminals.Info
		if err := cl.Call(c.ctx, "terminals.create", options, &info); err != nil {
			return err
		}
		return json.NewEncoder(c.out).Encode(info)
	case "persistent":
		var sessions []terminals.PersistentSession
		if err := cl.Call(c.ctx, "terminals.persistent", struct{}{}, &sessions); err != nil {
			return err
		}
		return json.NewEncoder(c.out).Encode(sessions)
	case "restore":
		var info terminals.Info
		if err := cl.Call(c.ctx, "terminals.restore", map[string]string{"id": args[1]}, &info); err != nil {
			return err
		}
		return json.NewEncoder(c.out).Encode(info)
	case "close":
		return cl.Call(c.ctx, "terminals.close", map[string]string{"id": args[1]}, nil)
	}
	var list []terminals.Info
	if err := cl.Call(c.ctx, "terminals.list", struct{}{}, &list); err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(c.out, "no terminal sessions")
		return nil
	}
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSHELL\tTITLE\tSIZE\tSTATE")
	for _, t := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%dx%d\t%s\n", t.ID, t.Shell, t.Title, t.Cols, t.Rows, state(t))
	}
	return tw.Flush()
}

func (c *cli) settings(args []string) error {
	if len(args) == 0 {
		return usageError("settings needs get or set")
	}
	switch args[0] {
	case "get":
		if len(args) > 2 {
			return usageError("settings get [key]")
		}
		cl, err := c.connect()
		if err != nil {
			return err
		}
		defer cl.Close()
		all := map[string]json.RawMessage{}
		if err := cl.Call(c.ctx, "settings.list", struct{}{}, &all); err != nil {
			return err
		}
		if len(args) == 2 {
			v, ok := all[args[1]]
			if !ok {
				return fmt.Errorf("no setting %q", args[1])
			}
			fmt.Fprintln(c.out, string(v))
			return nil
		}
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(c.out, "%s = %s\n", k, all[k])
		}
		return nil
	case "set":
		if len(args) < 3 {
			return usageError("settings set <key> <json-or-text>")
		}
		raw := strings.Join(args[2:], " ")
		val := json.RawMessage(raw)
		if !json.Valid(val) {
			b, _ := json.Marshal(raw) // plain text becomes a JSON string
			val = b
		}
		cl, err := c.connect()
		if err != nil {
			return err
		}
		defer cl.Close()
		if err := cl.Call(c.ctx, "settings.set", map[string]any{"key": args[1], "value": val}, nil); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "%s = %s\n", args[1], val)
		return nil
	}
	return usageError("unknown settings subcommand " + args[0])
}

func state(t terminals.Info) string {
	if t.State == terminals.Exited && t.ExitCode != nil {
		return fmt.Sprintf("exited (%d)", *t.ExitCode)
	}
	return string(t.State)
}

//go:build windows

package terminals_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/terminals"
)

type capSink struct {
	mu      sync.Mutex
	out     strings.Builder
	changed []terminals.Info
}

func (c *capSink) Output(_ string, _ uint64, d []byte) { c.mu.Lock(); c.out.Write(d); c.mu.Unlock() }
func (c *capSink) Changed(i terminals.Info) {
	c.mu.Lock()
	c.changed = append(c.changed, i)
	c.mu.Unlock()
}
func (c *capSink) text() string { c.mu.Lock(); defer c.mu.Unlock(); return c.out.String() }

func waitFor(t *testing.T, f func() bool, what string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if f() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestShellsInteractive(t *testing.T) {
	for _, id := range []string{"powershell", "cmd"} {
		t.Run(id, func(t *testing.T) {
			sink := &capSink{}
			m := terminals.NewManager(terminals.NewBackend(), sink)
			defer m.Shutdown()
			info, err := m.Create(terminals.CreateOptions{Shell: id, Cols: 100, Rows: 30})
			if err != nil {
				t.Fatal(err)
			}
			// Unicode + interactive input + exit code reporting.
			if err := m.Write(info.ID, []byte("echo héllo-✓-orole\r")); err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool { return strings.Contains(sink.text(), "héllo-✓-orole") }, "echo output")
			if err := m.Resize(info.ID, 80, 24); err != nil {
				t.Fatal(err)
			}
			// Reconnect: scrollback must contain earlier output.
			att, err := m.Attach(info.ID)
			if err != nil || !strings.Contains(string(att.Data), "héllo-✓-orole") {
				t.Fatalf("attach: %v %q", err, att.Data)
			}
			if err := m.Write(info.ID, []byte("exit 7\r")); err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool {
				for _, i := range m.List() {
					if i.State == terminals.Exited && i.ExitCode != nil && *i.ExitCode == 7 {
						return true
					}
				}
				return false
			}, "exit code 7")
		})
	}
}

func TestCtrlCInterruptsForegroundProcess(t *testing.T) {
	sink := &capSink{}
	m := terminals.NewManager(terminals.NewBackend(), sink)
	defer m.Shutdown()
	info, err := m.Create(terminals.CreateOptions{Shell: "powershell"})
	if err != nil {
		t.Fatal(err)
	}
	m.Write(info.ID, []byte("Start-Sleep 30\r"))
	time.Sleep(1500 * time.Millisecond)
	m.Write(info.ID, []byte{0x03})
	time.Sleep(500 * time.Millisecond)
	m.Write(info.ID, []byte("echo AFTER-INTERRUPT\r"))
	defer func() {
		if t.Failed() {
			t.Logf("%q", sink.text())
		}
	}()
	waitFor(t, func() bool { return strings.Count(sink.text(), "AFTER-INTERRUPT") >= 2 }, "prompt responsive after Ctrl-C")
}

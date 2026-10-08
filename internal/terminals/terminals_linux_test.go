//go:build linux

package terminals

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func awaitLinux(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}

func terminalText(m *Manager, id string) string {
	a, _ := m.Attach(id)
	return string(a.Data)
}

func TestLinuxPTYCommandEnvironmentAndExit(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "working directory")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := NewBackend().Start(StartSpec{
		Command: `test -t 0 && test -t 1 && printf '%s\n' "$MESSAGE" "$PWD" && stty size; exit 7`,
		Cwd:     cwd, Env: []string{"PATH=/usr/bin:/bin", "MESSAGE=héllo 'quoted' 世界"}, Cols: 97, Rows: 31,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	result := make(chan string, 1)
	go func() {
		var out strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			out.Write(buf[:n])
			if err != nil {
				result <- out.String()
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := p.Wait(ctx)
	if err != nil || code != 7 {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	select {
	case out := <-result:
		for _, want := range []string{"héllo 'quoted' 世界", cwd, "31 97"} {
			if !strings.Contains(out, want) {
				t.Fatalf("output %q missing %q", out, want)
			}
		}
	case <-ctx.Done():
		t.Fatal("PTY read did not finish after child exit")
	}
	if again, err := p.Wait(ctx); again != 7 || err != nil {
		t.Fatalf("repeat Wait = %d, %v", again, err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestLinuxInteractiveResizeCtrlCAndGUIAttach(t *testing.T) {
	m := NewManager(NewBackend(), nil)
	defer m.Shutdown()
	info, err := m.Create(CreateOptions{Shell: "sh", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	send := func(text string) {
		t.Helper()
		if err := m.Write(info.ID, []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	send("stty -echo; printf 'READY\\n'\n")
	awaitLinux(t, "shell ready", func() bool { return strings.Contains(terminalText(m, info.ID), "READY\r\n") })
	if err := m.Resize(info.ID, 101, 37); err != nil {
		t.Fatal(err)
	}
	send("stty size\n")
	awaitLinux(t, "resize visible inside PTY", func() bool { return strings.Contains(terminalText(m, info.ID), "37 101") })
	send("sleep 30\n")
	time.Sleep(150 * time.Millisecond)
	send("\x03")
	send("printf 'AFTER_INTERRUPT\\n'\n")
	awaitLinux(t, "Ctrl-C interrupts foreground job", func() bool { return strings.Contains(terminalText(m, info.ID), "AFTER_INTERRUPT") })
	a, err := m.Attach(info.ID)
	if err != nil || a.Seq == 0 || !strings.Contains(string(a.Data), "37 101") {
		t.Fatalf("GUI attachment = %+v, %v", a, err)
	}
	send("exit 9\n")
	awaitLinux(t, "exit status", func() bool {
		a, _ := m.Attach(info.ID)
		return a.Info.State == Exited && a.Info.ExitCode != nil && *a.Info.ExitCode == 9
	})
}

func TestLinuxWaitCancellationAndClose(t *testing.T) {
	p, err := NewBackend().Start(StartSpec{Command: "exec sleep 30", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Wait: %v", err)
	}
	readDone := make(chan struct{})
	go func() { _, _ = p.Read(make([]byte, 1)); close(readDone) }()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-readDone:
	case <-ctx.Done():
		t.Fatal("Close did not unblock Read")
	}
}

func TestLinuxRejectsInvalidLaunch(t *testing.T) {
	for _, spec := range []StartSpec{
		{Command: "", Cols: 80, Rows: 24},
		{Command: "sh", Cols: 65536, Rows: 24},
		{Command: "sh", Cols: 80, Rows: 24, Cwd: filepath.Join(t.TempDir(), "missing")},
	} {
		if p, err := NewBackend().Start(spec); err == nil {
			_ = p.Close()
			t.Fatalf("accepted invalid spec %+v", spec)
		}
	}
}

func TestLinuxTmuxSurvivesManagerRestart(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("requires tmux 3.2+")
	}
	// Never connect to a user's/default tmux server, including in cleanup.
	b := linuxBackend{tmuxSocket: "orole-test-" + uuid.NewString()}
	t.Cleanup(func() { _, _ = b.tmux("kill-server") })
	m := NewManager(b, nil)
	defer m.Shutdown()
	info, err := m.Create(CreateOptions{Shell: "sh", Persistence: "tmux", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if !validPersistentID(info.PersistentID) {
		t.Fatalf("missing durable identity: %+v", info)
	}
	if err := m.Write(info.ID, []byte("stty -echo; OROLE_PROOF=preserved; printf 'BEFORE_RESTART\\n'\n")); err != nil {
		t.Fatal(err)
	}
	awaitLinux(t, "tmux shell initialized", func() bool { return strings.Contains(terminalText(m, info.ID), "BEFORE_RESTART\r\n") })
	m.Shutdown()
	second := NewManager(b, nil)
	defer second.Shutdown()
	sessions, err := second.PersistentSessions()
	if err != nil || len(sessions) != 1 || sessions[0].ID != info.PersistentID {
		t.Fatalf("enumerate after shutdown: %+v, %v", sessions, err)
	}
	restored, err := second.Restore(info.PersistentID, 95, 29)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Write(restored.ID, []byte("printf 'AFTER:%s\\n' \"$OROLE_PROOF\"; stty size\n")); err != nil {
		t.Fatal(err)
	}
	awaitLinux(t, "same live shell after daemon restart", func() bool {
		out := terminalText(second, restored.ID)
		return strings.Contains(out, "AFTER:preserved") && strings.Contains(out, "29 95")
	})
	again, err := second.Restore(info.PersistentID, 80, 24)
	if err != nil || again.ID != restored.ID {
		t.Fatalf("duplicate Restore: %+v, %v", again, err)
	}
	if err := second.Close(restored.ID); err != nil {
		t.Fatal(err)
	}
	sessions, err = second.PersistentSessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("explicit Close must kill tmux session: %+v, %v", sessions, err)
	}
	if _, err := second.Restore(info.PersistentID, 80, 24); err == nil {
		t.Fatal("restored a killed session")
	}
}

func TestLinuxTmuxHomeIsolationAndFailures(t *testing.T) {
	t.Setenv("OROLE_HOME", t.TempDir())
	b := linuxBackend{}
	first, err := b.tmuxArgs("list-sessions")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OROLE_HOME", t.TempDir())
	second, err := b.tmuxArgs("list-sessions")
	if err != nil || first[1] == second[1] {
		t.Fatalf("homes share tmux server: %v / %v, %v", first, second, err)
	}
	if _, err := b.RestorePersistent("unrelated-user-session", 80, 24); err == nil {
		t.Fatal("allowed attaching unrelated tmux session")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := b.PersistentSessions(); err == nil {
		t.Fatal("missing tmux silently treated as no sessions")
	}
	if _, err := b.StartPersistent(StartSpec{Command: "sh", Cols: 80, Rows: 24}); err == nil {
		t.Fatal("missing tmux silently fell back to a native terminal")
	}
}

func TestLinuxTmuxDaemonCrash(t *testing.T) {
	// A helper process owns the manager so SIGKILL genuinely closes the daemon's
	// PTY descriptors without running Shutdown or any deferred cleanup.
	if socket := os.Getenv("OROLE_TMUX_CRASH_HELPER"); socket != "" {
		m := NewManager(linuxBackend{tmuxSocket: socket}, nil)
		info, err := m.Create(CreateOptions{Shell: "sh", Persistence: "tmux"})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Write(info.ID, []byte("stty -echo; OROLE_CRASH_PROOF=alive; printf 'CRASH_READY\\n'\n")); err != nil {
			t.Fatal(err)
		}
		awaitLinux(t, "crash helper shell", func() bool {
			return strings.Contains(terminalText(m, info.ID), "CRASH_READY\r\n")
		})
		if err := os.WriteFile(os.Getenv("OROLE_TMUX_CRASH_MARKER"), []byte(info.PersistentID), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("requires tmux 3.2+")
	}
	b := linuxBackend{tmuxSocket: "orole-crash-test-" + uuid.NewString()}
	t.Cleanup(func() { _, _ = b.tmux("kill-server") })
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "persistent-id")
	child := exec.Command(exe, "-test.run=^TestLinuxTmuxDaemonCrash$", "-test.timeout=30s")
	child.Env = append(os.Environ(), "OROLE_TMUX_CRASH_HELPER="+b.tmuxSocket, "OROLE_TMUX_CRASH_MARKER="+marker)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	var id string
	awaitLinux(t, "daemon subprocess starts persistent terminal", func() bool {
		data, err := os.ReadFile(marker)
		id = string(data)
		return err == nil && validPersistentID(id)
	})
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	m := NewManager(b, nil)
	defer m.Shutdown()
	info, err := m.Restore(id, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Write(info.ID, []byte("printf 'SURVIVED:%s\\n' \"$OROLE_CRASH_PROOF\"\n")); err != nil {
		t.Fatal(err)
	}
	awaitLinux(t, "same shell survives daemon SIGKILL", func() bool {
		return strings.Contains(terminalText(m, info.ID), "SURVIVED:alive")
	})
	if err := m.Close(info.ID); err != nil {
		t.Fatal(err)
	}
}

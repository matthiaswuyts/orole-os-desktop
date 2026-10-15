//go:build windows

package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/wire"
)

const e2eRepo = "rayorole/orole-os-desktop"

// TestRealGuestImageThroughTheManager boots the released NixOS guest image (DefaultPins) on this PC through the
// real Runtime install (hash checks, unzip), Manager and host backend (QEMU + WHPX). It needs WHP on, about
// 5 GB of free RAM and the gh CLI signed in (draft releases download with repository access). Assets are kept
// in OROLE_SANDBOX_E2E_ASSETS (default %TEMP%\orole-realboot\<tag>\<name>) and downloaded only when missing
// or not matching the pins. Run by hand:
//
//	$env:OROLE_SANDBOX_E2E = "1"
//	go test -run TestRealGuestImageThroughTheManager -v -timeout 30m ./internal/sandbox
//
// Lines starting with "E2E RESULT" are the measurements docs/architecture/bot-computer.md records.
func TestRealGuestImageThroughTheManager(t *testing.T) {
	if os.Getenv("OROLE_SANDBOX_E2E") == "" {
		t.Skip("set OROLE_SANDBOX_E2E=1 to boot the released guest image (downloads ~1.6 GB once)")
	}
	pins := DefaultPins
	if !pins.Complete() {
		t.Fatal("this build has no DefaultPins")
	}
	assets := os.Getenv("OROLE_SANDBOX_E2E_ASSETS")
	if assets == "" {
		assets = filepath.Join(os.TempDir(), "orole-realboot")
	}
	e2eFetchAssets(t, assets, pins.QEMUTag, []Asset{pins.QEMU})
	e2eFetchAssets(t, assets, pins.ImageTag, pins.Image)

	// The release server is a local stand-in for github.com serving the verified files at the same paths.
	srv := httptest.NewServer(http.FileServer(http.Dir(assets)))
	defer srv.Close()
	root := shortRoot(t)
	paths := Paths{Root: filepath.Join(root, "sandbox")}
	rt := NewRuntime(RuntimeConfig{Paths: paths, Pins: pins, BaseURL: srv.URL})
	start := time.Now()
	if err := rt.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Logf("E2E RESULT install (local copy, verify, unzip) = %s", time.Since(start).Round(time.Millisecond))

	backend := NewHostBackend(rt, paths)
	if av := backend.Availability(context.Background()); !av.Ready {
		t.Skipf("this PC cannot run the computer: %+v", av)
	}
	t.Logf("E2E RESULT this PC's keyboard layout: %s (the guest gets be below)", HostKeymap())
	// The guest gets a Belgian keyboard whatever this PC uses, so the key check below is the same everywhere.
	backend.(*hostBackend).keymap = func() string { return "be" }
	// Bot names: the computer's account, home and host name follow them; e2e-bot is renamed between boots.
	var namesMu sync.Mutex
	names := map[string]string{"e2e-bot": "Élodie Test", "e2e-other": "Atlas 2"}
	botName := func(id string) string { namesMu.Lock(); defer namesMu.Unlock(); return names[id] }
	m := NewManager(Config{Paths: paths, Image: rt.Image, BootTimeout: 3 * time.Minute, IdleTimeout: time.Hour, BotName: botName},
		backend, NewStore(testDB(t)))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = m.Shutdown(ctx)
	}()
	ctx := context.Background()
	var tokens []string

	ensure := func(botID, label string) *Guest {
		t.Helper()
		start := time.Now()
		g, err := m.Ensure(ctx, botID)
		if err != nil {
			log, _ := os.ReadFile(paths.Log(root, botID))
			t.Fatalf("%s: %v\n--- console (tail) ---\n%s", label, err, logTail(log, 6000))
		}
		tokens = append(tokens, g.token)
		t.Logf("E2E RESULT %s: boot to guest ready = %s", label, time.Since(start).Round(time.Millisecond))
		return g
	}
	run := func(g *Guest, cmd string) wire.ExecResult {
		t.Helper()
		res, err := g.Exec(ctx, wire.ExecRequest{Command: cmd, Timeout: 60})
		if err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
		return res
	}
	waitFor := func(g *Guest, what, cmd, want string, limit time.Duration) string {
		t.Helper()
		start := time.Now()
		var out string
		for {
			res := run(g, cmd)
			out = res.Stdout + res.Stderr
			if strings.Contains(res.Stdout, want) {
				t.Logf("E2E RESULT %s up %s after guest ready", what, time.Since(start).Round(time.Millisecond))
				return res.Stdout
			}
			if time.Since(start) > limit {
				t.Fatalf("%s not up after %s: %q", what, limit, out)
			}
			time.Sleep(time.Second)
		}
	}

	g := ensure("e2e-bot", "first boot (fresh overlay)")
	t.Logf("E2E RESULT QEMU process after first boot: %s", qemuMemory(t, m, "e2e-bot"))

	e2eIdentity(t, g, run, "elodie-test")
	if out := strings.TrimSpace(run(g, `python3 -c "print(1+1)"`).Stdout); out != "2" {
		t.Fatalf("python3: %q", out)
	}
	// The bot user cannot connect to Chromium's DevTools port or wayvnc (guest nftables), so both are checked by
	// their listening sockets, and the connections are checked to fail.
	waitFor(g, "Chromium CDP (127.0.0.1:9222)", "ss -ltn", "127.0.0.1:9222", 90*time.Second)
	ss := waitFor(g, "wayvnc (127.0.0.1:5900)", "ss -ltn", "127.0.0.1:5900", 60*time.Second)
	for _, line := range strings.Split(ss, "\n") {
		for _, port := range []string{":5900", ":9222"} {
			if strings.Contains(line, port+" ") && !strings.Contains(line, "127.0.0.1"+port) {
				t.Fatalf("a %s listener is reachable beyond guest loopback: %q", port, line)
			}
		}
	}
	t.Logf("E2E RESULT chromium: %s", strings.TrimSpace(run(g, "chromium --version").Stdout))
	e2eBotCannotReachCDPOrVNC(t, g, run)
	fonts := run(g, "fc-list | wc -l; fc-list : family | sort -u | head -5")
	if n, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(fonts.Stdout, "\n", 2)[0])); n == 0 {
		t.Fatalf("no fonts: %q %q", fonts.Stdout, fonts.Stderr)
	}
	t.Logf("E2E RESULT fonts: %s", strings.Join(strings.Fields(strings.ReplaceAll(fonts.Stdout, "\n", " | ")), " "))
	// Only root (orole-guest) reads the token and opens the channel; bot cannot.
	const tokenFile = "/sys/firmware/qemu_fw_cfg/by_name/opt/orole/token/raw"
	if out := strings.TrimSpace(run(g, "stat -c '%a %U' "+tokenFile+"; stat -L -c '%a %U' /dev/virtio-ports/"+guestPortName).Stdout); out != "400 root\n600 root" {
		t.Fatalf("token file and channel modes = %q, want 400 root and 600 root", out)
	}
	if res := run(g, "cat "+tokenFile); res.ExitCode == 0 || res.Stdout != "" {
		t.Fatalf("bot read the token file: %+v", res)
	}
	units := run(g, "systemctl is-active cage-tty1 wayvnc orole-guest | tr '\\n' ' '; systemd-analyze 2>&1 | head -1; df -h / | tail -1")
	t.Logf("E2E RESULT units, boot time, disk: %s", strings.Join(strings.Fields(units.Stdout), " "))
	if strings.Contains(units.Stdout, "inactive") || strings.Contains(units.Stdout, "failed") {
		t.Fatalf("a guest service is down: %q", units.Stdout)
	}
	tools := run(g, "python3 --version; node --version; uv --version; git --version; bash -c 'echo bash ok'")
	if tools.ExitCode != 0 {
		t.Fatalf("toolchain: %+v", tools)
	}
	t.Logf("E2E RESULT tools as bot: %s", strings.Join(strings.Fields(strings.ReplaceAll(tools.Stdout, "\n", " | ")), " "))
	t.Logf("E2E RESULT guest memory (free -m, used/total): %s",
		strings.TrimSpace(run(g, "free -m | awk '/Mem:/{print $3\" MB used of \"$2\" MB\"}'").Stdout))

	// The host's loopback (10.0.2.2 in QEMU user networking) is out of reach: nothing connects to a listener there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
			accepted <- struct{}{}
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	hostRes := run(g, fmt.Sprintf("curl -sS -m 5 -o /dev/null http://10.0.2.2:%d/; echo exit=$?", port))
	select {
	case <-accepted:
		t.Fatalf("the guest reached the host's loopback: %+v", hostRes)
	case <-time.After(500 * time.Millisecond):
	}
	if strings.Contains(hostRes.Stdout, "exit=0") {
		t.Fatalf("curl to 10.0.2.2 succeeded: %+v", hostRes)
	}
	t.Logf("E2E RESULT host loopback 10.0.2.2:%d blocked: %s %s", port, strings.TrimSpace(hostRes.Stdout), strings.TrimSpace(hostRes.Stderr))
	inet := run(g, "curl -sS -I -m 20 -o /dev/null -w 'http=%{http_code}' https://example.com 2>&1; echo \" exit=$?\"")
	t.Logf("E2E RESULT internet (curl -sI https://example.com): %s", strings.Join(strings.Fields(inet.Stdout+" "+inet.Stderr), " "))

	// The bot's browser (chromedp on the visible Chromium) and the VNC screen relay, through the real Guest.
	e2eBrowser(t, ctx, g, run)
	e2eLongStreams(t, ctx, g)

	e2eKeyboardAndPointer(t, ctx, g, run)

	// Files are written as the bot's account, in its home.
	if _, err := g.WriteFile(ctx, "persist.txt", []byte("kept")); err != nil {
		t.Fatal(err)
	}
	if out := strings.TrimSpace(run(g, "stat -c %U ~/persist.txt; ls /home/elodie-test/persist.txt").Stdout); out != "elodie-test\n/home/elodie-test/persist.txt" {
		t.Fatalf("persist.txt owner and place %q, want elodie-test in /home/elodie-test", out)
	}
	run(g, "sync")

	// Clean stop, then the same overlay boots again with the file.
	start = time.Now()
	e2eStop(t, m, "e2e-bot")
	t.Logf("E2E RESULT clean shutdown = %s", time.Since(start).Round(time.Millisecond))
	// Renamed while off: the next boot uses the new name, with the same files.
	namesMu.Lock()
	names["e2e-bot"] = "Élodie Renamed"
	namesMu.Unlock()
	g = ensure("e2e-bot", "second boot (same overlay, bot renamed)")
	e2eIdentity(t, g, run, "elodie-renamed")
	if f, err := g.ReadFile(ctx, "persist.txt"); err != nil || string(f.Content) != "kept" {
		t.Fatalf("persist.txt after a clean restart: %q %v", f.Content, err)
	}
	if out := strings.TrimSpace(run(g, "LC_ALL=C ls -A /home; stat -c %U ~/persist.txt").Stdout); out != ".orole-user\nelodie-renamed\nelodie-renamed" {
		t.Fatalf("after the rename /home and the owner = %q, want only .orole-user and elodie-renamed", out)
	}
	t.Logf("E2E RESULT rename: elodie-test -> elodie-renamed, persist.txt kept, old home gone")
	waitFor(g, "Chromium CDP after the rename", "ss -ltn", "127.0.0.1:9222", 90*time.Second)
	if res, err := g.Browser(ctx, wire.BrowserRequest{Action: wire.BrowserOpen, URL: "https://example.com"}); err != nil || !strings.Contains(res.Title, "Example") {
		t.Fatalf("browser after the rename: %+v %v", res, err)
	}
	t.Logf("E2E RESULT browser after the rename: open https://example.com works")

	// Hard kill (what a crashed daemon's job object does), then the overlay still boots and kept a synced file.
	if _, err := g.WriteFile(ctx, "marker.txt", []byte("synced before the kill")); err != nil {
		t.Fatal(err)
	}
	run(g, "sync")
	e2eKill(t, m, "e2e-bot")
	g = ensure("e2e-bot", "boot after a hard kill")
	if f, err := g.ReadFile(ctx, "marker.txt"); err != nil || string(f.Content) != "synced before the kill" {
		t.Fatalf("marker.txt after a hard kill: %q %v", f.Content, err)
	}

	// Two bots run at once on the one read-only base image.
	baseBefore, _ := os.Stat(filepath.Join(paths.Image(pins.ImageVersion), imageBase))
	g2 := ensure("e2e-other", "second bot (shared base image)")
	if out := strings.TrimSpace(run(g2, "id -un; test -e ~/persist.txt && echo leaked || echo separate").Stdout); out != "atlas-2\nseparate" {
		t.Fatalf("second bot: %q", out)
	}
	if out := strings.TrimSpace(run(g, "cat ~/marker.txt").Stdout); out != "synced before the kill" {
		t.Fatalf("first bot while the second runs: %q", out)
	}
	t.Logf("E2E RESULT QEMU processes with two bots: e2e-bot %s; e2e-other %s", qemuMemory(t, m, "e2e-bot"), qemuMemory(t, m, "e2e-other"))
	baseAfter, _ := os.Stat(filepath.Join(paths.Image(pins.ImageVersion), imageBase))
	if baseBefore == nil || baseAfter == nil || !baseAfter.ModTime().Equal(baseBefore.ModTime()) || baseAfter.Size() != baseBefore.Size() {
		t.Fatal("the shared base image changed")
	}
	for _, id := range []string{"e2e-bot", "e2e-other"} {
		if !pathExists(paths.Overlay(id)) {
			t.Fatalf("no overlay for %s", id)
		}
	}

	sctx, cancel := context.WithTimeout(ctx, time.Minute)
	if err := m.Shutdown(sctx); err != nil {
		t.Fatal(err)
	}
	cancel()

	// The per-boot tokens never reach a console log.
	for _, id := range []string{"e2e-bot", "e2e-other"} {
		log, err := os.ReadFile(paths.Log(root, id))
		if err != nil {
			t.Fatal(err)
		}
		for _, tok := range tokens {
			if strings.Contains(string(log), tok) {
				t.Fatalf("a guest token is in %s", paths.Log(root, id))
			}
		}
		t.Logf("E2E RESULT %s console log: %d bytes, no token", id, len(log))
	}
}

// e2eFetchAssets makes dir\<tag>\<name> match the pins, downloading with gh what is missing or different.
func e2eFetchAssets(t *testing.T, dir, tag string, want []Asset) {
	t.Helper()
	tagDir := filepath.Join(dir, tag)
	for _, a := range want {
		p := filepath.Join(tagDir, a.Name)
		if e2eMatches(p, a) == nil {
			continue
		}
		t.Logf("downloading %s/%s (%d MB)", tag, a.Name, a.Size>>20)
		cmd := exec.Command("gh", "release", "download", tag, "--repo", e2eRepo, "--pattern", a.Name, "--dir", tagDir, "--clobber")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("gh release download %s %s: %v\n%s", tag, a.Name, err, out)
		}
		if err := e2eMatches(p, a); err != nil {
			t.Fatalf("%s/%s does not match DefaultPins: %v", tag, a.Name, err)
		}
	}
}

func e2eMatches(p string, a Asset) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != a.Size {
		return fmt.Errorf("%d bytes, pinned %d", n, a.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("sha256 %s, pinned %s", got, a.SHA256)
	}
	return nil
}

// e2eStop stops one bot the way the idle reaper does (power off through the agent, then QMP, then kill).
func e2eStop(t *testing.T, m *Manager, botID string) {
	t.Helper()
	m.mu.Lock()
	inst := m.vms[botID]
	if inst == nil || inst.state != StateRunning {
		m.mu.Unlock()
		t.Fatalf("%s is not running", botID)
	}
	m.beginStopLocked(inst)
	m.mu.Unlock()
	m.finishStop(inst, errStopped)
	if st := m.Status(botID); st.State != StateOff {
		t.Fatalf("after stop: %+v", st)
	}
}

// e2eKill kills one bot's QEMU and waits until the manager has noticed.
func e2eKill(t *testing.T, m *Manager, botID string) {
	t.Helper()
	m.mu.Lock()
	inst := m.vms[botID]
	m.mu.Unlock()
	if inst == nil {
		t.Fatalf("%s is not running", botID)
	}
	if err := inst.vm.Kill(); err != nil {
		t.Fatal(err)
	}
	<-inst.vm.Done()
	deadline := time.Now().Add(10 * time.Second)
	for m.Status(botID).State != StateOff {
		if time.Now().After(deadline) {
			t.Fatalf("manager did not notice the kill: %+v", m.Status(botID))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st := m.Status(botID); !strings.Contains(st.Error, ErrCrashed.Error()) {
		t.Fatalf("a killed VM should read as crashed: %+v", st)
	}
}

// qemuMemory reports the working set and private bytes of a running bot's QEMU process.
func qemuMemory(t *testing.T, m *Manager, botID string) string {
	t.Helper()
	m.mu.Lock()
	inst := m.vms[botID]
	m.mu.Unlock()
	if inst == nil {
		return "not running"
	}
	qv, ok := inst.vm.(*qemuVM)
	if !ok {
		return "unknown"
	}
	ep, ok := qv.proc.(*execProcess)
	if !ok {
		return "unknown"
	}
	pid := ep.cmd.Process.Pid
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("$p = Get-Process -Id %d; '{0} {1}' -f $p.WorkingSet64, $p.PrivateMemorySize64", pid)).Output()
	if err != nil {
		return fmt.Sprintf("pid %d: %v", pid, err)
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		return strings.TrimSpace(string(out))
	}
	ws, _ := strconv.ParseInt(f[0], 10, 64)
	priv, _ := strconv.ParseInt(f[1], 10, 64)
	return fmt.Sprintf("pid %d working set %d MB, private %d MB", pid, ws>>20, priv>>20)
}

func logTail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

// e2eBotCannotReachCDPOrVNC checks that commands, which run as bot, cannot connect to Chromium's DevTools port or
// wayvnc on guest loopback (IPv4 and IPv6): only orole-guest (root) can. The image has no nc; bash's /dev/tcp is the
// same TCP connect.
func e2eBotCannotReachCDPOrVNC(t *testing.T, g *Guest, run func(*Guest, string) wire.ExecResult) {
	t.Helper()
	for _, c := range []struct{ label, cmd string }{
		{"curl -s 127.0.0.1:9222/json/version", "curl -s -m 5 http://127.0.0.1:9222/json/version; echo \" exit=$?\""},
		{"curl -s [::1]:9222/json/version", "curl -s -m 5 'http://[::1]:9222/json/version'; echo \" exit=$?\""},
		{"connect 127.0.0.1:5900 (nc -z)", "timeout 5 bash -c 'exec 3<>/dev/tcp/127.0.0.1/5900' 2>&1; echo \" exit=$?\""},
		{"connect [::1]:5900", "timeout 5 bash -c 'exec 3<>/dev/tcp/::1/5900' 2>&1; echo \" exit=$?\""},
		{"connect 127.0.0.2:9222", "timeout 5 bash -c 'exec 3<>/dev/tcp/127.0.0.2/9222' 2>&1; echo \" exit=$?\""},
	} {
		res := run(g, c.cmd)
		out := strings.Join(strings.Fields(res.Stdout+" "+res.Stderr), " ")
		if strings.Contains(out, "exit=0") || strings.Contains(out, `"Browser"`) {
			t.Fatalf("bot reached it: %s: %s", c.label, out)
		}
		t.Logf("E2E RESULT bot %s refused: %s", c.label, out)
	}
}

// e2eBrowser drives the guest's visible Chromium through Guest.Browser: open, read, screenshot, click (by text, and a
// clear error for a missing target), a target=_blank link whose new tab the next action must follow, and typing,
// which a password field refuses.
func e2eBrowser(t *testing.T, ctx context.Context, g *Guest, run func(*Guest, string) wire.ExecResult) {
	t.Helper()
	browse := func(label string, req wire.BrowserRequest) (wire.BrowserResult, error) {
		t.Helper()
		start := time.Now()
		res, err := g.Browser(ctx, req)
		t.Logf("E2E RESULT browser %s = %s (title %q, url %q, err %v)", label, time.Since(start).Round(time.Millisecond), res.Title, res.URL, err)
		return res, err
	}
	must := func(label string, req wire.BrowserRequest) wire.BrowserResult {
		t.Helper()
		res, err := browse(label, req)
		if err != nil {
			t.Fatalf("browser %s: %v", label, err)
		}
		return res
	}

	res := must("open https://example.com", wire.BrowserRequest{Action: wire.BrowserOpen, URL: "https://example.com"})
	if !strings.Contains(res.Title, "Example") {
		t.Fatalf("title after open = %q, want it to contain Example", res.Title)
	}
	// example.com's body changed in 2026: only a paragraph (several languages), no "Example Domain" heading and no link.
	res = must("read", wire.BrowserRequest{Action: wire.BrowserRead})
	if !strings.Contains(res.Text, "documentation examples") {
		t.Fatalf("page text = %q, want example.com's paragraph", res.Text)
	}
	res = must("screenshot", wire.BrowserRequest{Action: wire.BrowserScreenshot})
	img, err := png.Decode(bytes.NewReader(res.PNG))
	if err != nil {
		t.Fatalf("screenshot is not a PNG (%d bytes): %v", len(res.PNG), err)
	}
	if b := img.Bounds(); b.Dx() < 200 || b.Dy() < 200 || len(res.PNG) < 2000 {
		t.Fatalf("screenshot too small: %dx%d, %d bytes", b.Dx(), b.Dy(), len(res.PNG))
	}
	t.Logf("E2E RESULT screenshot: %dx%d PNG, %d bytes", img.Bounds().Dx(), img.Bounds().Dy(), len(res.PNG))

	// A missing target is a clear error, not a hang.
	if _, err := browse("click #orole-missing", wire.BrowserRequest{Action: wire.BrowserClick, Target: "#orole-missing"}); err == nil || !strings.Contains(err.Error(), "nothing visible") {
		t.Fatalf("click on a missing element = %v, want a 'nothing visible' error", err)
	}

	// Links: pages from a server on the guest's own loopback. A same-tab link clicked by its text navigates; a
	// target=_blank link opens a new front tab, which the next action must drive.
	pages := map[string]string{
		"www/index.html":  `<!doctype html><title>First tab</title><p><a href="/info.html">More information...</a></p><p><a href="/second.html" target="_blank">Open second</a></p>`,
		"www/info.html":   `<!doctype html><title>Info page</title><p>orole info page</p>`,
		"www/second.html": `<!doctype html><title>Second tab</title><p>orole second tab</p>`,
		"www/login.html":  `<!doctype html><title>Login</title><input id="user" aria-label="User"><input id="pw" type="password" aria-label="Password">`,
	}
	for name, body := range pages {
		if _, err := g.WriteFile(ctx, name, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	run(g, "setsid -f python3 -m http.server 8765 --bind 127.0.0.1 --directory ~/www >/dev/null 2>&1; for i in $(seq 50); do curl -s -o /dev/null http://127.0.0.1:8765/ && break; sleep 0.1; done")
	defer run(g, "pkill -f 'http.server 8765' || true")
	const local = "http://127.0.0.1:8765/index.html"
	if res = must("open local page", wire.BrowserRequest{Action: wire.BrowserOpen, URL: local}); res.Title != "First tab" {
		t.Fatalf("title = %q, want First tab", res.Title)
	}
	must("click text=More information", wire.BrowserRequest{Action: wire.BrowserClick, Target: "text=More information"})
	waitTitle := func(label, title, text string) {
		t.Helper()
		start := time.Now()
		for {
			res = must(label, wire.BrowserRequest{Action: wire.BrowserRead})
			if res.Title == title && strings.Contains(res.Text, text) {
				t.Logf("E2E RESULT %s: on %q %s after the click", label, title, time.Since(start).Round(time.Millisecond))
				return
			}
			if time.Since(start) > 15*time.Second {
				t.Fatalf("%s: title %q url %q, want %q", label, res.Title, res.URL, title)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	waitTitle("read after the link click", "Info page", "orole info page")
	if res = must("open local page again", wire.BrowserRequest{Action: wire.BrowserOpen, URL: local}); res.Title != "First tab" {
		t.Fatalf("title = %q, want First tab", res.Title)
	}
	must("click target=_blank link", wire.BrowserRequest{Action: wire.BrowserClick, Target: "text=Open second"})
	waitTitle("read after target=_blank", "Second tab", "orole second tab")

	// Typing: an ordinary field takes the text; a password field is refused (the bot asks the user to take over).
	if res = must("open login page", wire.BrowserRequest{Action: wire.BrowserOpen, URL: "http://127.0.0.1:8765/login.html"}); res.Title != "Login" {
		t.Fatalf("title = %q, want Login", res.Title)
	}
	must("type into #user", wire.BrowserRequest{Action: wire.BrowserType, Target: "#user", Text: "orole"})
	for _, target := range []string{"#pw", "text=Password"} {
		_, err := browse("type into "+target, wire.BrowserRequest{Action: wire.BrowserType, Target: target, Text: "secret"})
		if err == nil || !strings.Contains(err.Error(), "password field") || !strings.Contains(err.Error(), "ask_user_to_take_over") {
			t.Fatalf("type into the password field %s = %v, want the password refusal", target, err)
		}
	}
}

// e2eLongStreams opens the VNC screen relay (the RFB handshake with wayvnc) and a guest terminal, keeps both idle for
// longer than a minute and then uses both again: nothing on the path (guest http2.Server, host transport) may cut a
// long-lived stream.
func e2eLongStreams(t *testing.T, ctx context.Context, g *Guest) {
	t.Helper()
	start := time.Now()
	scr, err := g.OpenScreen(ctx)
	if err != nil {
		t.Fatalf("OpenScreen: %v", err)
	}
	defer scr.Close()
	read := func(n int, what string) []byte {
		t.Helper()
		b := make([]byte, n)
		if _, err := io.ReadFull(scr, b); err != nil {
			t.Fatalf("screen %s: %v", what, err)
		}
		return b
	}
	banner := read(12, "banner")
	if !strings.HasPrefix(string(banner), "RFB 003.0") {
		t.Fatalf("screen banner = %q, want RFB 003.0xx", banner)
	}
	t.Logf("E2E RESULT screen banner %q after %s", strings.TrimSpace(string(banner)), time.Since(start).Round(time.Millisecond))
	// RFB 3.8: version, security types, None, SecurityResult, ClientInit (shared), ServerInit.
	if _, err := scr.Write([]byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	types := read(int(read(1, "security type count")[0]), "security types")
	if !bytes.Contains(types, []byte{1}) {
		t.Fatalf("wayvnc offers security types %v, want None (1) on the guest loopback", types)
	}
	if _, err := scr.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if r := binary.BigEndian.Uint32(read(4, "security result")); r != 0 {
		t.Fatalf("security result %d", r)
	}
	if _, err := scr.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	init := read(24, "server init")
	w, h := binary.BigEndian.Uint16(init[0:2]), binary.BigEndian.Uint16(init[2:4])
	read(int(binary.BigEndian.Uint32(init[20:24])), "desktop name")
	t.Logf("E2E RESULT screen RFB handshake done: framebuffer %dx%d", w, h)

	pty, err := g.OpenPTY(ctx, 80, 24)
	if err != nil {
		t.Fatalf("OpenPTY: %v", err)
	}
	defer pty.Close()

	const idle = 65 * time.Second
	time.Sleep(idle)

	// The screen still answers: a full FramebufferUpdateRequest gets a FramebufferUpdate (message type 0).
	req := []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(req[6:8], w)
	binary.BigEndian.PutUint16(req[8:10], h)
	if _, err := scr.Write(req); err != nil {
		t.Fatalf("screen write after %s idle: %v", idle, err)
	}
	if typ := read(1, "update after idle")[0]; typ != 0 {
		t.Fatalf("screen message after idle = type %d, want FramebufferUpdate (0)", typ)
	}
	// The terminal still runs commands.
	if _, err := pty.Write([]byte("echo alive-$((40+2))\n")); err != nil {
		t.Fatalf("pty write after %s idle: %v", idle, err)
	}
	var out []byte
	buf := make([]byte, 4096)
	deadline := time.Now().Add(15 * time.Second)
	for !bytes.Contains(out, []byte("alive-42")) {
		if time.Now().After(deadline) {
			t.Fatalf("pty after %s idle: %q", idle, out)
		}
		n, err := pty.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			t.Fatalf("pty read after %s idle: %v (%q)", idle, err, out)
		}
	}
	t.Logf("E2E RESULT screen and terminal streams still work after %s idle", idle)
}

// e2eIdentity checks the account, home, host name and keyboard layout the guest took from fw_cfg (bot names, the
// "be" keymap), and that cage and wayvnc run as that account with the layout and the drawn pointer.
func e2eIdentity(t *testing.T, g *Guest, run func(*Guest, string) wire.ExecResult, user string) {
	t.Helper()
	got := strings.TrimSpace(run(g, `id -un; id -u; echo "$HOME"; pwd; hostname; cat /proc/sys/kernel/hostname`).Stdout)
	want := strings.Join([]string{user, "1000", "/home/" + user, "/home/" + user, user + "-computer", user + "-computer"}, "\n")
	if got != want {
		diag := run(g, `ls -l /etc/*.orole; grep -E " /etc/| /home/" /proc/self/mountinfo; grep 1000 /etc/passwd; `+
			`systemctl show -p Id,ActiveState,Result,ExecMainStatus orole-identity-reset userborn orole-identity orole-guest cage-tty1`)
		t.Fatalf("identity = %q, want %q\n--- diagnostics ---\n%s%s", got, want, diag.Stdout, diag.Stderr)
	}
	// userborn's read-only binds of the account files are back after the rename, and no edit was left behind.
	if out := strings.TrimSpace(run(g, `grep -cE " /etc/(passwd|group|shadow) ro," /proc/self/mountinfo; ls /etc/*.orole 2>/dev/null | wc -l`).Stdout); out != "3\n0" {
		t.Fatalf("read-only binds of passwd, group, shadow and leftovers = %q, want 3 and 0", out)
	}
	env := strings.TrimSpace(run(g, "cat /run/orole/session.env").Stdout)
	if env != "XKB_DEFAULT_LAYOUT=be\nXKB_DEFAULT_VARIANT=\nWAYVNC_KEYBOARD=be" {
		t.Fatalf("session.env = %q", env)
	}
	var procs string
	for start := time.Now(); ; time.Sleep(time.Second) {
		procs = run(g, `for p in /proc/[0-9]*; do c=$(tr '\0' ' ' < $p/cmdline 2>/dev/null); case "$c" in *bin/wayv[n]c*|*bin/cag[e]*) echo "$(stat -c %U $p) $c";; esac; done`).Stdout
		if strings.Contains(procs, "bin/wayvnc") && strings.Contains(procs, "bin/cage") || time.Since(start) > time.Minute {
			break
		}
	}
	for _, want := range []string{user + " ", "--render-cursor", "--keyboard=be ", "bin/cage"} {
		if !strings.Contains(procs, want) {
			diag := run(g, `systemctl show -p Id,ActiveState,SubState,NRestarts,Result,ExecMainStatus cage-tty1 wayvnc orole-identity systemd-logind; ls -la /run/user /home /home/.orole-user; ls -la /run/orole; cat /proc/sys/kernel/hostname; uptime`)
			t.Fatalf("cage/wayvnc processes lack %q:\n%s\n--- diagnostics ---\n%s%s", want, procs, diag.Stdout, diag.Stderr)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(procs), "\n") {
		if !strings.HasPrefix(line, user+" ") {
			t.Fatalf("a cage/wayvnc process runs as another account: %q", line)
		}
	}
	t.Logf("E2E RESULT identity %s: %s | %s | %s", user, strings.ReplaceAll(got, "\n", " "), strings.ReplaceAll(env, "\n", " "),
		strings.Join(strings.Fields(procs), " "))
}

// e2eKeyboardAndPointer types through the screen relay the way the Computer tab does: client bytes pass the daemon's
// RFB filter (with the user in control), which must keep the cursor pseudo-encodings, and reach wayvnc, which maps
// keysyms with the guest's "be" layout. eacute (0xe9) is a plain key on a Belgian keyboard and absent from a US one,
// so the field showing "é" proves the layout reached wayvnc.
func e2eKeyboardAndPointer(t *testing.T, ctx context.Context, g *Guest, run func(*Guest, string) wire.ExecResult) {
	t.Helper()
	page := `<!doctype html><title>Keys</title><input id="k" autofocus oninput="document.getElementById('o').textContent='typed:'+this.value"><p id="o">typed:</p>`
	if _, err := g.WriteFile(ctx, "keys/index.html", []byte(page)); err != nil {
		t.Fatal(err)
	}
	run(g, "setsid -f python3 -m http.server 8766 --bind 127.0.0.1 --directory ~/keys >/dev/null 2>&1; for i in $(seq 50); do curl -s -o /dev/null http://127.0.0.1:8766/ && break; sleep 0.1; done")
	defer run(g, "pkill -f 'http.server 8766' || true")
	if res, err := g.Browser(ctx, wire.BrowserRequest{Action: wire.BrowserOpen, URL: "http://127.0.0.1:8766/index.html"}); err != nil || res.Title != "Keys" {
		t.Fatalf("open key page: %+v %v", res, err)
	}
	if _, err := g.Browser(ctx, wire.BrowserRequest{Action: wire.BrowserClick, Target: "#k"}); err != nil {
		t.Fatalf("focus the field: %v", err)
	}

	scr, err := g.OpenScreen(ctx)
	if err != nil {
		t.Fatalf("OpenScreen: %v", err)
	}
	defer scr.Close()
	f := newRFBFilter(func() bool { return true })
	send := func(what string, b []byte) {
		t.Helper()
		out, err := f.Feed(b)
		if err != nil {
			t.Fatalf("filter %s: %v", what, err)
		}
		if _, err := scr.Write(out); err != nil {
			t.Fatalf("write %s: %v", what, err)
		}
	}
	read := func(n int, what string) []byte {
		t.Helper()
		b := make([]byte, n)
		if _, err := io.ReadFull(scr, b); err != nil {
			t.Fatalf("screen %s: %v", what, err)
		}
		return b
	}
	read(12, "banner")
	send("version", []byte("RFB 003.008\n"))
	read(int(read(1, "security type count")[0]), "security types")
	send("security None", []byte{1})
	if r := binary.BigEndian.Uint32(read(4, "security result")); r != 0 {
		t.Fatalf("security result %d", r)
	}
	send("client init", []byte{1})
	init := read(24, "server init")
	w, h := binary.BigEndian.Uint16(init[0:2]), binary.BigEndian.Uint16(init[2:4])
	name := read(int(binary.BigEndian.Uint32(init[20:24])), "desktop name")
	t.Logf("E2E RESULT keyboard screen ServerInit: %dx%d, %d bpp, name %q", w, h, init[4], name)

	// noVNC's encodings: raw, copyrect, cursor (-239), xcursor (-240), pointer position (-232), desktop size (-223).
	encodings := []int32{0, 1, -239, -240, -232, -223}
	enc := []byte{2, 0, 0, byte(len(encodings))}
	for _, e := range encodings {
		enc = binary.BigEndian.AppendUint32(enc, uint32(e))
	}
	out, err := f.Feed(enc)
	if err != nil || !bytes.Equal(out, enc) {
		t.Fatalf("the filter changed noVNC's encodings: %x -> %x (%v)", enc, out, err)
	}
	if _, err := scr.Write(out); err != nil {
		t.Fatal(err)
	}
	// Raw 32 bpp keeps the first update readable here.
	send("pixel format", []byte{0, 0, 0, 0, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0})
	req := []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(req[6:8], w)
	binary.BigEndian.PutUint16(req[8:10], h)
	send("update request", req)
	hdr := read(4, "first update")
	if hdr[0] != 0 {
		t.Fatalf("first server message type %d, want FramebufferUpdate", hdr[0])
	}
	rects := int(binary.BigEndian.Uint16(hdr[2:4]))
	var kinds []string
	for i := 0; i < rects; i++ {
		r := read(12, "rectangle")
		rw, rh := int(binary.BigEndian.Uint16(r[4:6])), int(binary.BigEndian.Uint16(r[6:8]))
		e := int32(binary.BigEndian.Uint32(r[8:12]))
		kinds = append(kinds, fmt.Sprintf("%dx%d enc %d", rw, rh, e))
		stop := false
		switch e {
		case 0:
			read(rw*rh*4, "raw pixels")
		case 1:
			read(4, "copyrect")
		case -239: // cursor: pixels, then a bitmask
			read(rw*rh*4+(rw+7)/8*rh, "cursor shape")
		case -232, -223:
		default:
			stop = true
		}
		if stop {
			kinds = append(kinds, "(stopped reading)")
			break
		}
	}
	t.Logf("E2E RESULT first framebuffer update: %d rectangles (%s); cursor encodings passed the filter unchanged", rects, strings.Join(kinds, ", "))

	// A pointer move (drawn into the picture by --render-cursor), then é and a. wayvnc creates its virtual keyboard
	// when the viewer connects; a person types well after that, so the keys wait a moment. A second try re-focuses the
	// field first and is reported.
	send("pointer", []byte{5, 0, 0, 100, 0, 100})
	time.Sleep(time.Second)
	var text string
	tries := 0
	for tries < 2 && !strings.Contains(text, "typed:éa") {
		tries++
		if tries > 1 {
			if _, err := g.Browser(ctx, wire.BrowserRequest{Action: wire.BrowserClick, Target: "#k"}); err != nil {
				t.Fatalf("focus the field again: %v", err)
			}
			run(g, "true") // a guest round trip before typing again
		}
		for _, sym := range []uint32{0xe9, 'a'} {
			k := []byte{4, 1, 0, 0, 0, 0, 0, 0}
			binary.BigEndian.PutUint32(k[4:], sym)
			send("key down", k)
			k[1] = 0
			send("key up", k)
		}
		for start := time.Now(); time.Since(start) < 5*time.Second; time.Sleep(300 * time.Millisecond) {
			res, err := g.Browser(ctx, wire.BrowserRequest{Action: wire.BrowserRead})
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			text = res.Text
			if strings.Contains(text, "éa") {
				break
			}
		}
		t.Logf("E2E RESULT keys try %d: the page says %q", tries, strings.TrimSpace(text))
	}
	if !strings.Contains(text, "typed:éa") {
		t.Fatalf("after typing eacute and a through the screen, the page says %q, want typed:éa", text)
	}
	t.Logf("E2E RESULT keys through the screen relay with the be layout: eacute + a -> %q", strings.TrimSpace(text))
}

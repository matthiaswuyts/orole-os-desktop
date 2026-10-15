package daemon_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/sandbox"
)

func TestSandboxIPCReportsAvailabilityAndStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	defer daemon.UseMemoryLLMCredentials()()
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := control.NewClient(dir, "sandbox-wiring-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var a sandbox.Availability
	if err := c.Call(ctx, "sandbox.availability", struct{}{}, &a); err != nil {
		t.Fatal(err)
	}
	// A fresh data dir never has the runtime installed, so it is never ready, and always says why.
	if a.Ready || a.Reason == "" || a.Detail == "" {
		t.Fatalf("fresh availability %+v", a)
	}
	if runtime.GOOS != "windows" && a.Reason != "unsupported-os" {
		t.Fatalf("non-Windows availability %+v", a)
	}

	var st sandbox.Status
	if err := c.Call(ctx, "sandbox.status", map[string]string{"botId": "no-such-bot"}, &st); err != nil {
		t.Fatal(err)
	}
	if st.State != sandbox.StateOff || st.BotID != "no-such-bot" {
		t.Fatalf("status %+v", st)
	}
	if err := c.Call(ctx, "sandbox.status", map[string]string{}, &st); err == nil {
		t.Fatal("status without botId must be refused")
	}
}

func TestSandboxSetupAndInstallFailCleanlyInTests(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	defer daemon.UseMemoryLLMCredentials()()
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := control.NewClient(dir, "sandbox-setup-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// The test binary has no orole-sandbox-setup.exe next to it (and other platforms have no setup), so
	// this never raises a UAC prompt; it must fail with a readable error.
	var res sandbox.SetupState
	err := c.Call(ctx, "sandbox.setup.enable", struct{}{}, &res)
	want := sandbox.ErrSetupHelperMissing.Error()
	if runtime.GOOS != "windows" {
		want = sandbox.ErrSetupUnsupported.Error()
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("setup.enable err = %v, want %q", err, want)
	}

	if sandbox.DefaultPins.Complete() {
		t.Skip("this build pins a runtime: installing it would download ~1 GB (see TestManualSandboxRuntimeInstall)")
	}
	var st sandbox.RuntimeState
	err = c.Call(ctx, "sandbox.runtime.install", struct{}{}, &st)
	if err == nil || !strings.Contains(err.Error(), sandbox.ErrNoPins.Error()) {
		t.Fatalf("runtime.install without pins: %+v, %v", st, err)
	}
	var a sandbox.Availability
	if err := c.Call(ctx, "sandbox.availability", struct{}{}, &a); err != nil {
		t.Fatal(err)
	}
	if a.Ready {
		t.Fatalf("availability without pins %+v", a)
	}
}

func TestArchivingABotDeletesItsComputer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	defer daemon.UseMemoryLLMCredentials()()
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := control.NewClient(dir, "sandbox-archive-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var bot chat.Bot
	if err := c.Call(ctx, "chat.bots.save", chat.BotInput{Name: "Computer user", Avatar: "C", Color: "blue"}, &bot); err != nil {
		t.Fatal(err)
	}
	paths := sandbox.Paths{Root: filepath.Join(dir, "sandbox")}
	overlay := paths.Overlay(bot.ID)
	if err := os.MkdirAll(filepath.Dir(overlay), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, []byte("QFI"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "orole.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO sandboxes(bot_id,image_version,overlay_path,created_at) VALUES(?,?,?,?)`,
		bot.ID, "0.1.0", overlay, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	if err := c.Call(ctx, "chat.bots.archive", chat.BotRef{ID: bot.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(overlay); !os.IsNotExist(err) {
		t.Fatalf("overlay still there: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sandboxes WHERE bot_id=?`, bot.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows left: %d, %v", n, err)
	}
}

func TestArchivingABotWithoutAComputerWorks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	defer daemon.UseMemoryLLMCredentials()()
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := control.NewClient(dir, "sandbox-archive-none-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var bot chat.Bot
	if err := c.Call(ctx, "chat.bots.save", chat.BotInput{Name: "No computer", Avatar: "N", Color: "blue"}, &bot); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "chat.bots.archive", chat.BotRef{ID: bot.ID}, nil); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "orole.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var outcome string
	err = db.QueryRowContext(ctx, `SELECT outcome FROM audit_events WHERE action='sandbox.delete' AND target=?`, bot.ID).Scan(&outcome)
	if err != sql.ErrNoRows {
		t.Fatalf("a bot without a computer audited a computer delete: %q %v", outcome, err)
	}
}

// TestManualSandboxRuntimeInstall downloads the pinned runtime (~1 GB) through the real daemon. Run by hand:
//
//	$env:OROLE_SANDBOX_MANUAL = "1"; go test -run TestManualSandboxRuntimeInstall -v -timeout 40m ./internal/daemon
func TestManualSandboxRuntimeInstall(t *testing.T) {
	if os.Getenv("OROLE_SANDBOX_MANUAL") != "1" {
		t.Skip("set OROLE_SANDBOX_MANUAL=1 to download the real runtime")
	}
	dir := t.TempDir()
	t.Setenv("OROLE_HOME", dir)
	defer daemon.UseMemoryLLMCredentials()()
	stop := start(t, dir)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()
	c := control.NewClient(dir, "sandbox-install-test")
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var st sandbox.RuntimeState
	if err := c.Call(ctx, "sandbox.runtime.install", struct{}{}, &st); err != nil || !st.Installing {
		t.Fatalf("install: %+v %v", st, err)
	}
	for st.Installing {
		time.Sleep(2 * time.Second)
		if err := c.Call(ctx, "sandbox.runtime.install", struct{}{}, &st); err != nil { // returns progress while running
			t.Fatal(err)
		}
		t.Logf("%s %d/%d", st.Step, st.Done, st.Total)
	}
	if !st.Ready || st.Error != "" {
		t.Fatalf("final %+v", st)
	}
	var a sandbox.Availability
	if err := c.Call(ctx, "sandbox.availability", struct{}{}, &a); err != nil {
		t.Fatal(err)
	}
	t.Logf("availability after install: %+v", a) // ready:true on a PC with WHP enabled
}

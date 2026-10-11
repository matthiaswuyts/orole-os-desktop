package daemon

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestAgentTitleSettingAndAccount(t *testing.T) {
	restore := UseMemoryLLMCredentials()
	defer restore()
	d, err := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	set := func(raw string) {
		t.Helper()
		if _, err := d.DB.Exec(`INSERT INTO settings(key, value, updated_at) VALUES('agents', ?, 1) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, raw); err != nil {
			t.Fatal(err)
		}
	}
	if !d.aiTitlesEnabled() || !d.yoloEnabled() {
		t.Fatal("both switches default on")
	}
	set(`{"aiTitlesDisabled":true}`)
	if d.aiTitlesEnabled() || !d.yoloEnabled() {
		t.Fatal("aiTitlesDisabled must turn off only AI titles")
	}
	set(`{"yoloDisabled":true,"aiTitlesDisabled":false}`)
	if !d.aiTitlesEnabled() || d.yoloEnabled() {
		t.Fatal("yoloDisabled must not turn off AI titles")
	}
	set(`not json`)
	if d.aiTitlesEnabled() || d.yoloEnabled() {
		t.Fatal("an unreadable setting must fail closed")
	}

	for _, kind := range []string{"omp", "custom", "gemini"} {
		if _, err := d.agentTitleAccount(context.Background(), kind); err == nil || !strings.Contains(err.Error(), "no account") {
			t.Fatalf("%s must have no title account: %v", kind, err)
		}
	}
	for _, kind := range []string{"claude", "codex"} {
		if route, err := d.agentTitleAccount(context.Background(), kind); err == nil && (route.Provider == nil || route.Provider.ID() != kind) {
			t.Fatalf("%s task resolved another vendor: %+v", kind, route)
		}
	}
}

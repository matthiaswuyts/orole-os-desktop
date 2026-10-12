package daemon_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

// agents.usage over IPC: the ledger's quarter-hour buckets land in the 24-hour, 7-day and all-time
// windows, reported cost wins, unknown models are named as unpriced, and a bad range is refused.
func TestAgentsUsageWindowsOverIPC(t *testing.T) {
	dir := t.TempDir()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sessions := []agents.Session{
		{ID: "recent", Kind: "claude", Model: "claude-sonnet-4-5", Title: "Recent", Status: agents.StatusDone, ToolCalls: 3, FailedTools: 1, StartedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-time.Minute)},
		{ID: "week", Kind: "codex", Model: "gpt-5", Task: "Week task", Status: agents.StatusDone, StartedAt: now.Add(-3 * 24 * time.Hour), UpdatedAt: now.Add(-3 * 24 * time.Hour)},
		{ID: "old", Kind: "omp", Model: "local/llama", Status: agents.StatusDone, StartedAt: now.Add(-40 * 24 * time.Hour), UpdatedAt: now.Add(-40 * 24 * time.Hour)},
	}
	for _, s := range sessions {
		s.Machine, s.Dir, s.Mode = "local", t.TempDir(), agents.ModeStructured
		if err := d.Agents.Store().SaveSession(s, ""); err != nil {
			t.Fatal(err)
		}
	}
	bucket := func(ago time.Duration) int64 { return now.Add(-ago).Truncate(15 * time.Minute).Unix() }
	for _, r := range []struct {
		session, provider, model string
		start                    int64
		input, output            int64
		cost                     float64
		reported                 bool
	}{
		{"recent", "anthropic", "claude-sonnet-4-5", bucket(time.Hour), 1000, 2000, 0.5, true},
		{"week", "openai", "gpt-5", bucket(3 * 24 * time.Hour), 4000, 0, 0, false},
		{"old", "local", "llama", bucket(40 * 24 * time.Hour), 10, 20, 0, false},
	} {
		if _, err := d.DB.Exec(`INSERT INTO agent_usage(session_id,provider,model,bucket_start,input,output,reported_cost_usd,has_reported_cost) VALUES(?,?,?,?,?,?,?,?)`,
			r.session, r.provider, r.model, r.start, r.input, r.output, r.cost, r.reported); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := control.NewClient(dir, "agents-usage-test")
	defer c.Close()
	for {
		if err = c.Connect(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(20 * time.Millisecond):
		}
	}

	var st agents.UsageStats
	if err := c.Call(ctx, "agents.usage", agents.UsageQuery{ByAgentRange: "7d", Latest: 2}, &st); err != nil {
		t.Fatal(err)
	}
	if st.Day.Sessions != 1 || st.Day.Tokens.Total != 3000 || st.Day.Tokens.CostUSD != 0.5 || len(st.Day.Agents) != 1 || st.Day.Agents[0] != "claude" {
		t.Fatalf("day: %+v", st.Day)
	}
	// gpt-5 is priced from the embedded models.dev snapshot.
	if st.Week.Sessions != 2 || st.Week.Tokens.Total != 7000 || !(st.Week.Tokens.CostUSD > 0.5) || len(st.Week.Tokens.Unpriced) != 0 {
		t.Fatalf("week: %+v", st.Week)
	}
	if st.All.Sessions != 3 || st.All.Tokens.Total != 7030 || len(st.All.Tokens.Unpriced) != 1 || st.All.Tokens.Unpriced[0] != "local/llama" ||
		math.Abs(st.All.Tokens.CostUSD-st.Week.Tokens.CostUSD) > 1e-12 {
		t.Fatalf("all: %+v", st.All)
	}
	if len(st.ByAgent) != 2 || st.ByAgent[0].Kind != "codex" || st.ByAgent[1].Kind != "claude" {
		t.Fatalf("byAgent 7d: %+v", st.ByAgent)
	}
	if len(st.Daily) != 365 || st.Daily[364].Day != time.Now().Format(time.DateOnly) {
		t.Fatalf("daily: %d entries ending %q", len(st.Daily), st.Daily[len(st.Daily)-1].Day)
	}
	if len(st.Latest) != 2 || st.Latest[0].ID != "recent" || st.Latest[0].ToolCalls != 3 || st.Latest[0].FailedTools != 1 ||
		st.Latest[0].Provider != "anthropic" || st.Latest[1].ID != "week" || st.Latest[1].Title != "Week task" {
		t.Fatalf("latest: %+v", st.Latest)
	}
	if st.PricesUpdatedAt.IsZero() {
		t.Fatal("no catalogue time")
	}

	var e *ipc.Error
	if err := c.Call(ctx, "agents.usage", agents.UsageQuery{ByAgentRange: "forever"}, &st); !errors.As(err, &e) || e.Code != ipc.CodeBadRequest {
		t.Fatalf("bad range: %v", err)
	}
}

package daemon_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
)

// agents.list pages: the default and maximum page bound the reply, and paging by offset
// visits every session exactly once, newest first.
func TestAgentsListPaginates(t *testing.T) {
	dir := t.TempDir()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-time.Hour)
	const total = 7
	for i := range total {
		at := base.Add(time.Duration(i) * time.Minute)
		s := agents.Session{ID: fmt.Sprintf("list-%d", i), Kind: "claude", Machine: "local", Dir: t.TempDir(), Mode: agents.ModeStructured, Status: agents.StatusDone, StartedAt: at, UpdatedAt: at}
		if err := d.Agents.Store().SaveSession(s, ""); err != nil {
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
	c := control.NewClient(dir, "agents-list-test")
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
	var all []agents.Session
	if err := c.Call(ctx, "agents.list", nil, &all); err != nil || len(all) != total {
		t.Fatalf("default page: %d sessions, %v", len(all), err)
	}
	var seen []string
	for offset := 0; ; offset += 3 {
		var page []agents.Session
		if err := c.Call(ctx, "agents.list", map[string]int{"limit": 3, "offset": offset}, &page); err != nil {
			t.Fatal(err)
		}
		if len(page) > 3 {
			t.Fatalf("page of %d exceeds the limit", len(page))
		}
		for _, s := range page {
			seen = append(seen, s.ID)
		}
		if len(page) < 3 {
			break
		}
	}
	for i, s := range all {
		if seen[i] != s.ID || s.ID != fmt.Sprintf("list-%d", total-1-i) {
			t.Fatalf("paged order %v differs from newest-first full list at %d", seen, i)
		}
	}
	if len(seen) != total {
		t.Fatalf("paging visited %d sessions, want %d", len(seen), total)
	}
}

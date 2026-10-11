// Package performance holds the benchmarks whose results are recorded in
// docs/performance.md. Run: go test -run '^$' -bench . -benchmem ./tests/performance
package performance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/api"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func openDB(b *testing.B) *database.DB {
	b.Helper()
	db, err := database.Open(filepath.Join(b.TempDir(), "bench.db"), migrations.FS)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

// Audit rows are the highest-volume write the daemon does.
func BenchmarkSQLiteInsertAudit(b *testing.B) {
	db := openDB(b)
	i := 0
	for b.Loop() {
		i++
		if _, err := db.Exec(`INSERT INTO audit_events(at,actor,action,target,outcome,detail) VALUES(?,?,?,?,?,?)`,
			time.Now().Unix(), "bench", "terminal.create", fmt.Sprint(i), "allowed", "powershell"); err != nil {
			b.Fatal(err)
		}
	}
}

// One op = one transaction of 100 inserts.
func BenchmarkSQLiteInsertAuditTx100(b *testing.B) {
	db := openDB(b)
	for b.Loop() {
		tx, err := db.Begin()
		if err != nil {
			b.Fatal(err)
		}
		for range 100 {
			if _, err := tx.Exec(`INSERT INTO audit_events(at,actor,action,target,outcome,detail) VALUES(?,?,?,?,?,?)`,
				time.Now().Unix(), "bench", "x", "", "allowed", ""); err != nil {
				b.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

// Indexed range query over 100k rows (the audit log view's access pattern).
func BenchmarkSQLiteQueryAuditRecent(b *testing.B) {
	db := openDB(b)
	tx, _ := db.Begin()
	for i := range 100_000 {
		tx.Exec(`INSERT INTO audit_events(at,actor,action,target,outcome,detail) VALUES(?,?,?,?,?,?)`, i, "bench", "x", "", "allowed", "")
	}
	tx.Commit()
	for b.Loop() {
		rows, err := db.Query(`SELECT id,at,actor,action,target,outcome,detail FROM audit_events ORDER BY at DESC LIMIT 50`)
		if err != nil {
			b.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var id, at int64
			var a, ac, t, o, d string
			if err := rows.Scan(&id, &at, &a, &ac, &t, &o, &d); err != nil {
				b.Fatal(err)
			}
			n++
		}
		rows.Close()
		if n != 50 {
			b.Fatalf("got %d rows", n)
		}
	}
}

func BenchmarkSQLiteSettingsUpsert(b *testing.B) {
	db := openDB(b)
	i := 0
	for b.Loop() {
		i++
		if _, err := db.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			"theme", `"midnight"`, i); err != nil {
			b.Fatal(err)
		}
	}
}

// IPC protocol cost alone: a no-op echo handler on the real named pipe with auth.
func BenchmarkIPCEchoRoundTrip(b *testing.B) {
	ep := ipc.Endpoint(b.TempDir())
	ln, err := ipc.Listen(ep)
	if err != nil {
		b.Fatal(err)
	}
	s := ipc.NewServer("tok", "bench", 1, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Handle("echo", func(ctx context.Context, p json.RawMessage) (any, error) { return p, nil })
	go s.Serve(ln)
	b.Cleanup(func() { ln.Close(); s.CloseAll() })
	c := ipc.NewClient(ipc.Dial(ep), "tok", "bench")
	if err := c.Connect(context.Background()); err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	var out map[string]int
	i := 0
	for b.Loop() {
		i++
		if err := c.Call(ctx, "echo", map[string]int{"a": i}, &out); err != nil {
			b.Fatal(err)
		}
	}
}

// Full path through a real daemon.Daemon (DB read for schema version included).
func BenchmarkDaemonHealthRoundTrip(b *testing.B) {
	dir := b.TempDir()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	b.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := control.Running(ctx, dir); err == nil {
			break
		}
		if time.Now().After(deadline) {
			b.Fatal("daemon did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	c := control.NewClient(dir, "bench")
	if err := c.Connect(ctx); err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	var h api.Health
	for b.Loop() {
		if err := c.Call(ctx, "daemon.health", struct{}{}, &h); err != nil {
			b.Fatal(err)
		}
	}
}

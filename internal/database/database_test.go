package database_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func TestMigrateIsIdempotentAndEnforcesForeignKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "o.db")
	db, err := database.Open(p, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := db.Version()
	db.Close()
	db, err = database.Open(p, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v2, _ := db.Version(); v2 != v1 || v1 < 1 {
		t.Fatalf("version %d -> %d", v1, v2)
	}
	if _, err := db.Exec(`INSERT INTO mcp_permissions(server_id,tool,decision) VALUES('nope','t','allow')`); err == nil {
		t.Fatal("foreign key not enforced")
	}
}

func TestPragmasAndCloseCheckpoint(t *testing.T) {
	p := filepath.Join(t.TempDir(), "o.db")
	db, err := database.Open(p, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	var ts, jsl int
	db.QueryRow(`PRAGMA temp_store`).Scan(&ts)
	db.QueryRow(`PRAGMA journal_size_limit`).Scan(&jsl)
	if ts != 2 || jsl != 67108864 {
		t.Fatalf("temp_store=%d journal_size_limit=%d", ts, jsl)
	}
	if _, err := db.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('k','1',0)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if fi, err := os.Stat(p + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatalf("wal not truncated: %d bytes", fi.Size())
	}
}

// Two read-then-write transactions racing must both commit: with deferred BEGIN the loser gets
// SQLITE_BUSY_SNAPSHOT immediately, with _txlock=immediate it waits for the lock.
func TestReadThenWriteTxDoesNotFailBusySnapshot(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "o.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				tx, err := db.Begin()
				if err != nil {
					errs <- err
					return
				}
				var n int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&n); err != nil {
					tx.Rollback()
					errs <- err
					return
				}
				if _, err := tx.Exec(`INSERT INTO settings(key,value,updated_at) VALUES(?,?,0)`, fmt.Sprintf("k%d-%d", i, j), "1"); err != nil {
					tx.Rollback()
					errs <- err
					return
				}
				if err := tx.Commit(); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

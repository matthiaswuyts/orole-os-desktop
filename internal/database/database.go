// Package database opens the local SQLite metadata store and applies migrations.
package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps *sql.DB.
type DB struct{ *sql.DB }

// Open opens (creating if needed) the database at path and migrates it using migs.
func Open(path string, migs fs.FS) (*DB, error) {
	dsn := "file:" + path + "?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)" +
		"&_pragma=temp_store(MEMORY)&_pragma=cache_size(-16000)&_pragma=journal_size_limit(67108864)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// _txlock=immediate makes every Begin take the write lock up front, so read-then-write
	// transactions wait on busy_timeout instead of failing with SQLITE_BUSY_SNAPSHOT.
	sdb.SetMaxOpenConns(4)
	sdb.SetMaxIdleConns(4) // default of 2 would churn connections (and their page caches) under 4 concurrent users
	db := &DB{sdb}
	if err := db.Migrate(migs); err != nil {
		sdb.Close()
		return nil, err
	}
	return db, nil
}

// Close refreshes planner statistics, folds the WAL back into the main file and closes the pool.
// The maintenance steps are best effort: a failure must not prevent closing.
func (d *DB) Close() error {
	_, _ = d.Exec(`PRAGMA optimize`)
	_, _ = d.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return d.DB.Close()
}

// Migrate applies every NNNN_name.sql not yet recorded, each in a transaction.
func (d *DB) Migrate(migs fs.FS) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT (unixepoch()))`); err != nil {
		return err
	}
	entries, err := fs.Glob(migs, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
		v, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %q: bad version prefix", name)
		}
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, v).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := fs.ReadFile(migs, name)
		if err != nil {
			return err
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Version returns the highest applied migration.
func (d *DB) Version() (int, error) {
	var v sql.NullInt64
	err := d.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

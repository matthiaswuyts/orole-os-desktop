package database_test

import (
	"path/filepath"
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

func TestReadSchemaVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "orole.db")
	if _, err := database.ReadSchemaVersion(p); err == nil {
		t.Fatal("expected error for missing db")
	}
	db, err := database.Open(p, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := db.Version()
	db.Close()
	if got, err := database.ReadSchemaVersion(p); err != nil || got != want {
		t.Fatalf("ReadSchemaVersion = %d, %v; want %d", got, err, want)
	}
}

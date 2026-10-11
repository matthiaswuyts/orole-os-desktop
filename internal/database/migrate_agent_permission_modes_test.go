package database_test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// 0100 renames stored Claude Code permission modes to Orole's manual | auto | yolo.
func TestMigrationAgentPermissionModes(t *testing.T) {
	before := fstest.MapFS{}
	entries, _ := fs.Glob(migrations.FS, "*.sql")
	for _, name := range entries {
		if strings.Compare(name, "0100") < 0 {
			b, _ := fs.ReadFile(migrations.FS, name)
			before[name] = &fstest.MapFile{Data: b}
		}
	}
	db, err := database.Open(filepath.Join(t.TempDir(), "o.db"), before)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for id, data := range map[string]string{
		"empty":   `{"id":"empty","permissionMode":"","title":"keep"}`,
		"default": `{"id":"default","permissionMode":"default"}`,
		"missing": `{"id":"missing"}`,
		"edits":   `{"id":"edits","permissionMode":"acceptEdits"}`,
		"plan":    `{"id":"plan","permissionMode":"plan"}`,
		"yolo":    `{"id":"yolo","permissionMode":"yolo"}`,
	} {
		if _, err := db.Exec(`INSERT INTO agent_sessions(id,kind,status,project,dir,parent_id,grp,journal_key,started_at,ended_at,updated_at,data)
			VALUES(?,'claude','idle','','','','',NULL,1,NULL,1,?)`, id, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(migrations.FS); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"empty": "manual", "default": "manual", "missing": "manual", "edits": "auto", "plan": "plan", "yolo": "yolo"} {
		var got string
		if err := db.QueryRow(`SELECT json_extract(data, '$.permissionMode') FROM agent_sessions WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: permissionMode = %q, want %q", id, got, want)
		}
	}
	var title string
	if err := db.QueryRow(`SELECT json_extract(data, '$.title') FROM agent_sessions WHERE id = 'empty'`).Scan(&title); err != nil || title != "keep" {
		t.Errorf("other fields changed: %q %v", title, err)
	}
}

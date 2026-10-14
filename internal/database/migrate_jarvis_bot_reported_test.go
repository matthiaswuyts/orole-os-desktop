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

// 0111 widens the jarvis_rules trigger CHECK to bot_reported by rebuilding the table: the rules saved before it stay.
func TestMigrationJarvisBotReported(t *testing.T) {
	before := fstest.MapFS{}
	entries, _ := fs.Glob(migrations.FS, "*.sql")
	for _, name := range entries {
		if strings.Compare(name, "0111") < 0 {
			b, _ := fs.ReadFile(migrations.FS, name)
			before[name] = &fstest.MapFile{Data: b}
		}
	}
	db, err := database.Open(filepath.Join(t.TempDir(), "o.db"), before)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO bots(id,name,created_at,updated_at) VALUES('b1','Atlas',0,0)`); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO jarvis_rules(id,bot_id,trigger,target,repeat,urgent,quiet_override,note,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`
	if _, err := db.Exec(insert, "r1", "b1", "task_finished", "any", 1, 1, 0, "deploy", nil, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insert, "r2", "b1", "at_time", "08:00", 0, 0, 1, "", 99, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insert, "r0", "b1", "bot_reported", "Nova", 0, 0, 0, "", nil, 30); err == nil {
		t.Fatal("bot_reported was accepted before the migration")
	}
	if err := db.Migrate(migrations.FS); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(`SELECT id,bot_id,trigger,target,repeat,urgent,quiet_override,note,COALESCE(expires_at,0),created_at FROM jarvis_rules ORDER BY created_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id, bot, trig, target, note string
		var repeat, urgent, quiet, exp, created int64
		if err := rows.Scan(&id, &bot, &trig, &target, &repeat, &urgent, &quiet, &note, &exp, &created); err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{id, bot, trig, target, note}, "|")+
			"|"+strings.Repeat("x", int(repeat))+strings.Repeat("u", int(urgent))+strings.Repeat("q", int(quiet)))
		if id == "r2" && (exp != 99 || created != 20) {
			t.Errorf("r2 times: expires %d created %d", exp, created)
		}
	}
	want := []string{"r1|b1|task_finished|any|deploy|xu", "r2|b1|at_time|08:00||q"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rules after the migration: %v, want %v", got, want)
	}

	if _, err := db.Exec(insert, "r3", "b1", "bot_reported", "Nova", 0, 0, 0, "", nil, 30); err != nil {
		t.Fatalf("bot_reported rejected after the migration: %v", err)
	}
	if _, err := db.Exec(insert, "r4", "b1", "bogus", "x", 0, 0, 0, "", nil, 40); err == nil {
		t.Error("an unknown trigger was accepted")
	}
	var idx int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='jarvis_rules_trigger' AND tbl_name='jarvis_rules'`).Scan(&idx); err != nil || idx != 1 {
		t.Errorf("trigger index: %d %v", idx, err)
	}
	// Deleting the bot still removes its rules (the foreign key survived the rebuild).
	if _, err := db.Exec(`DELETE FROM bots WHERE id='b1'`); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jarvis_rules`).Scan(&left); err != nil || left != 0 {
		t.Errorf("rules left after the bot was deleted: %d %v", left, err)
	}
}

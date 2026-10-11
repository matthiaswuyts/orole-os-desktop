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

// 0099 turns a bot's per-tool MCP allow-list into the set of servers it names.
func TestMigrationBotMCPToolsBecomeServers(t *testing.T) {
	before := fstest.MapFS{}
	entries, _ := fs.Glob(migrations.FS, "*.sql")
	for _, name := range entries {
		if strings.Compare(name, "0099") < 0 {
			b, _ := fs.ReadFile(migrations.FS, name)
			before[name] = &fstest.MapFile{Data: b}
		}
	}
	db, err := database.Open(filepath.Join(t.TempDir(), "o.db"), before)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for id, cfg := range map[string]string{
		"a": `{"tools":[],"mcpTools":["srv1/read","srv1/write","srv2/list"],"skills":["x"]}`,
		"b": `{"tools":[],"mcpTools":[],"skills":[]}`,
		"c": `{"tools":[],"skills":[]}`,
	} {
		if _, err := db.Exec(`INSERT INTO bots(id, name, config, created_at, updated_at) VALUES(?, ?, ?, 1, 1)`, id, id, cfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(migrations.FS); err != nil {
		t.Fatal(err)
	}
	get := func(id, path string) string {
		var v *string
		if err := db.QueryRow(`SELECT json_extract(config, ?) FROM bots WHERE id = ?`, path, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v == nil {
			return "<null>"
		}
		return *v
	}
	if got := get("a", "$.mcpServers"); got != `["srv1","srv2"]` {
		t.Errorf("servers = %s", got)
	}
	if got := get("a", "$.mcpTools"); got != "<null>" {
		t.Errorf("old key survived: %s", got)
	}
	if got := get("a", "$.skills"); got != `["x"]` {
		t.Errorf("other settings changed: %s", got)
	}
	if got := get("b", "$.mcpServers"); got != `[]` {
		t.Errorf("empty list -> %s", got)
	}
	if got := get("c", "$.mcpServers"); got != "<null>" {
		t.Errorf("a bot without the key was changed: %s", got)
	}
}

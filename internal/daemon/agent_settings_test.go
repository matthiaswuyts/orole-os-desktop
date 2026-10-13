package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// The launcher saves the user's mode as the "agents" setting's defaultPermissionMode. The runtime
// reads these accessors on every start, so a saved value reaches user and bot starts alike, and a
// bot only gets YOLO while botYolo (and YOLO itself) are on.
func TestStoredPermissionPreferenceAccessors(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	d := &Daemon{DB: db}
	save := func(v map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(v)
		if _, err := d.DB.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('agents',?,0) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.defaultPermissionMode(); got != "" || d.botYoloEnabled() {
		t.Fatalf("nothing saved must mean manual and no bot yolo: %q %v", got, d.botYoloEnabled())
	}
	save(map[string]any{"defaultPermissionMode": "auto"})
	if got := d.defaultPermissionMode(); got != "auto" {
		t.Fatalf("saved auto: %q", got)
	}
	save(map[string]any{"defaultPermissionMode": "yolo"})
	if got := d.defaultPermissionMode(); got != "yolo" || d.botYoloEnabled() {
		t.Fatalf("saved yolo must not enable bot yolo: %q %v", got, d.botYoloEnabled())
	}
	save(map[string]any{"defaultPermissionMode": "yolo", "botYolo": true})
	if !d.botYoloEnabled() {
		t.Fatal("botYolo allowed")
	}
	save(map[string]any{"defaultPermissionMode": "yolo", "botYolo": true, "yoloDisabled": true})
	if d.botYoloEnabled() {
		t.Fatal("yoloDisabled must override botYolo")
	}
}

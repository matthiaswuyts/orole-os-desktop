package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	var sb strings.Builder
	for i := 1; i <= 5000; i++ { // > 256 KiB window forces the partial-line path
		fmt.Fprintf(&sb, "line %d %s\n", i, strings.Repeat("p", 80))
	}
	os.WriteFile(p, []byte(sb.String()), 0o600)
	got, err := TailFile(p, 3)
	if err != nil || len(got) != 3 || !strings.HasPrefix(got[2], "line 5000 ") || !strings.HasPrefix(got[0], "line 4998 ") {
		t.Fatalf("tail wrong: %v %v", got, err)
	}
	if _, err := TailFile(filepath.Join(t.TempDir(), "none"), 3); err == nil {
		t.Fatal("expected error for missing file")
	}
}

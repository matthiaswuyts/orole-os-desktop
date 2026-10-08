package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFileKeepsFiveAndNeverSplits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daemon.log")
	r, err := OpenRotating(p, 100, 5)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 39) + "\n" // 40 bytes: 2 fit per file
	for range 40 {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	r.Close()
	for i := 1; i <= 5; i++ {
		if _, err := os.Stat(fmt.Sprintf("%s.%d", p, i)); err != nil {
			t.Fatalf("missing rotated file %d: %v", i, err)
		}
	}
	if _, err := os.Stat(p + ".6"); err == nil {
		t.Fatal("kept more than 5 rotated files")
	}
	for _, f := range []string{p, p + ".1", p + ".5"} {
		b, _ := os.ReadFile(f)
		if len(b) > 100 || len(b)%40 != 0 {
			t.Fatalf("%s has %d bytes: size cap exceeded or write split", f, len(b))
		}
	}
}

func TestRotatingFileResumesSize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d.log")
	os.WriteFile(p, []byte(strings.Repeat("a", 90)), 0o600)
	r, _ := OpenRotating(p, 100, 2)
	r.Write([]byte(strings.Repeat("b", 20)))
	r.Close()
	if b, _ := os.ReadFile(p + ".1"); len(b) != 90 {
		t.Fatalf("existing content not rotated, .1 has %d bytes", len(b))
	}
}

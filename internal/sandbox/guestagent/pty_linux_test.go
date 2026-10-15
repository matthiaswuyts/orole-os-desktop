//go:build linux

package guestagent

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

func readUntil(t *testing.T, r io.Reader, want string) {
	t.Helper()
	found := make(chan struct{})
	go func() {
		var got []byte
		buf := make([]byte, 1024)
		for {
			n, err := r.Read(buf)
			got = append(got, buf[:n]...)
			if bytes.Contains(got, []byte(want)) {
				close(found)
				return
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-found:
	case <-time.After(5 * time.Second):
		t.Fatalf("never read %q", want)
	}
}

func TestStartPTYRunsShell(t *testing.T) {
	p, err := startPTY(PTYSpec{Argv: []string{"/bin/sh"}, Dir: t.TempDir(), Env: append(os.Environ(), "TERM=xterm-256color"), Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	io.WriteString(p, "stty size\n")
	readUntil(t, p, "24 80")
	if err := p.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	io.WriteString(p, "stty size; exit 3\n")
	readUntil(t, p, "30 100")
	go io.Copy(io.Discard, p)
	if code, _ := p.Wait(); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
}

//go:build linux

package guestagent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/wire"
)

func TestSetIdentityWiresCredential(t *testing.T) {
	cmd := exec.Command("true")
	setProcessGroup(cmd)
	if err := setIdentity(cmd, &Identity{Name: "bot", UID: 1000, GID: 100, Groups: []uint32{100, 27}}); err != nil {
		t.Fatal(err)
	}
	a := cmd.SysProcAttr
	if a == nil || !a.Setpgid || a.Credential == nil {
		t.Fatalf("SysProcAttr = %+v", a)
	}
	if c := a.Credential; c.Uid != 1000 || c.Gid != 100 || !slices.Equal(c.Groups, []uint32{100, 27}) || c.NoSetGroups {
		t.Fatalf("Credential = %+v", c)
	}

	plain := exec.Command("true")
	if err := setIdentity(plain, nil); err != nil || plain.SysProcAttr != nil {
		t.Fatalf("nil identity changed the command: %+v %v", plain.SysProcAttr, err)
	}
}

func TestIdentityEnv(t *testing.T) {
	env := identityEnv([]string{"PATH=/bin", "USER=root"}, "/home/bot", &Identity{Name: "bot"})
	want := []string{"PATH=/bin", "USER=root", "HOME=/home/bot", "USER=bot", "LOGNAME=bot"}
	if !slices.Equal(env, want) {
		t.Fatalf("env = %q", env)
	}
	if env := identityEnv([]string{"PATH=/bin"}, "/h", nil); !slices.Equal(env, []string{"PATH=/bin", "HOME=/h"}) {
		t.Fatalf("env without identity = %q", env)
	}
}

// runAsNobody returns an identity for the nobody user and a home it can use; it skips unless the test runs as root.
func runAsNobody(t *testing.T) (*Identity, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root to switch users")
	}
	u, err := user.Lookup("nobody")
	if err != nil {
		t.Skip("no nobody user:", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	home, err := os.MkdirTemp("", "orole-runas-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	if err := os.Chown(home, uid, gid); err != nil {
		t.Fatal(err)
	}
	return &Identity{Name: u.Username, UID: uint32(uid), GID: uint32(gid)}, home
}

func TestRunAsSwitchesUser(t *testing.T) {
	id, home := runAsNobody(t)
	a := New(Config{Token: "t", Home: home, RunAs: id})

	res, err := a.exec(t.Context(), wire.ExecRequest{Command: `id -un; id -u; echo "$HOME $USER $LOGNAME"; touch made`})
	if err != nil {
		t.Fatal(err)
	}
	want := id.Name + "\n" + strconv.Itoa(int(id.UID)) + "\n" + home + " " + id.Name + " " + id.Name + "\n"
	if res.Stdout != want || res.ExitCode != 0 {
		t.Fatalf("exec as %s: %+v, want stdout %q", id.Name, res, want)
	}
	assertOwner(t, filepath.Join(home, "made"), id.UID)

	p, err := startPTY(PTYSpec{Argv: []string{"/bin/sh"}, Dir: home, Env: identityEnv(os.Environ(), home, id), Cols: 80, Rows: 24, RunAs: id})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	io.WriteString(p, "echo uid=$(id -u) tty=$(stat -c %u $(tty))\n")
	u := strconv.Itoa(int(id.UID))
	readUntil(t, p, "uid="+u+" tty="+u)
}

func TestWriteFileAsRunAsOwnsNewPathsOnly(t *testing.T) {
	id, home := runAsNobody(t)
	a := New(Config{Token: "t", Home: home, RunAs: id})
	existing := filepath.Join(home, "rootfile")
	if err := os.WriteFile(existing, nil, 0o644); err != nil { // created by root, stays root's
		t.Fatal(err)
	}
	for _, p := range []string{"a/b/new.txt", "rootfile"} {
		rec := callInternal(t, a, "/files/write", wire.WriteFileRequest{Path: p, Content: []byte("x")})
		if !strings.Contains(rec, `"bytes":1`) {
			t.Fatalf("write %s: %s", p, rec)
		}
	}
	assertOwner(t, filepath.Join(home, "a"), id.UID)
	assertOwner(t, filepath.Join(home, "a/b"), id.UID)
	assertOwner(t, filepath.Join(home, "a/b/new.txt"), id.UID)
	assertOwner(t, existing, 0)
}

func assertOwner(t *testing.T, p string, uid uint32) {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Sys().(*syscall.Stat_t).Uid; got != uid {
		t.Errorf("%s owned by %d, want %d", p, got, uid)
	}
}

func callInternal(t *testing.T, a *Agent, path string, body any) string {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://guest"+path, bytes.NewReader(b))
	req.Header.Set(wire.TokenHeader, a.cfg.Token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}

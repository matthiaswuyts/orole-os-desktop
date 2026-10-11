package machines

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
	"golang.org/x/crypto/ssh"
)

type testPolicy struct {
	decision policy.Decision
	mu       sync.Mutex
	seen     []policy.Request
}

func (p *testPolicy) record(r policy.Request) {
	p.mu.Lock()
	p.seen = append(p.seen, r)
	p.mu.Unlock()
}
func (p *testPolicy) Check(_ context.Context, r policy.Request) (policy.Decision, error) {
	p.record(r)
	return p.decision, nil
}
func (p *testPolicy) Decide(_ context.Context, r policy.Request) (policy.Evaluation, error) {
	p.record(r)
	return policy.Evaluation{Decision: p.decision}, nil
}
func testManager(t *testing.T) (*Manager, *testPolicy) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "machines.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	p := &testPolicy{decision: policy.Allow}
	m := New(db.DB, credentials.NewMemory(), p)
	t.Cleanup(m.Close)
	return m, p
}
func signer(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, pem.EncodeToMemory(block)
}
func TestPinnedIdentityRejectsChanges(t *testing.T) {
	one, _ := signer(t)
	two, _ := signer(t)
	key := one.PublicKey()
	callback := pinnedCallback(base64.StdEncoding.EncodeToString(key.Marshal()), ssh.FingerprintSHA256(key))
	if err := callback("host", nil, key); err != nil {
		t.Fatal(err)
	}
	if err := callback("host", nil, two.PublicKey()); !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("changed key accepted: %v", err)
	}
	if err := pinnedCallback("", "")("host", nil, key); !errors.Is(err, ErrHostKeyChanged) {
		t.Fatal("empty trust accepted")
	}
}
func TestCommandsPreserveArguments(t *testing.T) {
	command, err := remoteCommand("posix", "/usr/bin/printf", []string{"%s", "a'b; $(touch /tmp/pwn)", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := "exec '/usr/bin/printf' '%s' 'a'\"'\"'b; $(touch /tmp/pwn)' ''"
	if command != want {
		t.Fatalf("got %q want %q", command, want)
	}
	command, err = remoteCommand("powershell", "C:\\Program Files\\tool.exe", []string{"a'b; $(bad)", "héllo"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand "))
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	script := string(utf16.Decode(u))
	if !strings.Contains(script, "$p.StartInfo.FileName = 'C:\\Program Files\\tool.exe'") || !strings.Contains(script, `$p.StartInfo.Arguments = '"a''b; $(bad)" "héllo"'`) {
		t.Fatalf("unsafe script: %s", script)
	}
	if _, err = remoteCommand("posix", "echo", []string{"bad\x00arg"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("NUL accepted")
	}
}
func TestWindowsNativeArgumentQuoting(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"", `""`}, {`a b`, `"a b"`}, {`a"b`, `"a\"b"`}, {`C:\dir\`, `"C:\dir\\"`}, {`a\"b`, `"a\\\"b"`}} {
		if got := quoteWindowsArg(tc.in); got != tc.want {
			t.Errorf("quote(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
func TestMachineAccessIsSeparateAndFailsClosed(t *testing.T) {
	m, p := testManager(t)
	ctx := context.Background()
	root := t.TempDir()
	outside := t.TempDir()
	file := filepath.Join(root, "note.txt")
	os.WriteFile(file, []byte("hello"), 0600)
	if _, err := m.ReadFile(ctx, "user", FileReadRequest{MachineID: "local", Path: file}); !errors.Is(err, files.ErrDenied) {
		t.Fatalf("unapproved read: %v", err)
	}
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	read, err := m.ReadFile(ctx, "user", FileReadRequest{MachineID: "local", Path: file})
	if err != nil || string(read.Data) != "hello" {
		t.Fatalf("read: %s %v", read.Data, err)
	}
	if _, err = m.ListFiles(ctx, "user", FileRequest{"local", outside}); !errors.Is(err, files.ErrDenied) {
		t.Fatalf("outside read: %v", err)
	}
	if _, err = m.WriteFile(ctx, "user", FileWriteRequest{MachineID: "local", Path: file, Data: []byte("bad")}); !errors.Is(err, ErrDenied) {
		t.Fatalf("write without grant: %v", err)
	}
	if _, err = m.Execute(ctx, "user", Command{ID: "local", OperationID: uuid.NewString(), Program: "anything"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("command without grant: %v", err)
	}
	p.decision = policy.Deny
	if _, err = m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{outside}, AllowCommands: true}); !errors.Is(err, ErrDenied) {
		t.Fatalf("policy deny ignored: %v", err)
	}
	if p.seen[len(p.seen)-1].Resource != "machine/local" {
		t.Fatal("missing machine policy namespace")
	}
}
func TestRemoteNamespaceCannotSelectLocal(t *testing.T) {
	for _, input := range []string{"/local/C:/secret", "C:\\secret", "/bad/etc/passwd", "/"} {
		if _, err := splitRemote(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	id := uuid.NewString()
	r, err := splitRemote("/" + id + "/home/alice/../bob")
	if err != nil || r.Path != "/home/bob" {
		t.Fatalf("normalize: %+v %v", r, err)
	}
	if within("/home/alice", "/home/alice-other/secret") {
		t.Fatal("sibling prefix escaped root")
	}
	if within("/C:/Users/Alice", "/C:/Users/Alice2") {
		t.Fatal("Windows sibling prefix escaped root")
	}
}
func TestPairAuthenticatesAndPersistsOnlyPublicIdentity(t *testing.T) {
	host, _ := signer(t)
	client, key := signer(t)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) != string(client.PublicKey().Marshal()) {
			return nil, errors.New("wrong key")
		}
		return nil, nil
	}}
	cfg.AddHostKey(host)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				server, channels, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					conn.Close()
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					ch.Reject(ssh.Prohibited, "test only authenticates")
				}
			}()
		}
	}()
	m, _ := testManager(t)
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err = os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := m.Probe(context.Background(), "user", ProbeRequest{listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	machine, err := m.Pair(context.Background(), "user", PairRequest{Name: "test endpoint", Address: identity.Address, Username: "test", OS: "linux", Fingerprint: identity.Fingerprint, PrivateKeyPath: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := m.Get(context.Background(), machine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.hostKey == "" || stored.Fingerprint != ssh.FingerprintSHA256(host.PublicKey()) {
		t.Fatal("identity not pinned")
	}
	if stored.AllowCommands || stored.AllowWrite || len(stored.Roots) > 0 {
		t.Fatal("pairing granted capabilities")
	}
	health, err := m.Health(context.Background(), "user", machine.ID)
	if err != nil || !health.Reachable {
		t.Fatalf("reconnect: %+v %v", health, err)
	}
	var dbText string
	if err = m.db.QueryRow(`SELECT host_key || fingerprint || address || username || roots FROM machines WHERE id=?`, machine.ID).Scan(&dbText); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dbText, "PRIVATE KEY") {
		t.Fatal("private key stored in SQLite")
	}
}

package machines

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"golang.org/x/crypto/ssh"
)

type countingReader struct {
	sftp.FileReader
	reads *atomic.Int64
}

func (c countingReader) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	c.reads.Add(1)
	return c.FileReader.Fileread(r)
}

// sftpServer is an in-process SSH server whose sftp subsystem serves an
// in-memory filesystem. It counts connections so reuse is observable.
type sftpServer struct {
	addr     string
	host     ssh.Signer
	client   ssh.Signer
	accepted atomic.Int64
	live     atomic.Int64
	reads    atomic.Int64
	mu       sync.Mutex
	conns    []*ssh.ServerConn
}

func newSFTPServer(t *testing.T) (*sftpServer, []byte) {
	t.Helper()
	host, _ := signer(t)
	client, key := signer(t)
	s := &sftpServer{host: host, client: client}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) != string(client.PublicKey().Marshal()) {
			return nil, errors.New("wrong key")
		}
		return nil, nil
	}}
	cfg.AddHostKey(host)
	handlers := sftp.InMemHandler()
	handlers.FileGet = countingReader{handlers.FileGet, &s.reads}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	s.addr = listener.Addr().String()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			s.accepted.Add(1)
			s.live.Add(1)
			go func() {
				defer s.live.Add(-1)
				server, channels, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					conn.Close()
					return
				}
				s.mu.Lock()
				s.conns = append(s.conns, server)
				s.mu.Unlock()
				go ssh.DiscardRequests(requests)
				go func() {
					for ch := range channels {
						channel, reqs, err := ch.Accept()
						if err != nil {
							continue
						}
						go func() {
							for r := range reqs {
								ok := r.Type == "subsystem" && len(r.Payload) >= 8 && string(r.Payload[4:]) == "sftp"
								_ = r.Reply(ok, nil)
								if ok {
									go func() { _ = sftp.NewRequestServer(channel, handlers).Serve(); _ = channel.Close() }()
								}
							}
						}()
					}
				}()
				_ = server.Wait()
			}()
		}
	}()
	return s, key
}

// admin runs fn against the server with a throwaway connection.
func (s *sftpServer) admin(t *testing.T, fn func(*sftp.Client)) {
	t.Helper()
	c, err := ssh.Dial("tcp", s.addr, &ssh.ClientConfig{User: "admin", Auth: []ssh.AuthMethod{ssh.PublicKeys(s.client)}, HostKeyCallback: ssh.FixedHostKey(s.host.PublicKey())})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fs, err := sftp.NewClient(c)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	fn(fs)
}

func (s *sftpServer) closeConns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
	s.conns = nil
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pairedRemote registers a remote machine in m pointing at s, with /data
// approved for reads and writes, and returns its ID.
func pairedRemote(t *testing.T, m *Manager, s *sftpServer, key []byte) string {
	t.Helper()
	id := uuid.NewString()
	hostKey := base64.StdEncoding.EncodeToString(s.host.PublicKey().Marshal())
	if _, err := m.db.Exec(`INSERT INTO machines(id,name,kind,os,transport,backend,address,username,fingerprint,host_key,roots,allow_commands,allow_write,created_at) VALUES(?,'remote','remote','linux','ssh','posix',?,'account',?,?,'["/data"]',0,1,0)`, id, s.addr, ssh.FingerprintSHA256(s.host.PublicKey()), hostKey); err != nil {
		t.Fatal(err)
	}
	if err := m.secrets.Set("machines/"+id+"/key", key); err != nil {
		t.Fatal(err)
	}
	s.admin(t, func(fs *sftp.Client) {
		if err := fs.Mkdir("/data"); err != nil {
			t.Fatal(err)
		}
	})
	return id
}

func TestPoolReusesOneSessionAndEtagReadsNoContent(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	ctx := context.Background()
	base := s.accepted.Load()
	info, err := m.WriteFile(ctx, "user", FileWriteRequest{MachineID: id, Path: "/data/a.txt", Data: []byte("hello"), Options: files.WriteOptions{Create: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Etag, "v1:") || !strings.HasSuffix(info.Etag, ":5:") {
		t.Fatalf("etag should be mtime+size without a hash: %q", info.Etag)
	}
	s.reads.Store(0)
	stat, err := m.StatFile(ctx, "user", FileRequest{MachineID: id, Path: "/data/a.txt"})
	if err != nil || stat.Etag != info.Etag {
		t.Fatalf("stat: %+v %v", stat, err)
	}
	list, err := m.ListFiles(ctx, "user", FileRequest{MachineID: id, Path: "/data"})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if n := s.reads.Load(); n != 0 {
		t.Fatalf("stat/list opened file content %d times", n)
	}
	read, err := m.ReadFile(ctx, "user", FileReadRequest{MachineID: id, Path: "/data/a.txt"})
	if err != nil || string(read.Data) != "hello" || read.Info.Etag != info.Etag {
		t.Fatalf("read: %+v %v", read, err)
	}
	if n := s.reads.Load(); n != 1 {
		t.Fatalf("read should open the file once, opened %d times", n)
	}
	if n := s.accepted.Load() - base; n != 1 {
		t.Fatalf("expected one SSH connection for all operations, got %d", n)
	}
}

func TestPoolCoalescesConcurrentDials(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	base := s.accepted.Load()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.ListFiles(context.Background(), "user", FileRequest{MachineID: id, Path: "/data"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := s.accepted.Load() - base; n != 1 {
		t.Fatalf("concurrent operations dialed %d times", n)
	}
}

func TestPoolClosesIdleSession(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	m.pool.idle = 40 * time.Millisecond
	id := pairedRemote(t, m, s, key)
	base := s.accepted.Load()
	req := FileRequest{MachineID: id, Path: "/data"}
	if _, err := m.ListFiles(context.Background(), "user", req); err != nil {
		t.Fatal(err)
	}
	eventually(t, "idle session to close", func() bool { return m.pool.size() == 0 && s.live.Load() == 0 })
	if _, err := m.ListFiles(context.Background(), "user", req); err != nil {
		t.Fatal(err)
	}
	if n := s.accepted.Load() - base; n != 2 {
		t.Fatalf("operation after idle close should dial again, dials=%d", n)
	}
}

func TestPoolDoesNotCloseSessionInUse(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	m.pool.idle = 30 * time.Millisecond
	id := pairedRemote(t, m, s, key)
	v, err := m.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := m.pool.acquire(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	if _, err := lease.FS.Stat("/data"); err != nil {
		t.Fatalf("session closed while leased: %v", err)
	}
	lease.Release()
	eventually(t, "released session to expire", func() bool { return m.pool.size() == 0 })
}

func TestPoolClosesOnAccessEditAndRemoval(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	ctx := context.Background()
	req := FileRequest{MachineID: id, Path: "/data"}
	if _, err := m.ListFiles(ctx, "user", req); err != nil {
		t.Fatal(err)
	}
	if m.pool.size() != 1 {
		t.Fatal("session not pooled")
	}
	if _, err := m.SetAccess(ctx, "user", Access{ID: id, Roots: []string{"/data"}}); err != nil {
		t.Fatal(err)
	}
	if m.pool.size() != 0 {
		t.Fatal("access edit left a pooled session")
	}
	eventually(t, "edited machine's connection to close", func() bool { return s.live.Load() == 0 })
	if _, err := m.ListFiles(ctx, "user", req); err != nil {
		t.Fatal(err)
	}
	if m.pool.size() != 1 {
		t.Fatal("session not pooled after edit")
	}
	if err := m.Remove(ctx, "user", id); err != nil {
		t.Fatal(err)
	}
	if m.pool.size() != 0 {
		t.Fatal("removal left a pooled session")
	}
	eventually(t, "removed machine's connection to close", func() bool { return s.live.Load() == 0 })
	if _, err := m.ListFiles(ctx, "user", req); err == nil {
		t.Fatal("removed machine still reachable")
	}
}

func TestPoolManagerCloseClosesSessions(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	if _, err := m.ListFiles(context.Background(), "user", FileRequest{MachineID: id, Path: "/data"}); err != nil {
		t.Fatal(err)
	}
	m.Close()
	if m.pool.size() != 0 {
		t.Fatal("close left a pooled session")
	}
	eventually(t, "connection to close", func() bool { return s.live.Load() == 0 })
}

func TestPoolRedialsAfterConnectionLoss(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	req := FileRequest{MachineID: id, Path: "/data"}
	if _, err := m.ListFiles(context.Background(), "user", req); err != nil {
		t.Fatal(err)
	}
	base := s.accepted.Load()
	s.closeConns()
	eventually(t, "dead session to be evicted", func() bool { return m.pool.size() == 0 })
	if _, err := m.ListFiles(context.Background(), "user", req); err != nil {
		t.Fatalf("operation after connection loss: %v", err)
	}
	if s.accepted.Load()-base != 1 {
		t.Fatal("expected a fresh connection")
	}
}

func TestPoolEvictsSessionWhenOperationContextEnds(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	v, err := m.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// A normal release must survive the context ending afterwards.
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := m.pool.acquire(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	cancel()
	time.Sleep(50 * time.Millisecond)
	if m.pool.size() != 1 {
		t.Fatal("released session was evicted by its old context")
	}
	// Cancelling while the session is in use closes it: one shared socket cannot
	// cancel a single in-flight request.
	ctx, cancel = context.WithCancel(context.Background())
	lease, err = m.pool.acquire(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	eventually(t, "cancelled session to be evicted", func() bool { return m.pool.size() == 0 })
	lease.Release()
	eventually(t, "connection to close", func() bool { return s.live.Load() == 0 })
}

func TestPoolDiscardsDialRacingAccessChange(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	v, err := m.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	m.pool.dial = func(ctx context.Context, v Machine) (*ssh.Client, *sftp.Client, error) {
		close(entered)
		<-release
		return m.dialSession(ctx, v)
	}
	done := make(chan error, 1)
	go func() {
		_, err := m.pool.acquire(context.Background(), v)
		done <- err
	}()
	<-entered
	m.pool.drop(id)
	close(release)
	if err := <-done; !errors.Is(err, ErrAccessChanged) {
		t.Fatalf("dial that raced an access change was used: %v", err)
	}
	if m.pool.size() != 0 {
		t.Fatal("racing dial was cached")
	}
	eventually(t, "discarded connection to close", func() bool { return s.live.Load() == 0 })
}

func TestPoolDoesNotReuseSessionForChangedIdentity(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	v, err := m.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := m.pool.acquire(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	other, _ := signer(t)
	v.Fingerprint = ssh.FingerprintSHA256(other.PublicKey())
	v.hostKey = base64.StdEncoding.EncodeToString(other.PublicKey().Marshal())
	if _, err = m.pool.acquire(context.Background(), v); !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("session pinned to another identity was reused or identity not enforced: %v", err)
	}
	if m.pool.size() != 0 {
		t.Fatal("stale-identity session left pooled")
	}
}

func TestRemotePathChecksStillRefuseSymlinksAndEscapes(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	ctx := context.Background()
	s.admin(t, func(fs *sftp.Client) {
		if err := fs.Mkdir("/data/dir"); err != nil {
			t.Fatal(err)
		}
		if err := fs.Mkdir("/outside"); err != nil {
			t.Fatal(err)
		}
		if err := fs.Symlink("/outside", "/data/dirlink"); err != nil {
			t.Fatal(err)
		}
		if err := fs.Symlink("/outside/secret", "/data/filelink"); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name, path string
		want       error
	}{
		{"root", "/data", nil},
		{"directory", "/data/dir", nil},
		{"outside root", "/outside", files.ErrRootNotApproved},
		{"sibling prefix", "/data2", files.ErrRootNotApproved},
		{"symlinked directory", "/data/dirlink", files.ErrRootNotApproved},
		{"through symlinked directory", "/data/dirlink/secret", files.ErrRootNotApproved},
		{"symlinked file", "/data/filelink", files.ErrRootNotApproved},
		{"missing", "/data/missing", files.ErrNotFound},
		{"missing parent", "/data/missing/child", files.ErrNotFound},
	} {
		_, err := m.StatFile(ctx, "user", FileRequest{MachineID: id, Path: tc.path})
		if tc.want == nil {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
			continue
		}
		if tc.want == files.ErrNotFound {
			if !os.IsNotExist(err) {
				t.Errorf("%s: want not found, got %v", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
	// Creating a new file below an approved directory still works, and creating
	// through a symlinked directory is still refused.
	if _, err := m.WriteFile(ctx, "user", FileWriteRequest{MachineID: id, Path: "/data/dir/new.txt", Data: []byte("x"), Options: files.WriteOptions{Create: true}}); err != nil {
		t.Fatalf("create in approved directory: %v", err)
	}
	if _, err := m.WriteFile(ctx, "user", FileWriteRequest{MachineID: id, Path: "/data/dirlink/new.txt", Data: []byte("x"), Options: files.WriteOptions{Create: true}}); !errors.Is(err, files.ErrRootNotApproved) {
		t.Fatalf("create through symlinked directory: %v", err)
	}
}

func TestRemoteEtagConflictsOnSizeChange(t *testing.T) {
	s, key := newSFTPServer(t)
	m, _ := testManager(t)
	id := pairedRemote(t, m, s, key)
	ctx := context.Background()
	first, err := m.WriteFile(ctx, "user", FileWriteRequest{MachineID: id, Path: "/data/n.txt", Data: []byte("one"), Options: files.WriteOptions{Create: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.WriteFile(ctx, "user", FileWriteRequest{MachineID: id, Path: "/data/n.txt", Data: []byte("two"), Options: files.WriteOptions{}}); !errors.Is(err, files.ErrNeedsEtag) {
		t.Fatalf("write without etag: %v", err)
	}
	second, err := m.WriteFile(ctx, "user", FileWriteRequest{MachineID: id, Path: "/data/n.txt", Data: []byte("changed!"), Options: files.WriteOptions{IfMatch: first.Etag}})
	if err != nil {
		t.Fatalf("write with current etag: %v", err)
	}
	if second.Etag == first.Etag {
		t.Fatal("etag did not change with size")
	}
	var conflict *files.ConflictError
	if _, err = m.WriteFile(ctx, "user", FileWriteRequest{MachineID: id, Path: "/data/n.txt", Data: []byte("stale"), Options: files.WriteOptions{IfMatch: first.Etag}}); !errors.As(err, &conflict) || conflict.Current.Etag != second.Etag {
		t.Fatalf("stale etag accepted: %v", err)
	}
	read, err := m.ReadFile(ctx, "user", FileReadRequest{MachineID: id, Path: "/data/n.txt"})
	if err != nil || string(read.Data) != "changed!" || read.Info.Etag != second.Etag {
		t.Fatalf("read: %+v %v", read, err)
	}
	// Directories are not readable as files.
	if _, err = m.ReadFile(ctx, "user", FileReadRequest{MachineID: id, Path: "/data"}); !errors.Is(err, files.ErrIsDir) {
		t.Fatalf("reading a directory: %v", err)
	}
}

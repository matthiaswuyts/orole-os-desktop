package machines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rayorole/orole-os-desktop/internal/files"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

type approvalGate struct {
	action             string
	ignoreCancellation bool
	entered            chan policy.Request
	release            chan struct{}
	canceled           chan struct{}
	once               sync.Once
}

func newApprovalGate(t *testing.T, action string, ignoreCancellation bool) *approvalGate {
	t.Helper()
	gate := &approvalGate{action: action, ignoreCancellation: ignoreCancellation, entered: make(chan policy.Request, 1), release: make(chan struct{}), canceled: make(chan struct{})}
	t.Cleanup(gate.approve)
	return gate
}

func (g *approvalGate) approve() { g.once.Do(func() { close(g.release) }) }

func (g *approvalGate) Check(ctx context.Context, req policy.Request) (policy.Decision, error) {
	if req.Action != g.action {
		return policy.Allow, nil
	}
	g.entered <- req
	select {
	case <-g.release:
		return policy.Allow, nil
	case <-ctx.Done():
		close(g.canceled)
		if g.ignoreCancellation {
			<-g.release
			return policy.Allow, nil
		}
		return policy.Deny, ctx.Err()
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("machine operation did not reach its synchronization boundary")
		var zero T
		return zero
	}
}

func helperCommand(t *testing.T, marker string) Command {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Command{ID: "local", OperationID: uuid.NewString(), Program: exe, Args: []string{"-test.run=^TestNativeCommandHelper$", "--", "--orole-machine-test-helper", "write-marker", marker}}
}

func TestPendingMachineOperationsRespectAccessRevocation(t *testing.T) {
	for _, action := range []string{"execute", "files.write", "files.read"} {
		for _, ignore := range []bool{false, true} {
			name := action + "/cancellable"
			if ignore {
				name = action + "/late-allow"
			}
			t.Run(name, func(t *testing.T) {
				m, _ := testManager(t)
				ctx := context.Background()
				root := t.TempDir()
				file := filepath.Join(root, "note.txt")
				marker := filepath.Join(root, "command-ran")
				if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{root}, AllowCommands: true, AllowWrite: true}); err != nil {
					t.Fatal(err)
				}
				before, err := m.StatFile(ctx, "user", FileRequest{"local", file})
				if err != nil {
					t.Fatal(err)
				}
				command := helperCommand(t, marker)
				gate := newApprovalGate(t, "machines."+action, ignore)
				m.policy = gate
				done := make(chan error, 1)
				go func() {
					var err error
					switch action {
					case "execute":
						_, err = m.Execute(ctx, "user", command)
					case "files.write":
						_, err = m.WriteFile(ctx, "user", FileWriteRequest{MachineID: "local", Path: file, Data: []byte("changed"), Options: files.WriteOptions{IfMatch: before.Etag}})
					case "files.read":
						_, err = m.ReadFile(ctx, "user", FileReadRequest{MachineID: "local", Path: file})
					}
					done <- err
				}()
				receive(t, gate.entered)
				if _, err := m.SetAccess(ctx, "user", Access{ID: "local"}); err != nil {
					t.Fatal(err)
				}
				receive(t, gate.canceled)
				gate.approve()
				if err := receive(t, done); !errors.Is(err, ErrAccessChanged) {
					t.Fatalf("revoked operation: %v", err)
				}
				data, err := os.ReadFile(file)
				if err != nil || string(data) != "original" {
					t.Fatalf("revoked write changed file: %q, %v", data, err)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("revoked command executed: %v", err)
				}
			})
		}
	}
}

func TestUnchangedMachineApprovalDispatchesExactOperation(t *testing.T) {
	for _, action := range []string{"execute", "files.write"} {
		t.Run(action, func(t *testing.T) {
			m, _ := testManager(t)
			ctx := context.Background()
			root := t.TempDir()
			file := filepath.Join(root, "created.txt")
			unapproved := filepath.Join(root, "unapproved.txt")
			if _, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{root}, AllowCommands: true, AllowWrite: true}); err != nil {
				t.Fatal(err)
			}
			command := helperCommand(t, file)
			gate := newApprovalGate(t, "machines."+action, false)
			m.policy = gate
			done := make(chan error, 1)
			go func() {
				var err error
				if action == "execute" {
					_, err = m.Execute(ctx, "user", command)
				} else {
					_, err = m.WriteFile(ctx, "user", FileWriteRequest{MachineID: "local", Path: file, Data: []byte("executed"), Options: files.WriteOptions{Create: true}})
				}
				done <- err
			}()
			req := receive(t, gate.entered)
			if len(req.Detail) > maxApprovalDetail {
				t.Fatalf("approval would be truncated: %d", len(req.Detail))
			}
			if action == "execute" {
				var detail struct {
					Target  approvalIdentity `json:"target"`
					Program string           `json:"program"`
					Args    []string         `json:"args"`
				}
				if err := json.Unmarshal([]byte(req.Detail), &detail); err != nil {
					t.Fatal(err)
				}
				canonical, err := filepath.EvalSymlinks(command.Program)
				if err != nil {
					t.Fatal(err)
				}
				if detail.Program != canonical || !slices.Equal(detail.Args, command.Args) || detail.Target.ID != "local" || detail.Target.Username == "" || detail.Target.Transport != "native" {
					t.Fatalf("incomplete execution approval: %s", req.Detail)
				}
				// Mutating caller-owned argv after consent was requested must not
				// replace the snapshot used for the eventual executable dispatch.
				command.Args[len(command.Args)-1] = unapproved
			}
			gate.approve()
			if err := receive(t, done); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "executed" {
				t.Fatalf("approved operation missing: %q, %v", data, err)
			}
			if _, err := os.Stat(unapproved); !os.IsNotExist(err) {
				t.Fatalf("caller replaced approved argv: %v", err)
			}
		})
	}
}

func TestMachineAccessApprovalShowsExactTransition(t *testing.T) {
	m, p := testManager(t)
	ctx := context.Background()
	old, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{t.TempDir()}, AllowWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{t.TempDir()}, AllowCommands: true})
	if err != nil {
		t.Fatal(err)
	}
	req := p.seen[len(p.seen)-1]
	var detail struct {
		Target approvalIdentity `json:"target"`
		Old    approvalGrants   `json:"old"`
		New    approvalGrants   `json:"new"`
	}
	if err := json.Unmarshal([]byte(req.Detail), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Target.ID != old.ID || detail.Target.Name != old.Name || detail.Target.Username == "" || !slices.Equal(detail.Old.Roots, old.Roots) || !slices.Equal(detail.New.Roots, updated.Roots) || !detail.Old.AllowWrite || detail.Old.AllowCommands || detail.New.AllowWrite || !detail.New.AllowCommands {
		t.Fatalf("incorrect access transition: %s", req.Detail)
	}
	if updated.accessRevision != old.accessRevision+1 {
		t.Fatalf("access revision did not advance: %d -> %d", old.accessRevision, updated.accessRevision)
	}
}

func TestMachineApprovalRejectsUnrepresentableDetails(t *testing.T) {
	m, p := testManager(t)
	ctx := context.Background()
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local", AllowCommands: true}); err != nil {
		t.Fatal(err)
	}
	command := helperCommand(t, filepath.Join(t.TempDir(), "unused"))
	for _, args := range [][]string{{"bad\x00arg"}, {"hidden\u202esuffix"}, {string([]byte{0xff})}, {strings.Repeat("x", maxApprovalDetail)}, {strings.Repeat("\n", maxApprovalDetail/2)}} {
		command.Args = args
		before := len(p.seen)
		if _, err := m.Execute(ctx, "user", command); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted unrepresentable argv: %v", err)
		}
		if len(p.seen) != before {
			t.Fatal("invalid argv reached approval")
		}
	}
	command.Args = nil
	for _, program := range []string{"bad\x00program", "hidden\u202eprogram", string([]byte{0xff}), strings.Repeat("x", maxApprovalDetail+1)} {
		command.Program = program
		before := len(p.seen)
		if _, err := m.Execute(ctx, "user", command); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted unrepresentable executable: %v", err)
		}
		if len(p.seen) != before {
			t.Fatal("invalid executable reached approval")
		}
	}
	root := t.TempDir()
	for _, roots := range [][]string{{root + "\u202e"}, {string([]byte{0xff})}, {strings.Repeat("x", maxApprovalDetail+1)}, make([]string, 33)} {
		before := len(p.seen)
		if _, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: roots}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted unrepresentable grants: %v", err)
		}
		if len(p.seen) != before {
			t.Fatal("invalid grants reached approval")
		}
	}
	repeatedRoots := make([]string, 32)
	for i := range repeatedRoots {
		repeatedRoots[i] = root
	}
	before := len(p.seen)
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: repeatedRoots}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized valid roots reached approval: %v", err)
	}
	if len(p.seen) != before {
		t.Fatal("oversized roots reached approval")
	}
	v, err := m.Get(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	roots := make([]string, 32)
	for i := range roots {
		roots[i] = strings.Repeat("/x", 30)
	}
	if _, err := accessDetail(v, roots, true, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized serialized grants accepted: %v", err)
	}
	for _, tc := range []struct{ backend, program string }{{"posix", "printf"}, {"posix", "/usr/bin/../bin/printf"}, {"powershell", "cmd.exe"}, {"powershell", `C:\Windows\..\cmd.exe`}} {
		if _, _, err := prepareCommand(Machine{Backend: tc.backend}, Command{Program: tc.program}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ambiguous remote executable accepted: %q: %v", tc.program, err)
		}
	}
}

func TestRemovingMachineInvalidatesQueuedOperations(t *testing.T) {
	for _, action := range []string{"execute", "files.write"} {
		t.Run(action, func(t *testing.T) {
			m, _ := testManager(t)
			ctx := context.Background()
			id := uuid.NewString()
			_, err := m.db.Exec(`INSERT INTO machines(id,name,kind,os,transport,backend,address,username,fingerprint,host_key,roots,allow_commands,allow_write,created_at) VALUES(?,'remote','remote','linux','ssh','posix','127.0.0.1:1','account','SHA256:test','test','["/tmp"]',1,1,0)`, id)
			if err != nil {
				t.Fatal(err)
			}
			gate := newApprovalGate(t, "machines."+action, true)
			m.policy = gate
			done := make(chan error, 1)
			go func() {
				var err error
				if action == "execute" {
					_, err = m.Execute(ctx, "user", Command{ID: id, OperationID: uuid.NewString(), Program: "/usr/bin/printf", Args: []string{"unchanged"}})
				} else {
					_, err = m.WriteFile(ctx, "user", FileWriteRequest{MachineID: id, Path: "/tmp/test", Data: []byte("not sent"), Options: files.WriteOptions{Create: true}})
				}
				done <- err
			}()
			receive(t, gate.entered)
			if err := m.Remove(ctx, "user", id); err != nil {
				t.Fatal(err)
			}
			receive(t, gate.canceled)
			gate.approve()
			if err := receive(t, done); !errors.Is(err, ErrAccessChanged) {
				t.Fatalf("removed identity dispatched: %v", err)
			}
			if _, err := m.Get(ctx, id); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("machine was not removed: %v", err)
			}
		})
	}
}

func TestQueuedWriteIsCanceledBeforeSerializationSlot(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	root := t.TempDir()
	target := filepath.Join(root, "not-created")
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{root}, AllowWrite: true}); err != nil {
		t.Fatal(err)
	}
	// Model another in-progress write without letting this request acquire the
	// serialization slot. Revocation must not wait for a queued writer.
	m.writes <- struct{}{}
	defer func() { <-m.writes }()
	gate := newApprovalGate(t, "machines.files.write", false)
	m.policy = gate
	done := make(chan error, 1)
	go func() {
		_, err := m.WriteFile(ctx, "user", FileWriteRequest{MachineID: "local", Path: target, Data: []byte("not committed"), Options: files.WriteOptions{Create: true}})
		done <- err
	}()
	receive(t, gate.entered)
	gate.approve()
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, done); !errors.Is(err, ErrAccessChanged) {
		t.Fatalf("queued write survived revocation: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("queued write committed: %v", err)
	}
}

func TestDispatchRevalidatesDurableAccessSnapshot(t *testing.T) {
	for _, action := range []string{"execute", "files.write"} {
		t.Run(action, func(t *testing.T) {
			m, _ := testManager(t)
			ctx := context.Background()
			root := t.TempDir()
			target := filepath.Join(root, "not-created")
			if _, err := m.SetAccess(ctx, "user", Access{ID: "local", Roots: []string{root}, AllowCommands: true, AllowWrite: true}); err != nil {
				t.Fatal(err)
			}
			command := helperCommand(t, target)
			gate := newApprovalGate(t, "machines."+action, false)
			m.policy = gate
			done := make(chan error, 1)
			go func() {
				var err error
				if action == "execute" {
					_, err = m.Execute(ctx, "user", command)
				} else {
					_, err = m.WriteFile(ctx, "user", FileWriteRequest{MachineID: "local", Path: target, Data: []byte("not committed"), Options: files.WriteOptions{Create: true}})
				}
				done <- err
			}()
			receive(t, gate.entered)
			// Deliberately bypass proactive cancellation. The final dispatch
			// must still reject an old revision, even if grants are restored.
			if _, err := m.db.Exec(`UPDATE machines SET access_revision=access_revision+2 WHERE id='local'`); err != nil {
				t.Fatal(err)
			}
			gate.approve()
			if err := receive(t, done); !errors.Is(err, ErrAccessChanged) {
				t.Fatalf("stale durable snapshot dispatched: %v", err)
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("stale snapshot committed: %v", err)
			}
		})
	}
}

func TestPendingAccessChangeCannotOverwriteLaterGrant(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	if _, err := m.Get(ctx, "local"); err != nil {
		t.Fatal(err)
	}
	gate := newApprovalGate(t, "machines.access", true)
	m.policy = gate
	done := make(chan error, 1)
	go func() {
		_, err := m.SetAccess(ctx, "user", Access{ID: "local", AllowCommands: true})
		done <- err
	}()
	receive(t, gate.entered)
	// Simulate a separately committed transition without satisfying the old
	// prompt. The revision check binds old/new grants, not only the machine ID.
	if _, err := m.db.Exec(`UPDATE machines SET allow_write=1,access_revision=access_revision+1 WHERE id='local'`); err != nil {
		t.Fatal(err)
	}
	gate.approve()
	if err := receive(t, done); !errors.Is(err, ErrAccessChanged) {
		t.Fatalf("stale grant approval overwrote newer grant: %v", err)
	}
	current, err := m.Get(ctx, "local")
	if err != nil || current.AllowCommands || !current.AllowWrite {
		t.Fatalf("newer grant lost: %+v, %v", current, err)
	}
}

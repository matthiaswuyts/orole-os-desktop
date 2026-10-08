package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/agents"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/drive"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// This test-only adapter receives launches from the real agent manager. Drive
// reads use a real SDK Streamable HTTP server, not source-string assertions.
type shareAgent struct{ sent chan string }

func (a shareAgent) Kind() string         { return "claude" }
func (a shareAgent) Name() string         { return "share fixture" }
func (a shareAgent) Modes() []agents.Mode { return []agents.Mode{agents.ModeStructured} }
func (a shareAgent) CanResume() bool      { return true }
func (a shareAgent) Detect(context.Context) agents.Detection {
	return agents.Detection{Kind: a.Kind(), Installed: true}
}
func (a shareAgent) Start(_ context.Context, _ *agents.Host, s agents.LaunchSpec) (agents.Process, error) {
	p := &shareProcess{events: make(chan agents.Event), sent: a.sent}
	if s.Prompt != "" {
		a.sent <- s.Prompt
	}
	return p, nil
}

type shareProcess struct {
	events chan agents.Event
	once   sync.Once
	sent   chan string
}

func (*shareProcess) Pid() int                           { return 1 }
func (*shareProcess) TerminalID() string                 { return "" }
func (p *shareProcess) Send(s string) error              { p.sent <- s; return nil }
func (*shareProcess) Respond(string, bool, string) error { return nil }
func (p *shareProcess) Stop() error                      { p.once.Do(func() { close(p.events) }); return nil }
func (p *shareProcess) Events() <-chan agents.Event      { return p.events }

type shareFixture struct {
	h       driveHandoff
	mcp     *mcp.Manager
	db      *database.DB
	request drive.ShareRequest
	sent    chan string
	text    atomic.Value
}

func newShareFixture(t *testing.T) *shareFixture {
	t.Helper()
	f := &shareFixture{sent: make(chan string, 8)}
	f.text.Store("private fixture text")
	db, err := database.Open(filepath.Join(t.TempDir(), "share.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	f.db = db
	t.Cleanup(func() { _ = db.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	gate := policy.New(db.DB, nil, log)
	server := mcptest.New(t, mcptest.Options{OpenMCP: true, Setup: func(s *sdk.Server) {
		mcptest.AddJSONTool(s, "read_file", "bounded fixture text", mcptest.ReadOnly(), map[string]any{"fileId": map[string]any{"type": "string"}}, []string{"fileId"}, func(args map[string]any) (any, error) {
			return map[string]any{"fileId": args["fileId"], "mimeType": "text/plain", "text": f.text.Load().(string)}, nil
		})
	}})
	manager := mcp.New(mcp.Options{DB: db.DB, Creds: credentials.NewMemory(), Policy: gate, ConnectTimeout: 3 * time.Second, CallTimeout: 3 * time.Second})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	f.mcp = manager
	state, err := manager.Add(context.Background(), mcp.AddParams{Name: "scoped fixture", URL: server.MCPURL, Adapter: "drive", AuthKind: mcp.AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for {
		current, err := manager.Get(ctx, state.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == mcp.StatusConnected {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("SDK fixture did not connect")
		case <-time.After(10 * time.Millisecond):
		}
	}
	am := agents.NewManager(agents.Config{Store: agents.NewStore(db), Log: log, Host: &agents.Host{DataDir: t.TempDir()}, Adapters: []agents.Adapter{shareAgent{sent: f.sent}}, Authorize: func(ctx context.Context, action, resource, detail string) error {
		return gate.Require(ctx, policy.Request{Actor: "user", Action: action, Resource: resource, Detail: detail, Effect: policy.EffectDestructive})
	}})
	t.Cleanup(am.Shutdown)
	session := agents.Session{ID: "recipient", Kind: "claude", Machine: "local", Dir: t.TempDir(), Mode: agents.ModeStructured, Status: agents.StatusDone, AgentSessionID: "continuation-id", StartedAt: time.Now(), UpdatedAt: time.Now()}
	if err := am.Store().SaveSession(session, ""); err != nil {
		t.Fatal(err)
	}
	f.h = driveHandoff{adapter: drive.New(manager, gate), agents: am, policy: gate}
	preview, err := f.h.adapter.Preview(ctx, "selected-file")
	if err != nil {
		t.Fatal(err)
	}
	f.request = drive.ShareRequest{ServerID: preview.ServerID, FileID: preview.FileID, SnapshotHash: preview.SnapshotHash, SessionID: session.ID, RecipientRevision: agents.PromptRevision(session), Task: "Summarize only"}
	return f
}

func (f *shareFixture) pending(t *testing.T, action string, done <-chan error) policy.Pending {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, p := range f.h.policy.Pending() {
			if p.Action != action {
				t.Fatalf("approval %q; want %q", p.Action, action)
			}
			if p.CanRemember {
				t.Fatal("sharing approval can be remembered")
			}
			if strings.Contains(p.Detail, "private fixture text") || strings.Contains(p.Resource, "selected-file") {
				t.Fatal("content or file identity leaked into audit metadata")
			}
			return p
		}
		select {
		case err := <-done:
			t.Fatalf("handoff ended before %s approval: %v", action, err)
		case <-deadline:
			t.Fatal("missing approval")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
func (f *shareFixture) begin(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { _, err := f.h.share(ctx, f.request); done <- err }()
	return done
}
func (f *shareFixture) resolve(t *testing.T, p policy.Pending, allow bool) {
	t.Helper()
	if err := f.h.policy.Resolve(context.Background(), p.ID, policy.Resolution{Allow: allow}); err != nil {
		t.Fatal(err)
	}
}
func (f *shareFixture) noDelivery(t *testing.T) {
	t.Helper()
	select {
	case <-f.sent:
		t.Fatal("content delivered without valid scoped approval")
	default:
	}
	events, err := f.h.agents.Events(f.request.SessionID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind == agents.EvPrompt {
			t.Fatal("denied content persisted to agent transcript")
		}
	}
}

func TestDriveScopedHandoffSDK(t *testing.T) {
	for _, stage := range []string{"share denied", "agent denied", "permission revoked", "destination changed", "expired", "success"} {
		t.Run(stage, func(t *testing.T) {
			f := newShareFixture(t)
			// Exercise revocation while an explicitly configured prompt approval
			// is pending; direct user task submission no longer asks by default.
			if _, err := f.h.policy.AddRule(context.Background(), policy.Rule{
				Actor: "user", Action: "agent.prompt", Resource: f.request.SessionID, Decision: policy.Ask,
			}); err != nil {
				t.Fatal(err)
			}
			timeout := 8 * time.Second
			if stage == "expired" {
				timeout = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			done := f.begin(ctx)
			sharing := f.pending(t, "drive.share", done)
			if stage == "expired" {
				if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expiry: %v", err)
				}
				if err := f.h.policy.Resolve(context.Background(), sharing.ID, policy.Resolution{Allow: true}); !errors.Is(err, policy.ErrNotFound) {
					t.Fatalf("expired approval replay: %v", err)
				}
				f.noDelivery(t)
				return
			}
			f.resolve(t, sharing, stage != "share denied")
			if stage == "share denied" {
				if err := <-done; !errors.Is(err, policy.ErrDenied) {
					t.Fatal(err)
				}
				f.noDelivery(t)
				return
			}
			prompting := f.pending(t, "agent.prompt", done)
			if stage == "permission revoked" {
				if err := f.mcp.SetToolPermission(ctx, f.request.ServerID, "read_file", "deny"); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "destination changed" {
				s, err := f.h.agents.Get(f.request.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				s.Model = "different-provider-model"
				if err := f.h.agents.Store().SaveSession(s, ""); err != nil {
					t.Fatal(err)
				}
			}
			f.resolve(t, prompting, stage != "agent denied")
			err := <-done
			if stage != "success" {
				if err == nil {
					t.Fatal("unsafe handoff succeeded")
				}
				f.noDelivery(t)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case prompt := <-f.sent:
				start := strings.IndexByte(prompt, '{')
				var payload struct {
					Task    string `json:"userTask"`
					Content string `json:"untrustedDriveText"`
				}
				if start < 0 || json.Unmarshal([]byte(prompt[start:]), &payload) != nil || payload.Content != f.text.Load() || payload.Task != f.request.Task {
					t.Fatal("wrong snapshot delivered")
				}
			default:
				t.Fatal("manager did not deliver snapshot")
			}
			if _, err := f.h.share(ctx, f.request); err == nil {
				t.Fatal("stale destination replay delivered twice")
			}
			select {
			case <-f.sent:
				t.Fatal("duplicate delivery")
			default:
			}
			var leaked int
			if err := f.db.QueryRow(`SELECT count(*) FROM audit_events WHERE detail LIKE '%private fixture text%' OR target LIKE '%selected-file%'`).Scan(&leaked); err != nil || leaked != 0 {
				t.Fatalf("audit disclosure: %d %v", leaked, err)
			}
		})
	}
}

func TestDriveScopedHandoffRejectsChangedAndUnsupportedContent(t *testing.T) {
	for _, text := range []string{"changed text", "unsafe\u202e", strings.Repeat("x", drive.MaxShareBytes+1)} {
		f := newShareFixture(t)
		f.text.Store(text)
		if _, err := f.h.share(context.Background(), f.request); err == nil {
			t.Fatal("changed/unsupported snapshot shared")
		}
		if len(f.h.policy.Pending()) != 0 {
			t.Fatal("unsupported content queued for sharing")
		}
		f.noDelivery(t)
	}
}

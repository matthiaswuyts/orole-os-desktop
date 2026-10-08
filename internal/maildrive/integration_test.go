package maildrive_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/drive"
	"github.com/rayorole/orole-os-desktop/internal/mail"
	"github.com/rayorole/orole-os-desktop/internal/maildrive"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type approvalEvents struct{ changed chan struct{} }

func (p *approvalEvents) Publish(name string, _ any) {
	if name == "policy.pending" {
		select {
		case p.changed <- struct{}{}:
		default:
		}
	}
}

type protocolHarness struct {
	manager *mcp.Manager
	policy  *policy.Engine
	events  *approvalEvents
	server  *mcptest.Server
	id      string
}

func protocol(t *testing.T, adapter string, setup func(*sdk.Server)) *protocolHarness {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	events := &approvalEvents{changed: make(chan struct{}, 8)}
	gate := policy.New(db.DB, events, nil)
	manager := mcp.New(mcp.Options{DB: db.DB, Creds: credentials.NewMemory(), Policy: gate, ConnectTimeout: 3 * time.Second, CallTimeout: 3 * time.Second, BackoffBase: 10 * time.Millisecond})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	server := mcptest.New(t, mcptest.Options{DCR: true, Setup: setup})
	state, err := manager.Add(context.Background(), mcp.AddParams{Name: "protocol adapter", URL: server.MCPURL, Adapter: adapter, AuthKind: mcp.AuthOAuth})
	if err != nil {
		t.Fatal(err)
	}
	h := &protocolHarness{manager: manager, policy: gate, events: events, server: server, id: state.ID}
	h.wait(t, mcp.StatusAuthRequired)
	start, err := manager.StartAuth(context.Background(), state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := server.Browser(context.Background(), start.URL); err != nil || code != 200 {
		t.Fatalf("browser: %d %v", code, err)
	}
	h.wait(t, mcp.StatusConnected)
	return h
}
func (h *protocolHarness) wait(t *testing.T, want mcp.Status) {
	t.Helper()
	deadline := time.After(8 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := h.manager.Get(context.Background(), h.id)
		if err != nil {
			t.Fatal(err)
		}
		if state.Status == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("want %s; got %+v", want, state)
		case <-ticker.C:
		}
	}
}
func (h *protocolHarness) approval(t *testing.T, action string, allow bool, fn func() error) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- fn() }()
	deadline := time.After(5 * time.Second)
	for {
		for _, pending := range h.policy.Pending() {
			if pending.Action != action {
				t.Fatalf("approval action %s; want %s", pending.Action, action)
			}
			if action == "mail.trash" || action == "drive.trash" {
				if pending.Effect != policy.EffectDestructive || pending.CanRemember {
					t.Fatalf("unsafe destructive approval: %+v", pending)
				}
			}
			if err := h.policy.Resolve(context.Background(), pending.ID, policy.Resolution{Allow: allow}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				return err
			case <-deadline:
				t.Fatal("approved invocation stalled")
				return nil
			}
		}
		select {
		case err := <-result:
			t.Fatalf("invocation bypassed approval: %v", err)
			return err
		case <-h.events.changed:
		case <-deadline:
			t.Fatal("no policy approval")
			return nil
		}
	}
}
func str() map[string]any { return map[string]any{"type": "string"} }
func add(s *sdk.Server, name string, props map[string]any, required []string, out any) {
	mcptest.AddJSONTool(s, name, "test contract", mcptest.ReadOnly(), props, required, func(map[string]any) (any, error) { return out, nil })
}

func TestMailOAuthProtocolAndPolicy(t *testing.T) {
	var writes atomic.Int32
	var mode atomic.Int32
	message := map[string]any{"id": "m1", "threadId": "t1", "subject": "Hello\u202e", "from": "sender@example.test", "to": []string{"me@example.test"}, "body": "<p>Hello <b>world</b></p><script>bad()</script>", "unread": true}
	h := protocol(t, "mail", func(s *sdk.Server) {
		add(s, "list_folders", map[string]any{}, nil, map[string]any{"folders": []any{map[string]any{"id": "inbox", "name": "Inbox", "unreadCount": 1}}})
		add(s, "list_messages", map[string]any{"folderId": str(), "pageToken": str(), "limit": map[string]any{"type": "integer"}}, nil, map[string]any{"messages": []any{message}, "nextPageToken": "p2"})
		add(s, "search_messages", map[string]any{"query": str()}, []string{"query"}, map[string]any{"messages": []any{message}})
		add(s, "get_thread", map[string]any{"threadId": str()}, []string{"threadId"}, map[string]any{"id": "t1", "messages": []any{message}})
		mcptest.AddJSONTool(s, "send_email", "send", mcptest.ReadOnly(), map[string]any{"to": map[string]any{"type": "array", "items": str()}, "subject": str(), "body": str()}, []string{"to", "subject", "body"}, func(args map[string]any) (any, error) {
			writes.Add(1)
			if args["body"] != "Hello" {
				return nil, errors.New("wrong body argument")
			}
			switch mode.Load() {
			case 1:
				return map[string]any{"ok": true}, nil
			case 2:
				return nil, errors.New("server refused send")
			}
			return map[string]any{"id": "sent1", "sent": true}, nil
		})
		add(s, "reply_email", map[string]any{"threadId": str(), "body": str()}, []string{"threadId", "body"}, map[string]any{"id": "reply1", "status": "sent"})
		mcptest.AddJSONTool(s, "trash_message", "trash", mcptest.ReadOnly(), map[string]any{"id": str()}, []string{"id"}, func(args map[string]any) (any, error) {
			writes.Add(1)
			return map[string]any{"id": args["id"], "trashed": true}, nil
		})
	})
	a := mail.New(h.manager, h.policy)
	ctx := context.Background()
	status, err := a.Status(ctx)
	if err != nil || !status.Connected || status.Capabilities != (mail.Capabilities{CanFolders: true, CanList: true, CanSearch: true, CanThread: true, CanSend: true, CanReply: true, CanTrash: true}) {
		t.Fatalf("status: %+v %v", status, err)
	}
	folders, err := a.Folders(ctx)
	if err != nil || len(folders) != 1 || folders[0].Name != "Inbox" {
		t.Fatalf("folders: %+v %v", folders, err)
	}
	page, err := a.List(ctx, mail.Query{FolderID: "inbox", Limit: 20})
	if err != nil || len(page.Messages) != 1 || page.NextPageToken != "p2" {
		t.Fatalf("list: %+v %v", page, err)
	}
	if page.Messages[0].Subject != "Hello" || page.Messages[0].Body != "\nHello world\n" {
		t.Fatalf("unsafe mail: %+v", page.Messages[0])
	}
	if _, err := a.Search(ctx, mail.Query{Query: "hello"}); err != nil {
		t.Fatal(err)
	}
	thread, err := a.Thread(ctx, "t1")
	if err != nil || len(thread.Messages) != 1 {
		t.Fatalf("thread: %+v %v", thread, err)
	}
	send := func() error {
		out, err := a.Send(ctx, mail.Compose{To: []string{"a@example.test"}, Subject: "Hello", Body: "Hello"})
		if err == nil && (!out.Confirmed || out.ID != "sent1") {
			return errors.New("missing sent confirmation")
		}
		return err
	}
	if err := h.manager.SetToolPermission(ctx, h.id, "send_email", "deny"); err != nil {
		t.Fatal(err)
	}
	if err := send(); !errors.Is(err, policy.ErrDenied) || writes.Load() != 0 {
		t.Fatalf("typed adapter bypassed per-tool deny: %v", err)
	}
	if err := h.manager.SetToolPermission(ctx, h.id, "send_email", ""); err != nil {
		t.Fatal(err)
	}
	if err := h.approval(t, "mail.send", false, send); !errors.Is(err, policy.ErrDenied) || writes.Load() != 0 {
		t.Fatalf("denied send invoked server: %v %d", err, writes.Load())
	}
	if err := h.approval(t, "mail.send", true, send); err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	if err := h.approval(t, "mail.send", true, send); !errors.Is(err, mail.ErrInvalidResult) {
		t.Fatalf("generic ok accepted: %v", err)
	}
	mode.Store(2)
	if err := h.approval(t, "mail.send", true, send); !errors.Is(err, maildrive.ErrTool) {
		t.Fatalf("tool error swallowed: %v", err)
	}
	if err := h.approval(t, "mail.reply", true, func() error { _, err := a.Reply(ctx, mail.Reply{ThreadID: "t1", Body: "Hello"}); return err }); err != nil {
		t.Fatal(err)
	}
	if err := h.approval(t, "mail.trash", true, func() error {
		out, err := a.Trash(ctx, "m1")
		if err == nil && !out.Confirmed {
			return errors.New("no trash confirmation")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDriveOAuthProtocolAndPolicy(t *testing.T) {
	var calls atomic.Int32
	var bad atomic.Bool
	file := map[string]any{"id": "f1", "name": "note\u202e.md", "mimeType": "text/markdown", "parentId": "root", "size": "12"}
	h := protocol(t, "drive", func(s *sdk.Server) {
		add(s, "list_files", map[string]any{"parentId": str(), "pageToken": str(), "limit": map[string]any{"type": "integer"}}, nil, map[string]any{"files": []any{file}, "nextPageToken": "next"})
		add(s, "search_files", map[string]any{"query": str()}, []string{"query"}, map[string]any{"files": []any{file}})
		add(s, "get_file", map[string]any{"fileId": str()}, []string{"fileId"}, file)
		add(s, "read_file", map[string]any{"fileId": str()}, []string{"fileId"}, map[string]any{"fileId": "f1", "mimeType": "text/markdown", "text": "# Title\u202e"})
		mcptest.AddJSONTool(s, "trash_file", "trash", mcptest.ReadOnly(), map[string]any{"fileId": str()}, []string{"fileId"}, func(args map[string]any) (any, error) {
			calls.Add(1)
			if bad.Load() {
				return map[string]any{"id": "another", "trashed": true}, nil
			}
			return map[string]any{"id": args["fileId"], "status": "trashed"}, nil
		})
	})
	a := drive.New(h.manager, h.policy)
	ctx := context.Background()
	status, err := a.Status(ctx)
	if err != nil || status.Capabilities != (drive.Capabilities{CanList: true, CanSearch: true, CanStat: true, CanPreview: true, CanTrash: true}) {
		t.Fatalf("status: %+v %v", status, err)
	}
	page, err := a.List(ctx, drive.Query{ParentID: "root", Limit: 20})
	if err != nil || len(page.Files) != 1 || page.Files[0].Name != "note.md" || page.NextPageToken != "next" {
		t.Fatalf("list: %+v %v", page, err)
	}
	if _, err := a.Search(ctx, drive.Query{Query: "note"}); err != nil {
		t.Fatal(err)
	}
	stat, err := a.Stat(ctx, "f1")
	if err != nil || stat.Size != 12 {
		t.Fatalf("stat: %+v %v", stat, err)
	}
	preview, err := a.Preview(ctx, "f1")
	if err != nil || preview.Kind != "markdown" || preview.Text != "# Title" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	trash := func() error { _, err := a.Trash(ctx, "f1"); return err }
	if err := h.approval(t, "drive.trash", false, trash); !errors.Is(err, policy.ErrDenied) || calls.Load() != 0 {
		t.Fatalf("denial: %v calls %d", err, calls.Load())
	}
	if err := h.approval(t, "drive.trash", true, trash); err != nil {
		t.Fatal(err)
	}
	bad.Store(true)
	if err := h.approval(t, "drive.trash", true, trash); !errors.Is(err, drive.ErrInvalidResult) {
		t.Fatalf("wrong identity accepted: %v", err)
	}
}

package maildrive_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/drive"
	"github.com/rayorole/orole-os-desktop/internal/mail"
	"github.com/rayorole/orole-os-desktop/internal/maildrive"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
)

type catalogManager struct {
	configured bool
	state      mcp.Status
	tools      []mcp.ToolInfo
	result     *mcp.CallResult
	callErr    error
	calls      int
	args       map[string]any
}

func (m *catalogManager) ByAdapter(context.Context, string) (mcp.ServerState, bool) {
	return mcp.ServerState{ServerConfig: mcp.ServerConfig{ID: "server", Name: "Example\u202e"}, Status: m.state}, m.configured
}
func (m *catalogManager) Tools(context.Context, string) ([]mcp.ToolInfo, error) { return m.tools, nil }
func (m *catalogManager) CallTool(_ context.Context, _, _ string, args map[string]any, _ mcp.CallOptions) (*mcp.CallResult, error) {
	m.calls++
	m.args = args
	return m.result, m.callErr
}

type gate struct {
	requests []policy.Request
	err      error
}

func (g *gate) Require(_ context.Context, r policy.Request) error {
	g.requests = append(g.requests, r)
	return g.err
}
func tool(name, schema string) mcp.ToolInfo {
	return mcp.ToolInfo{Name: name, InputSchema: json.RawMessage(schema)}
}

func TestUnconfiguredAndDisconnected(t *testing.T) {
	ctx := context.Background()
	for _, configured := range []bool{false, true} {
		manager := &catalogManager{configured: configured, state: mcp.StatusAuthRequired}
		ma, da := mail.New(manager, &gate{}), drive.New(manager, &gate{})
		ms, err := ma.Status(ctx)
		if err != nil || ms.Configured != configured || ms.Connected || ms.Capabilities != (mail.Capabilities{}) {
			t.Fatalf("mail status: %+v %v", ms, err)
		}
		ds, err := da.Status(ctx)
		if err != nil || ds.Configured != configured || ds.Connected || ds.Capabilities != (drive.Capabilities{}) {
			t.Fatalf("drive status: %+v %v", ds, err)
		}
		want := maildrive.ErrNotConfigured
		if configured {
			want = mcp.ErrNotConnected
		}
		if _, err := ma.List(ctx, mail.Query{}); !errors.Is(err, want) {
			t.Fatalf("mail: %v", err)
		}
		if _, err := da.List(ctx, drive.Query{}); !errors.Is(err, want) {
			t.Fatalf("drive: %v", err)
		}
	}
}

func TestCapabilityFlagsRejectUnknownAndUnsupportedSchemas(t *testing.T) {
	cases := []struct {
		name, valid string
		mail        bool
		flag        string
	}{
		{"list_folders", `{"type":"object","properties":{}}`, true, "folders"},
		{"list_messages", `{"type":"object","properties":{}}`, true, "list"},
		{"search_messages", `{"type":"object","properties":{"query":{"type":"string"}}}`, true, "search"},
		{"get_thread", `{"type":"object","properties":{"threadId":{"type":"string"}}}`, true, "thread"},
		{"send_email", `{"type":"object","properties":{"to":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"}}}`, true, "send"},
		{"reply_email", `{"type":"object","properties":{"threadId":{"type":"string"},"body":{"type":"string"}}}`, true, "reply"},
		{"trash_message", `{"type":"object","properties":{"id":{"type":"string"}}}`, true, "trash"},
		{"list_files", `{"type":"object","properties":{}}`, false, "list"},
		{"search_files", `{"type":"object","properties":{"query":{"type":"string"}}}`, false, "search"},
		{"get_file", `{"type":"object","properties":{"fileId":{"type":"string"}}}`, false, "stat"},
		{"read_file", `{"type":"object","properties":{"fileId":{"type":"string"}}}`, false, "preview"},
		{"trash_file", `{"type":"object","properties":{"fileId":{"type":"string"}}}`, false, "trash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, schema := range []string{tc.valid, `{"type":"object","properties":{"account":{"type":"string"}},"required":["account"]}`, `{"type":"object","oneOf":[{}]}`, `{"type":"array"}`, `{`} {
				manager := &catalogManager{configured: true, state: mcp.StatusConnected, tools: []mcp.ToolInfo{tool(tc.name, schema)}}
				flags := map[string]bool{}
				if tc.mail {
					s, err := mail.New(manager, &gate{}).Status(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					c := s.Capabilities
					flags = map[string]bool{"folders": c.CanFolders, "list": c.CanList, "search": c.CanSearch, "thread": c.CanThread, "send": c.CanSend, "reply": c.CanReply, "trash": c.CanTrash}
				} else {
					s, err := drive.New(manager, &gate{}).Status(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					c := s.Capabilities
					flags = map[string]bool{"list": c.CanList, "search": c.CanSearch, "stat": c.CanStat, "preview": c.CanPreview, "trash": c.CanTrash}
				}
				for key, value := range flags {
					if value != (schema == tc.valid && key == tc.flag) {
						t.Fatalf("%s=%v for %s", key, value, schema)
					}
				}
			}
		})
	}
	manager := &catalogManager{configured: true, state: mcp.StatusConnected, tools: []mcp.ToolInfo{tool("delete_everything", `{"type":"object"}`)}}
	if _, err := mail.New(manager, &gate{}).Trash(context.Background(), "m1"); !errors.Is(err, mail.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := drive.New(manager, &gate{}).Trash(context.Background(), "f1"); !errors.Is(err, drive.ErrUnsupported) {
		t.Fatal(err)
	}
	if manager.calls != 0 {
		t.Fatal("unknown tools invoked")
	}
}

func TestSchemaArgumentValidationAndPolicyFailure(t *testing.T) {
	manager := &catalogManager{configured: true, state: mcp.StatusConnected, tools: []mcp.ToolInfo{tool("drive_search", `{"type":"object","properties":{"q":{"type":"string","minLength":3,"maxLength":8}},"required":["q"],"additionalProperties":false}`)}, result: &mcp.CallResult{Structured: json.RawMessage(`{"files":[]}`)}}
	g := &gate{}
	a := drive.New(manager, g)
	ctx := context.Background()
	for _, q := range []drive.Query{{Query: "ab"}, {Query: "abc", ParentID: "folder"}, {Query: "abc", PageToken: "page"}, {Query: "abc", Limit: 2}} {
		if _, err := a.Search(ctx, q); !errors.Is(err, drive.ErrUnsupported) {
			t.Fatalf("unsupported args: %v", err)
		}
	}
	if manager.calls != 0 || len(g.requests) != 0 {
		t.Fatal("invalid schema arguments reached policy or server")
	}
	if _, err := a.Search(ctx, drive.Query{Query: "abc"}); err != nil {
		t.Fatal(err)
	}
	if manager.args["q"] != "abc" {
		t.Fatal("alias not mapped")
	}
	g.err = context.Canceled
	if _, err := a.Search(ctx, drive.Query{Query: "abc"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if manager.calls != 1 {
		t.Fatal("policy cancellation invoked tool")
	}
	if _, err := drive.New(manager, nil).Search(ctx, drive.Query{Query: "abc"}); !errors.Is(err, policy.ErrDenied) {
		t.Fatal(err)
	}
}

func TestResultErrorsAndConfirmation(t *testing.T) {
	manager := &catalogManager{configured: true, state: mcp.StatusConnected, tools: []mcp.ToolInfo{tool("trash_file", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)}}
	g := &gate{}
	a := drive.New(manager, g).ForActor("automation:test")
	ctx := context.Background()
	for _, result := range []*mcp.CallResult{nil, {IsError: true}, {Structured: json.RawMessage(`{"ok":true}`)}, {Structured: json.RawMessage(`{"id":"f1","trashed":false}`)}, {Structured: json.RawMessage(`{"id":"other","trashed":true}`)}, {Content: []mcp.ContentBlock{{Type: "text", Text: `{"id":"f1","trashed":true}`, Truncated: true}}}, {Content: []mcp.ContentBlock{{Type: "text", Text: "done"}}}} {
		manager.result = result
		if _, err := a.Trash(ctx, "f1"); err == nil {
			t.Fatalf("invalid confirmation accepted: %+v", result)
		}
	}
	manager.result = &mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: `{"id":"f1","trashed":true}`}}}
	if out, err := a.Trash(ctx, "f1"); err != nil || !out.Confirmed {
		t.Fatalf("valid text JSON: %+v %v", out, err)
	}
	if g.requests[0].Actor != "automation:test" || g.requests[0].Effect != policy.EffectDestructive {
		t.Fatalf("policy request: %+v", g.requests[0])
	}
	manager.callErr = context.DeadlineExceeded
	if _, err := a.Trash(ctx, "f1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

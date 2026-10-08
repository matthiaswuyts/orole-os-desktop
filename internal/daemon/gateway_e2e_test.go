package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/gateway"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/skills"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// gwEnv is a daemon with the real skills registry, MCP manager, policy engine and IPC handlers,
// reached by a gateway server over a real authenticated IPC connection (named pipe on Windows).
type gwEnv struct {
	t     *testing.T
	d     *Daemon
	pol   *policy.Engine
	mgr   *mcp.Manager
	admin *ipc.Client // the "UI" connection
	cs    *sdk.ClientSession
	srv   *mcptest.Server
}

func newGatewayEnv(t *testing.T) *gwEnv {
	t.Helper()
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(filepath.Join(dir, "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	token := "gateway-test-token"
	srv := ipc.NewServer(token, "test", os.Getpid(), log)
	pol := policy.New(db.DB, srv, log)
	mgr := mcp.New(mcp.Options{DB: db.DB, Creds: credentials.NewMemory(), Pub: srv, Policy: pol, ConnectTimeout: 5 * time.Second, CallTimeout: 5 * time.Second})
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	d := &Daemon{DataDir: dir, Log: log, DB: db, IPC: srv, MCP: mgr, Policy: pol}
	d.registerSkills()

	endpoint := ipc.Endpoint(dir)
	ln, err := ipc.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { ln.Close(); srv.CloseAll() })

	env := &gwEnv{t: t, d: d, pol: pol, mgr: mgr}
	env.admin = ipc.NewClient(ipc.Dial(endpoint), token, "ui")
	t.Cleanup(env.admin.Close)

	gw := ipc.NewClient(ipc.Dial(endpoint), token, "gateway")
	t.Cleanup(gw.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	refresh := gateway.WatchEvents(ctx, gw)
	gs := gateway.NewServer(gateway.NewIPCBackend(gw, "sess-1"), "test", log)
	st, ct := sdk.NewInMemoryTransports()
	done := make(chan struct{})
	go func() { defer close(done); _ = gs.Run(ctx, st, refresh) }()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "harness", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close(); cancel(); <-done })
	env.cs = cs
	return env
}

func (e *gwEnv) call(ctx context.Context, method string, params, out any) {
	e.t.Helper()
	if err := e.admin.Call(ctx, method, params, out); err != nil {
		e.t.Fatalf("%s: %v", method, err)
	}
}

func (e *gwEnv) waitTool(name string, want bool) []*sdk.Tool {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := e.cs.ListTools(context.Background(), nil)
		if err != nil {
			e.t.Fatal(err)
		}
		for _, tool := range res.Tools {
			if tool.Name == name && want {
				return res.Tools
			}
		}
		found := false
		for _, tool := range res.Tools {
			found = found || tool.Name == name
		}
		if found == want {
			return res.Tools
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("tool %q never became present=%v", name, want)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (e *gwEnv) waitPending(actor string) policy.Pending {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, p := range e.pol.Pending() {
			if p.Actor == actor {
				return p
			}
		}
		if time.Now().After(deadline) {
			e.t.Fatal("no approval became pending")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func firstText(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("content is %T", res.Content[0])
	}
	return tc.Text
}

func TestGatewayEndToEnd(t *testing.T) {
	e := newGatewayEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// --- skills over the UI's IPC methods, served through the gateway ---
	var saved skills.Skill
	e.call(ctx, "skills.save", map[string]string{"name": "x", "body": "---\nname: release-notes\ndescription: Write release notes\n---\nUse the changelog.\n"}, &saved)
	if saved.Name != "release-notes" || !saved.Enabled {
		t.Fatalf("saved = %+v", saved)
	}
	e.call(ctx, "skills.save", map[string]string{"name": "off", "body": "---\nname: off-skill\ndescription: disabled one\n---\nsecret\n"}, &saved)
	e.call(ctx, "skills.setEnabled", map[string]any{"name": "off-skill", "enabled": false}, &saved)
	if err := e.admin.Call(ctx, "skills.save", map[string]string{"name": "../evil", "body": "---\ndescription: d\n---\n"}, nil); err == nil {
		t.Fatal("path traversal name accepted over IPC")
	}
	var list []skills.Skill
	e.call(ctx, "skills.list", struct{}{}, &list)
	if len(list) != 2 {
		t.Fatalf("skills.list = %+v", list)
	}

	res, err := e.cs.CallTool(ctx, &sdk.CallToolParams{Name: gateway.ToolListSkills})
	if err != nil || res.IsError {
		t.Fatalf("list skills: %v %+v", err, res)
	}
	if txt := firstText(t, res); !strings.Contains(txt, "release-notes: Write release notes") || strings.Contains(txt, "off-skill") {
		t.Fatalf("gateway skill list = %q (disabled skills must not be listed)", txt)
	}
	res, _ = e.cs.CallTool(ctx, &sdk.CallToolParams{Name: gateway.ToolGetSkill, Arguments: map[string]any{"name": "release-notes"}})
	if res.IsError || !strings.Contains(firstText(t, res), "Use the changelog.") {
		t.Fatalf("get_skill = %+v", res)
	}
	res, _ = e.cs.CallTool(ctx, &sdk.CallToolParams{Name: gateway.ToolGetSkill, Arguments: map[string]any{"name": "off-skill"}})
	if !res.IsError || strings.Contains(firstText(t, res), "secret") {
		t.Fatalf("disabled skill served: %+v", res)
	}
	res, _ = e.cs.CallTool(ctx, &sdk.CallToolParams{Name: gateway.ToolGetSkill, Arguments: map[string]any{"name": "release-notes", "file": "../../orole.db"}})
	if !res.IsError {
		t.Fatalf("traversal through get_skill: %+v", res)
	}

	// --- an MCP server connects: its tools appear (list_changed driven by daemon events) ---
	fake := mcptest.New(t, mcptest.Options{OpenMCP: true})
	state, err := e.mgr.Add(ctx, mcp.AddParams{Name: "Team Notes", URL: fake.MCPURL, AuthKind: mcp.AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	tools := e.waitTool("team_notes__echo", true)
	byName := map[string]*sdk.Tool{}
	for _, tl := range tools {
		byName[tl.Name] = tl
	}
	for _, n := range []string{"team_notes__echo", "team_notes__write_note", "team_notes__delete_note", gateway.ToolGetSkill} {
		if byName[n] == nil {
			t.Fatalf("missing tool %s in %v", n, byName)
		}
	}
	if !strings.Contains(byName["team_notes__echo"].Description, "untrusted") || byName["team_notes__echo"].Annotations == nil || !byName["team_notes__echo"].Annotations.ReadOnlyHint {
		t.Fatalf("echo tool = %+v", byName["team_notes__echo"])
	}
	if schema, _ := byName["team_notes__echo"].InputSchema.(map[string]any); schema["required"] == nil {
		t.Fatalf("server schema not forwarded: %v", byName["team_notes__echo"].InputSchema)
	}

	// --- allowed by policy ---
	if _, err := e.pol.AddRule(ctx, policy.Rule{Actor: "agent:sess-1", Action: "mcp.tool.call", Resource: "mcp/" + state.ID + "/echo", Decision: policy.Allow}); err != nil {
		t.Fatal(err)
	}
	res, err = e.cs.CallTool(ctx, &sdk.CallToolParams{Name: "team_notes__echo", Arguments: map[string]any{"message": "hello gateway"}})
	if err != nil || res.IsError || !strings.Contains(firstText(t, res), "hello gateway") {
		t.Fatalf("allowed call: %v %+v", err, res)
	}
	if res.Meta["orole/untrusted"] != true {
		t.Fatalf("result not marked untrusted: %v", res.Meta)
	}

	// --- needs approval: the UI's existing approval flow, denied then allowed ---
	type outcome struct {
		res *sdk.CallToolResult
		err error
	}
	callWrite := func() chan outcome {
		ch := make(chan outcome, 1)
		go func() {
			r, err := e.cs.CallTool(ctx, &sdk.CallToolParams{Name: "team_notes__write_note", Arguments: map[string]any{"text": "n1"}})
			ch <- outcome{r, err}
		}()
		return ch
	}
	ch := callWrite()
	p := e.waitPending("agent:sess-1")
	if p.Action != "mcp.tool.call" || p.Resource != "mcp/"+state.ID+"/write_note" || !strings.Contains(p.Detail, "n1") {
		t.Fatalf("pending approval = %+v", p)
	}
	if err := e.pol.Resolve(ctx, p.ID, policy.Resolution{Allow: false}); err != nil {
		t.Fatal(err)
	}
	if o := <-ch; o.err != nil || !o.res.IsError || !strings.Contains(strings.ToLower(firstText(t, o.res)), "denied") {
		t.Fatalf("denied call = %+v %v", o.res, o.err)
	}
	ch = callWrite()
	p = e.waitPending("agent:sess-1")
	if err := e.pol.Resolve(ctx, p.ID, policy.Resolution{Allow: true}); err != nil {
		t.Fatal(err)
	}
	if o := <-ch; o.err != nil || o.res.IsError || !strings.Contains(firstText(t, o.res), `"ok":true`) {
		t.Fatalf("approved call = %+v %v", o.res, o.err)
	}

	// --- denied by an explicit per-tool permission: no approval is even requested ---
	if err := e.mgr.SetToolPermission(ctx, state.ID, "delete_note", "deny"); err != nil {
		t.Fatal(err)
	}
	res, err = e.cs.CallTool(ctx, &sdk.CallToolParams{Name: "team_notes__delete_note", Arguments: map[string]any{"id": "1"}})
	if err != nil || !res.IsError || strings.Contains(firstText(t, res), `"deleted"`) {
		t.Fatalf("denied tool ran: %v %+v", err, res)
	}
	if n := len(e.pol.Pending()); n != 0 {
		t.Fatalf("%d approvals pending after a hard deny", n)
	}

	// --- unknown tools and bad arguments fail without reaching any server ---
	res, err = e.cs.CallTool(ctx, &sdk.CallToolParams{Name: "team_notes__echo", Arguments: json.RawMessage(`[1,2]`)})
	if err == nil && !res.IsError {
		t.Fatalf("array arguments accepted: %+v", res)
	}

	// --- audit trail attributes every call to the agent session ---
	var n int
	if err := e.d.DB.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE actor='agent:sess-1' AND action='mcp.tool.call'`).Scan(&n); err != nil || n < 4 {
		t.Fatalf("audit rows for agent:sess-1 = %d (%v)", n, err)
	}
	var detail string
	if err := e.d.DB.QueryRow(`SELECT outcome FROM audit_events WHERE actor='agent:sess-1' AND target=? ORDER BY id DESC LIMIT 1`, "mcp/"+state.ID+"/delete_note").Scan(&detail); err != nil || !strings.Contains(detail, "den") {
		t.Fatalf("delete_note audit outcome = %q (%v)", detail, err)
	}

	// --- the server disconnects: tools vanish, skill tools stay ---
	if _, err := e.mgr.SetEnabled(ctx, state.ID, false); err != nil {
		t.Fatal(err)
	}
	tools = e.waitTool("team_notes__echo", false)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	if !names[gateway.ToolListSkills] || !names[gateway.ToolGetSkill] || len(names) != 2 {
		t.Fatalf("tools after disconnect = %v", names)
	}
}

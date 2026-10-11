package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/internal/skills"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type nopPub struct{}

func (nopPub) Publish(string, any) {}

type hubRig struct {
	hub   *Hub
	m     *mcp.Manager
	calls atomic.Int32 // echo executions on the fake server
	srv   *mcptest.Server
}

func newHubRig(t *testing.T) *hubRig {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "orole.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m := mcp.New(mcp.Options{DB: db.DB, Creds: credentials.NewMemory(), Pub: nopPub{}, Policy: policy.New(db.DB, nopPub{}, nil),
		BackoffBase: 20 * time.Millisecond, BackoffMax: 100 * time.Millisecond, ConnectTimeout: 5 * time.Second, CallTimeout: 5 * time.Second})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	r := &hubRig{hub: &Hub{MCP: m}, m: m}
	r.srv = mcptest.New(t, mcptest.Options{OpenMCP: true, Setup: func(s *sdk.Server) {
		mcptest.AddJSONTool(s, "echo", "Echo", mcptest.ReadOnly(), nil, nil, func(map[string]any) (any, error) {
			r.calls.Add(1)
			return map[string]any{"ok": true}, nil
		})
	}})
	return r
}

func (r *hubRig) waitTools(t *testing.T, n int) ToolList {
	t.Helper()
	var l ToolList
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var err error
		if l, err = r.hub.ToolList(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
		if len(l.Tools) == n {
			return l
		}
	}
	t.Fatalf("tool list never reached %d tools; last %d", n, len(l.Tools))
	return l
}

func (r *hubRig) call(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.hub.Call(ctx, "s1", name, json.RawMessage(`{}`))
	return err
}

func TestHubIndexIsCachedUntilTheServerSetChanges(t *testing.T) {
	ctx := context.Background()
	r := newHubRig(t)
	st, err := r.m.Add(ctx, mcp.AddParams{Name: "notes", URL: r.srv.MCPURL, AuthKind: mcp.AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	l := r.waitTools(t, 1)
	if l.Tools[0].Name != "notes__echo" || l.ETag == "" {
		t.Fatalf("list = %+v", l)
	}

	// Repeated reads reuse one index: same object, same ETag, no rebuild.
	i1, err := r.hub.index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		i2, err := r.hub.index(ctx)
		if err != nil || i2 != i1 {
			t.Fatalf("index was rebuilt without a change (err=%v)", err)
		}
	}
	// A matching ETag costs no tool bodies; a stale or empty one gets the full list.
	if nm, err := r.hub.ToolList(ctx, l.ETag); err != nil || !nm.NotModified || len(nm.Tools) != 0 || nm.ETag != l.ETag {
		t.Fatalf("matching etag: %+v, %v", nm, err)
	}
	if full, err := r.hub.ToolList(ctx, "stale"); err != nil || full.NotModified || len(full.Tools) != 1 {
		t.Fatalf("stale etag: %+v, %v", full, err)
	}

	// Permission changes: the tool list is unchanged (the cache survives) but the decision applies
	// to the very next call, so a cached index can never widen access.
	if err := r.m.SetToolPermission(ctx, st.ID, "echo", "allow"); err != nil {
		t.Fatal(err)
	}
	if err := r.call("notes__echo"); err != nil {
		t.Fatalf("call with a stored allow: %v", err)
	}
	if err := r.m.SetToolPermission(ctx, st.ID, "echo", "deny"); err != nil {
		t.Fatal(err)
	}
	if i2, _ := r.hub.index(ctx); i2 != i1 {
		t.Fatal("a permission change must not rebuild the index")
	}
	before := r.calls.Load()
	if err := r.call("notes__echo"); !errors.Is(err, policy.ErrDenied) {
		t.Fatalf("call after deny = %v, want policy.ErrDenied", err)
	}
	if r.calls.Load() != before {
		t.Fatal("a denied call reached the server")
	}
	if err := r.m.SetToolPermission(ctx, st.ID, "echo", "allow"); err != nil {
		t.Fatal(err)
	}
	if err := r.call("notes__echo"); err != nil {
		t.Fatalf("call after allow: %v", err)
	}

	// Disabling removes the tools at once; the ETag moves; enabling brings them back.
	if _, err := r.m.SetEnabled(ctx, st.ID, false); err != nil {
		t.Fatal(err)
	}
	off := r.waitTools(t, 0)
	if off.ETag == l.ETag {
		t.Fatal("etag did not change when the server was disabled")
	}
	if err := r.call("notes__echo"); err == nil {
		t.Fatal("a disabled server's tool was still routable")
	}
	if _, err := r.m.SetEnabled(ctx, st.ID, true); err != nil {
		t.Fatal(err)
	}
	if on := r.waitTools(t, 1); on.ETag != l.ETag {
		t.Fatalf("same tools must give the same etag: %q vs %q", on.ETag, l.ETag)
	}
	if err := r.call("notes__echo"); err != nil {
		t.Fatalf("call after re-enable: %v", err)
	}

	// Removing the server drops its tools and routes.
	if err := r.m.Remove(ctx, st.ID); err != nil {
		t.Fatal(err)
	}
	if gone := r.waitTools(t, 0); gone.ETag != off.ETag {
		t.Fatalf("empty lists must share an etag: %q vs %q", gone.ETag, off.ETag)
	}
	if err := r.call("notes__echo"); err == nil {
		t.Fatal("a removed server's tool was still routable")
	}
}

// Skills are never cached: an install, disable or delete shows in the very next list and read,
// and none of it disturbs the cached MCP tool index.
func TestHubSkillChangesAreVisibleImmediately(t *testing.T) {
	ctx := context.Background()
	r := newHubRig(t)
	store, err := skills.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.hub.Skills = store
	if _, err := r.m.Add(ctx, mcp.AddParams{Name: "notes", URL: r.srv.MCPURL, AuthKind: mcp.AuthNone}); err != nil {
		t.Fatal(err)
	}
	r.waitTools(t, 1)
	idx, err := r.hub.index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := func() []string {
		t.Helper()
		list, err := r.hub.SkillList()
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(list))
		for _, s := range list {
			out = append(out, s.Name)
		}
		return out
	}
	if got := names(); len(got) != 0 {
		t.Fatalf("skills before install = %v", got)
	}
	if _, err := store.Save("", "---\nname: pdf-tools\ndescription: Work with PDFs\n---\n# PDF\n", ""); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 1 || got[0] != "pdf-tools" {
		t.Fatalf("skills after install = %v", got)
	}
	if c, err := r.hub.SkillGet("pdf-tools", ""); err != nil || c.Content == "" {
		t.Fatalf("SkillGet = %+v, %v", c, err)
	}
	if _, err := store.SetEnabled("pdf-tools", false); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 0 {
		t.Fatalf("skills after disable = %v", got)
	}
	if _, err := r.hub.SkillGet("pdf-tools", ""); !errors.Is(err, skills.ErrNotFound) {
		t.Fatalf("SkillGet of a disabled skill = %v, want ErrNotFound", err)
	}
	if _, err := store.SetEnabled("pdf-tools", true); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("pdf-tools"); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 0 {
		t.Fatalf("skills after delete = %v", got)
	}
	if again, _ := r.hub.index(ctx); again != idx {
		t.Fatal("a skill change rebuilt the MCP tool index")
	}
}

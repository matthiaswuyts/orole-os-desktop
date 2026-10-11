package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
)

func (r *recorder) count(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e == event {
			n++
		}
	}
	return n
}

// settle waits until no mcp.status event arrived for a while (connect churn has finished).
func (r *recorder) settle(t *testing.T) int {
	t.Helper()
	last, stable := -1, 0
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if n := r.count("mcp.status"); n != last {
			last, stable = n, 0
		} else if stable++; stable >= 5 {
			return n
		}
	}
	t.Fatal("mcp.status events never settled")
	return 0
}

func TestStatusIsPublishedOnlyOnChange(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	base := h.rec.settle(t)
	gen := h.m.Generation()

	// Repeating the same state, in any way it can be produced, publishes nothing.
	cur, err := h.m.Get(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		h.m.publishStatus(cur, false)
		if _, err := h.m.Update(ctx, st.ID, UpdateParams{Name: &cur.Name}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.m.SetEnabled(ctx, st.ID, true); err != nil {
			t.Fatal(err)
		}
		if _, err := h.m.RefreshCatalog(ctx, st.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.rec.settle(t); got != base {
		t.Fatalf("mcp.status events %d -> %d after repeating an unchanged state", base, got)
	}
	if got := h.m.Generation(); got != gen {
		t.Fatalf("Generation moved %d -> %d without a change", gen, got)
	}

	// A real change is published once and moves the generation.
	renamed := "Renamed"
	if _, err := h.m.Update(ctx, st.ID, UpdateParams{Name: &renamed}); err != nil {
		t.Fatal(err)
	}
	if got := h.rec.count("mcp.status"); got != base+1 {
		t.Fatalf("mcp.status events = %d, want %d after a rename", got, base+1)
	}
	if h.m.Generation() == gen {
		t.Fatal("Generation did not move on a rename")
	}
}

func TestEveryStateChangeMovesTheGeneration(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	moved := func(what string, f func()) {
		t.Helper()
		h.rec.settle(t)
		before := h.m.Generation()
		f()
		if h.m.Generation() == before {
			t.Fatalf("Generation did not move on %s", what)
		}
	}
	var st ServerState
	moved("add and connect", func() {
		st = h.add(t, srv, AddParams{AuthKind: AuthNone})
		h.wait(t, st.ID, StatusConnected)
	})
	moved("disable", func() {
		if _, err := h.m.SetEnabled(ctx, st.ID, false); err != nil {
			t.Fatal(err)
		}
	})
	moved("enable", func() {
		if _, err := h.m.SetEnabled(ctx, st.ID, true); err != nil {
			t.Fatal(err)
		}
		h.wait(t, st.ID, StatusConnected)
	})
	moved("remove", func() {
		if err := h.m.Remove(ctx, st.ID); err != nil {
			t.Fatal(err)
		}
	})
	if !h.rec.seen("mcp.removed") {
		t.Fatal("mcp.removed was not published")
	}
	h.m.statusMu.Lock()
	_, remembered := h.m.lastStatus[st.ID]
	h.m.statusMu.Unlock()
	if remembered {
		t.Fatal("removed server's last status is still remembered")
	}
}

func TestToolPermissionChangeLeavesGenerationAlone(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	h.rec.settle(t)
	before, permBefore := h.m.Generation(), h.m.PermissionVersion()
	if err := h.m.SetToolPermission(ctx, st.ID, "echo", "deny"); err != nil {
		t.Fatal(err)
	}
	if h.m.Generation() != before {
		t.Fatal("a permission change must not rebuild tool indexes: it never alters the tool list")
	}
	if h.m.PermissionVersion() == permBefore {
		t.Fatal("a permission change must move PermissionVersion: bots' offered tools depend on it")
	}
}

// A catalog that changes without changing the tool count must still be published, or the gateway
// and bots would keep serving the old tool definitions.
func TestCatalogContentChangeIsPublishedEvenWithSameToolCount(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	base := h.rec.settle(t)
	gen := h.m.Generation()

	mcptest.AddJSONTool(srv.MCP, "echo", "A completely different description.", mcptest.ReadOnly(),
		map[string]any{"message": map[string]any{"type": "string"}}, []string{"message"},
		func(a map[string]any) (any, error) { return a, nil })

	deadline := time.Now().Add(5 * time.Second)
	for {
		tools, err := h.m.Tools(ctx, st.ID)
		if err != nil {
			t.Fatal(err)
		}
		changed := false
		for _, ti := range tools {
			changed = changed || (ti.Name == "echo" && ti.Description == "A completely different description.")
		}
		if changed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("catalog never refreshed after tools/list_changed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := h.rec.settle(t); got <= base {
		t.Fatalf("mcp.status events = %d, want more than %d after the catalog changed", got, base)
	}
	if h.m.Generation() == gen {
		t.Fatal("Generation did not move when the catalog changed")
	}
}

func TestSameCatalogContent(t *testing.T) {
	a := &Catalog{ServerID: "s", FetchedAt: 1, Tools: []ToolInfo{{Name: "t", Description: "d"}}}
	same := &Catalog{ServerID: "s", FetchedAt: 99, Tools: []ToolInfo{{Name: "t", Description: "d"}}, Resources: []ResourceInfo{}}
	for name, tc := range map[string]struct {
		b    *Catalog
		want bool
	}{
		"fetched later":     {same, true},
		"description":       {&Catalog{Tools: []ToolInfo{{Name: "t", Description: "other"}}}, false},
		"extra tool":        {&Catalog{Tools: []ToolInfo{{Name: "t", Description: "d"}, {Name: "u"}}}, false},
		"prompt added":      {&Catalog{Tools: []ToolInfo{{Name: "t", Description: "d"}}, Prompts: []PromptInfo{{Name: "p"}}}, false},
		"resource added":    {&Catalog{Tools: []ToolInfo{{Name: "t", Description: "d"}}, Resources: []ResourceInfo{{URI: "x"}}}, false},
		"server version":    {&Catalog{Tools: []ToolInfo{{Name: "t", Description: "d"}}, ServerVersion: "2"}, false},
		"nil vs empty list": {&Catalog{Tools: []ToolInfo{{Name: "t", Description: "d"}}, Prompts: []PromptInfo{}}, true},
	} {
		if got := sameCatalogContent(a, tc.b); got != tc.want {
			t.Errorf("%s: sameCatalogContent = %v, want %v", name, got, tc.want)
		}
	}
	if sameCatalogContent(a, nil) || !sameCatalogContent(nil, nil) {
		t.Error("nil handling")
	}
}

func permissionOf(t *testing.T, m *Manager, id, tool string) string {
	t.Helper()
	cat, err := m.Catalog(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ti := range cat.Tools {
		if ti.Name == tool {
			return ti.Permission
		}
	}
	t.Fatalf("tool %q not in catalog", tool)
	return ""
}

func TestPermissionCacheFollowsEveryWrite(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)

	if got := permissionOf(t, h.m, st.ID, "write_note"); got != "" {
		t.Fatalf("initial permission = %q", got)
	}
	for _, decision := range []string{"deny", "allow", "ask", "deny", ""} {
		if err := h.m.SetToolPermission(ctx, st.ID, "write_note", decision); err != nil {
			t.Fatal(err)
		}
		if got := permissionOf(t, h.m, st.ID, "write_note"); got != decision {
			t.Fatalf("after setting %q the catalog shows %q", decision, got)
		}
		perms, err := h.m.ToolPermissions(ctx, st.ID)
		if err != nil || perms["write_note"] != decision {
			t.Fatalf("ToolPermissions after %q = %v (%v)", decision, perms, err)
		}
	}
	// A rejected write must not leave a stale entry behind either.
	_ = h.m.SetToolPermission(ctx, st.ID, "write_note", "deny")
	if err := h.m.SetToolPermission(ctx, st.ID, "write_note", "bogus"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("bogus decision: %v", err)
	}
	if got := permissionOf(t, h.m, st.ID, "write_note"); got != "deny" {
		t.Fatalf("permission after a rejected write = %q, want deny", got)
	}
}

func TestPermissionsAreServedFromMemoryAndIsolatedFromCallers(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	if err := h.m.SetToolPermission(ctx, st.ID, "echo", "allow"); err != nil {
		t.Fatal(err)
	}
	if got := permissionOf(t, h.m, st.ID, "echo"); got != "allow" { // warms the cache
		t.Fatalf("permission = %q", got)
	}
	// Mutating a returned map never reaches the cache.
	p, _ := h.m.ToolPermissions(ctx, st.ID)
	p["echo"] = "deny"
	if got := permissionOf(t, h.m, st.ID, "echo"); got != "allow" {
		t.Fatalf("caller mutation leaked into the cache: %q", got)
	}
	// A row written behind the manager's back is not re-read until SetToolPermission invalidates.
	if _, err := h.db.Exec(`UPDATE mcp_permissions SET decision='deny' WHERE server_id=? AND tool='echo'`, st.ID); err != nil {
		t.Fatal(err)
	}
	if got := permissionOf(t, h.m, st.ID, "echo"); got != "allow" {
		t.Fatalf("permissions were re-read from SQLite on every Catalog call (got %q)", got)
	}
	if err := h.m.SetToolPermission(ctx, st.ID, "write_note", "ask"); err != nil {
		t.Fatal(err)
	}
	if got := permissionOf(t, h.m, st.ID, "echo"); got != "deny" {
		t.Fatalf("invalidation did not reload the server's permissions (got %q)", got)
	}
}

func TestPermissionCacheDroppedWithTheServer(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	if err := h.m.SetToolPermission(ctx, st.ID, "echo", "deny"); err != nil {
		t.Fatal(err)
	}
	permissionOf(t, h.m, st.ID, "echo")
	if err := h.m.Remove(ctx, st.ID); err != nil {
		t.Fatal(err)
	}
	h.m.permMu.RLock()
	_, cached := h.m.perms[st.ID]
	h.m.permMu.RUnlock()
	if cached {
		t.Fatal("removed server's permissions are still cached")
	}
	if _, err := h.m.Catalog(ctx, st.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Catalog of a removed server: %v, want ErrNotFound", err)
	}
	if _, err := h.m.Tools(ctx, st.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Tools of a removed server: %v, want ErrNotFound", err)
	}
	if err := h.m.SetToolPermission(ctx, st.ID, "echo", "allow"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetToolPermission on a removed server: %v", err)
	}
}

// Readers racing writers must end up agreeing with the last write (-race also checks the epoch
// logic that keeps a read that began before a write from caching its stale answer).
func TestPermissionCacheUnderConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)

	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 40 {
				d := []string{"allow", "deny", "ask"}[(w+i)%3]
				if err := h.m.SetToolPermission(ctx, st.ID, "echo", d); err != nil {
					t.Error(err)
					return
				}
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 80 {
				if _, err := h.m.Catalog(ctx, st.ID); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	var onDisk string
	if err := h.db.QueryRow(`SELECT decision FROM mcp_permissions WHERE server_id=? AND tool='echo'`, st.ID).Scan(&onDisk); err != nil {
		t.Fatal(err)
	}
	if got := permissionOf(t, h.m, st.ID, "echo"); got != onDisk {
		t.Fatalf("cached permission %q disagrees with SQLite %q", got, onDisk)
	}
}

func TestServerConfigComesFromTheLiveConnection(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	// server() answers from the connection, and falls back to the registry only without one.
	name := "Fresh"
	if _, err := h.m.Update(ctx, st.ID, UpdateParams{Name: &name}); err != nil {
		t.Fatal(err)
	}
	cfg, err := h.m.server(ctx, st.ID)
	if err != nil || cfg.Name != "Fresh" {
		t.Fatalf("server() = %+v, %v", cfg, err)
	}
	if err := h.m.Remove(ctx, st.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.server(ctx, st.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("server() after remove: %v", err)
	}
}

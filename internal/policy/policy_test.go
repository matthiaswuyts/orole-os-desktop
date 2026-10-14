package policy

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type capture struct {
	mu     sync.Mutex
	events []string
	last   map[string]any
	notify chan struct{}
}

func newCapture() *capture { return &capture{notify: make(chan struct{}, 64)} }

func (c *capture) Publish(event string, data any) {
	c.mu.Lock()
	c.events = append(c.events, event)
	if m, ok := data.(map[string]any); ok && event == "policy.pending" {
		c.last = m
	}
	c.mu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func newEngine(t *testing.T) (*Engine, *database.DB, *capture) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := newCapture()
	return New(db.DB, c, nil), db, c
}

func waitPending(t *testing.T, e *Engine) Pending {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p := e.Pending(); len(p) > 0 {
			return p[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no pending approval appeared")
	return Pending{}
}

func TestDefaults(t *testing.T) {
	e, _, _ := newEngine(t)
	ctx := context.Background()
	tests := []struct {
		name string
		req  Request
		want Decision
	}{
		{"read allowed", Request{Actor: "user", Action: "mail.list", Resource: "mail/x", Effect: EffectRead}, Allow},
		{"read from hint asks", Request{Actor: "agent:1", Action: "mcp.tool.call", Resource: "mcp/s/t", Effect: EffectRead, FromHint: true}, Ask},
		{"write asks", Request{Actor: "agent:1", Action: "mail.send", Resource: "mail/x", Effect: EffectWrite}, Ask},
		{"destructive asks", Request{Actor: "user", Action: "drive.trash", Resource: "drive/x/a", Effect: EffectDestructive}, Ask},
		{"unclassified denied", Request{Actor: "user", Action: "mystery", Resource: "r"}, Deny},
		{"missing action denied", Request{Actor: "user", Effect: EffectRead}, Deny},
		{"bogus effect denied", Request{Actor: "user", Action: "x", Effect: "nuke"}, Deny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := e.Decide(ctx, tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if ev.Decision != tt.want {
				t.Fatalf("got %s (%s), want %s", ev.Decision, ev.Reason, tt.want)
			}
		})
	}
}

func TestTaskSubmissionPermissions(t *testing.T) {
	e, _, _ := newEngine(t)
	ctx := context.Background()
	for _, action := range []string{"agent.start", "agent.prompt", "agent.restart", "agent.permissionMode"} {
		for _, actor := range []string{"user", "agent:1", "automation:1", "system"} {
			req := Request{Actor: actor, Action: action, Resource: "local:project", Effect: EffectDestructive}
			ev, err := e.Decide(ctx, req)
			want := Ask
			if actor == "user" {
				want = Allow
			}
			if err != nil || ev.Decision != want {
				t.Fatalf("%s %s: got %+v, %v; want %s", actor, action, ev, err, want)
			}
		}
		for _, decision := range []Decision{Deny, Ask} {
			if _, err := e.AddRule(ctx, Rule{Actor: "user", Action: action, Resource: "restricted", Decision: decision}); err != nil {
				t.Fatal(err)
			}
			ev, err := e.Decide(ctx, Request{Actor: "user", Action: action, Resource: "restricted", Effect: EffectDestructive})
			if err != nil || ev.Decision != decision {
				t.Fatalf("explicit %s rule must govern %s: %+v %v", decision, action, ev, err)
			}
		}
	}
	for _, req := range []Request{
		{Actor: "user", Action: "agent.start", Effect: EffectDestructive, FromHint: true},
		{Actor: "user", Action: "agent.tool.use", Effect: EffectDestructive},
		{Actor: "agent:1", Action: "agent.tool.use", Effect: EffectDestructive},
	} {
		ev, err := e.Decide(ctx, req)
		if err != nil || ev.Decision != Ask {
			t.Fatalf("task submission must not grant tool or hinted permissions: %+v %v", ev, err)
		}
	}
}

func TestRulesSpecificityAndEffectLimit(t *testing.T) {
	e, _, _ := newEngine(t)
	ctx := context.Background()
	mustRule := func(r Rule) Rule {
		t.Helper()
		r, err := e.AddRule(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// Broad: allow read-only mcp tools of server s.
	mustRule(Rule{Action: "mcp.tool.call", Resource: "mcp/s/*", MaxEffect: EffectRead, Decision: Allow})
	// Specific deny on one tool beats broad allow.
	mustRule(Rule{Action: "mcp.tool.call", Resource: "mcp/s/dangerous", Decision: Deny})

	get := func(res string, eff Effect, hint bool) Decision {
		ev, err := e.Decide(ctx, Request{Actor: "agent:1", Action: "mcp.tool.call", Resource: res, Effect: eff, FromHint: hint})
		if err != nil {
			t.Fatal(err)
		}
		return ev.Decision
	}
	if d := get("mcp/s/list", EffectRead, true); d != Allow {
		t.Fatalf("read tool under max_effect=read rule: %s", d)
	}
	if d := get("mcp/s/write", EffectWrite, true); d != Ask {
		t.Fatalf("write tool must not match read-limited rule: %s", d)
	}
	if d := get("mcp/s/dangerous", EffectRead, true); d != Deny {
		t.Fatalf("specific deny must win: %s", d)
	}
	if d := get("mcp/other/list", EffectRead, true); d != Ask {
		t.Fatalf("other server unaffected: %s", d)
	}

	// Same specificity: deny beats allow.
	mustRule(Rule{Actor: "agent:9", Action: "x.y", Resource: "r", Decision: Allow})
	mustRule(Rule{Actor: "agent:9", Action: "x.y", Resource: "r", MaxEffect: EffectDestructive, Decision: Deny})
	ev, _ := e.Decide(ctx, Request{Actor: "agent:9", Action: "x.y", Resource: "r", Effect: EffectWrite})
	if ev.Decision != Deny {
		t.Fatalf("got %s", ev.Decision)
	}

	// Re-adding replaces, not duplicates.
	mustRule(Rule{Action: "mcp.tool.call", Resource: "mcp/s/dangerous", Decision: Allow})
	rules, _ := e.Rules(ctx)
	n := 0
	for _, r := range rules {
		if r.Resource == "mcp/s/dangerous" {
			n++
			if r.Decision != Allow {
				t.Fatalf("not replaced: %+v", r)
			}
		}
	}
	if n != 1 {
		t.Fatalf("duplicate rules: %d", n)
	}
	if _, err := e.AddRule(ctx, Rule{Action: "a", Decision: "maybe"}); err == nil {
		t.Fatal("invalid decision accepted")
	}
}

func TestMcpPermissionTableWins(t *testing.T) {
	e, db, _ := newEngine(t)
	ctx := context.Background()
	db.Exec(`INSERT INTO mcp_servers(id,name,url,created_at) VALUES('s','S','http://x',1)`)
	db.Exec(`INSERT INTO mcp_permissions(server_id,tool,decision) VALUES('s','send','deny'),('s','list','allow')`)
	if _, err := e.AddRule(ctx, Rule{Action: "mcp.tool.call", Resource: "mcp/s/send", Decision: Allow}); err != nil {
		t.Fatal(err)
	}
	ev, _ := e.Decide(ctx, Request{Actor: "a", Action: "mcp.tool.call", Resource: "mcp/s/send", Effect: EffectWrite})
	if ev.Decision != Deny || ev.Source != "mcp_permission" {
		t.Fatalf("per-tool permission must win: %+v", ev)
	}
	ev, _ = e.Decide(ctx, Request{Actor: "a", Action: "mcp.tool.call", Resource: "mcp/s/list", Effect: EffectWrite})
	if ev.Decision != Allow {
		t.Fatalf("got %+v", ev)
	}
}

func TestCheckBlocksUntilApproved(t *testing.T) {
	e, db, cap := newEngine(t)
	req := Request{Actor: "agent:7", Action: "mail.send", Resource: "mail/s", Effect: EffectWrite,
		Detail: "To: a@b.c\x1b[31m subject\u202e evil"}
	done := make(chan struct{})
	var dec Decision
	var err error
	go func() {
		dec, err = e.Check(context.Background(), req)
		close(done)
	}()
	p := waitPending(t, e)
	select {
	case <-done:
		t.Fatal("Check returned before approval")
	case <-time.After(50 * time.Millisecond):
	}
	if strings.ContainsAny(p.Detail, "\x1b\u202e") {
		t.Fatalf("detail not sanitised: %q", p.Detail)
	}
	if !p.CanRemember || p.Actor != "agent:7" {
		t.Fatalf("pending: %+v", p)
	}
	if err := e.Resolve(context.Background(), p.ID, Resolution{Allow: true}); err != nil {
		t.Fatal(err)
	}
	<-done
	if err != nil || dec != Allow {
		t.Fatalf("got %s, %v", dec, err)
	}
	if len(e.Pending()) != 0 {
		t.Fatal("pending not cleared")
	}
	var out, detail string
	db.QueryRow(`SELECT outcome, detail FROM audit_events WHERE action='mail.send' ORDER BY id DESC LIMIT 1`).Scan(&out, &detail)
	if out != "allowed" || !strings.Contains(detail, "approved by user") {
		t.Fatalf("audit: %s %s", out, detail)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	found := false
	for _, ev := range cap.events {
		if ev == "policy.pending" {
			found = true
		}
	}
	if !found {
		t.Fatal("policy.pending event not published")
	}
}

func TestCheckRejectTimeoutCancel(t *testing.T) {
	e, db, _ := newEngine(t)
	base := Request{Actor: "agent:1", Action: "drive.trash", Resource: "drive/s/f", Effect: EffectWrite}

	t.Run("reject", func(t *testing.T) {
		done := make(chan Decision, 1)
		go func() { d, _ := e.Check(context.Background(), base); done <- d }()
		p := waitPending(t, e)
		if err := e.Resolve(context.Background(), p.ID, Resolution{Allow: false}); err != nil {
			t.Fatal(err)
		}
		if d := <-done; d != Deny {
			t.Fatalf("got %s", d)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		r := base
		r.Timeout = 30 * time.Millisecond
		d, err := e.Check(context.Background(), r)
		if d != Deny || !errors.Is(err, ErrApprovalTimeout) {
			t.Fatalf("got %s, %v", d, err)
		}
		if len(e.Pending()) != 0 {
			t.Fatal("timed out approval still pending")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := e.Check(ctx, base); done <- err }()
		waitPending(t, e)
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("require", func(t *testing.T) {
		r := base
		r.Action = "unknown.thing"
		r.Effect = EffectUnknown
		if err := e.Require(context.Background(), r); !errors.Is(err, ErrDenied) {
			t.Fatalf("got %v", err)
		}
	})
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE outcome='denied'`).Scan(&n)
	if n < 4 {
		t.Fatalf("expected denied audit rows, got %d", n)
	}
}

func TestRememberCreatesRuleButNotForDestructive(t *testing.T) {
	e, _, _ := newEngine(t)
	ctx := context.Background()
	req := Request{Actor: "agent:1", Action: "mail.send", Resource: "mail/s", Effect: EffectWrite}
	done := make(chan Decision, 1)
	go func() { d, _ := e.Check(ctx, req); done <- d }()
	p := waitPending(t, e)
	if err := e.Resolve(ctx, p.ID, Resolution{Allow: true, Remember: true}); err != nil {
		t.Fatal(err)
	}
	<-done
	// Second identical request is now allowed without prompting.
	d, err := e.Check(ctx, req)
	if err != nil || d != Allow {
		t.Fatalf("remembered rule not applied: %s %v", d, err)
	}
	if len(e.Pending()) != 0 {
		t.Fatal("should not have prompted")
	}

	dreq := Request{Actor: "agent:1", Action: "mail.trash", Resource: "mail/s/1", Effect: EffectDestructive}
	go func() { e.Check(ctx, dreq) }()
	p = waitPending(t, e)
	if p.CanRemember {
		t.Fatal("destructive approvals must not be rememberable")
	}
	if err := e.Resolve(ctx, p.ID, Resolution{Allow: true, Remember: true}); !errors.Is(err, ErrNoRemember) {
		t.Fatalf("got %v", err)
	}
	if err := e.Resolve(ctx, p.ID, Resolution{Allow: false}); err != nil {
		t.Fatal(err)
	}
	if err := e.Resolve(ctx, "nope", Resolution{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestRememberedGrantCannotEscalateEffect(t *testing.T) {
	for _, effect := range []Effect{EffectRead, EffectWrite} {
		t.Run(string(effect), func(t *testing.T) {
			e, _, _ := newEngine(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			req := Request{Actor: "user", Action: "mcp.tool.call", Resource: "mcp/server/tool", Effect: effect, FromHint: true}
			done := make(chan error, 1)
			go func() { done <- e.Require(ctx, req) }()
			p := waitPending(t, e)
			if err := e.Resolve(ctx, p.ID, Resolution{Allow: true, Remember: true}); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			reopened := New(e.db, nil, nil)
			for _, next := range []Effect{EffectRead, EffectWrite, EffectDestructive} {
				req.Effect = next
				ev, err := reopened.Decide(ctx, req)
				want := Allow
				if next.rank() > effect.rank() {
					want = Ask
				}
				if err != nil || ev.Decision != want {
					t.Fatalf("remembered %s, now %s: got %+v (%v), want %s", effect, next, ev, err, want)
				}
			}
		})
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		p, s string
		ok   bool
	}{
		{"*", "anything", true},
		{"mcp/s/*", "mcp/s/tool", true},
		{"mcp/s/*", "mcp/t/tool", false},
		{"a*c", "abc", true},
		{"a*c", "abd", false},
		{"*/x/*", "p/x/q", true},
		{"exact", "exact", true},
		{"exact", "exactly", false},
		{"a**", "a", true},
	}
	for _, c := range cases {
		if got := match(c.p, c.s); got != c.ok {
			t.Errorf("match(%q,%q)=%v want %v", c.p, c.s, got, c.ok)
		}
	}
}

func TestAutoApproveOnlyLiftsDefaultAsk(t *testing.T) {
	e, db, _ := newEngine(t)
	ctx := context.Background()
	db.Exec(`INSERT INTO mcp_servers(id,name,url,created_at) VALUES('s','S','http://x',1)`)
	db.Exec(`INSERT INTO mcp_permissions(server_id,tool,decision) VALUES('s','ask','ask'),('s','deny','deny')`)
	for _, r := range []Rule{
		{Actor: "bot:b", Action: "rule.ask", Decision: Ask},
		{Actor: "bot:b", Action: "rule.deny", Decision: Deny},
	} {
		if _, err := e.AddRule(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	levels := map[string]Effect{"none": EffectUnknown, "read": EffectRead, "write": EffectWrite, "all": EffectDestructive}
	var level string
	e.SetAutoApprove(func(_ context.Context, actor string) (Effect, string) {
		if actor != "bot:b" {
			return EffectUnknown, ""
		}
		return levels[level], "bot tool access setting: " + level
	})
	read := Request{Actor: "bot:b", Action: "chat.tool.call", Resource: "chat/b/x", Effect: EffectRead}
	hintRead := Request{Actor: "bot:b", Action: "mcp.tool.call", Resource: "mcp/s/free", Effect: EffectRead, FromHint: true}
	write := Request{Actor: "bot:b", Action: "mcp.tool.call", Resource: "mcp/s/free", Effect: EffectWrite, FromHint: true}
	destructive := Request{Actor: "bot:b", Action: "mcp.tool.call", Resource: "mcp/s/free", Effect: EffectDestructive, FromHint: true}
	explicitAsk := Request{Actor: "bot:b", Action: "mcp.tool.call", Resource: "mcp/s/ask", Effect: EffectRead, FromHint: true}
	explicitDeny := Request{Actor: "bot:b", Action: "mcp.tool.call", Resource: "mcp/s/deny", Effect: EffectRead}
	ruleAsk := Request{Actor: "bot:b", Action: "rule.ask", Resource: "r", Effect: EffectRead, FromHint: true}
	ruleDeny := Request{Actor: "bot:b", Action: "rule.deny", Resource: "r", Effect: EffectRead}
	unknown := Request{Actor: "bot:b", Action: "mystery", Resource: "r"}
	user := Request{Actor: "user", Action: "mail.send", Resource: "mail/x", Effect: EffectWrite}
	tests := []struct {
		level string
		req   Request
		want  Decision
		auto  bool
	}{
		{"none", read, Allow, false},
		{"none", hintRead, Ask, false},
		{"none", write, Ask, false},
		{"read", hintRead, Allow, true},
		{"read", write, Ask, false},
		{"read", destructive, Ask, false},
		{"write", hintRead, Allow, true},
		{"write", write, Allow, true},
		{"write", destructive, Ask, false},
		{"all", hintRead, Allow, true},
		{"all", write, Allow, true},
		{"all", destructive, Allow, true},
		{"all", explicitAsk, Ask, false},
		{"all", explicitDeny, Deny, false},
		{"all", ruleAsk, Ask, false},
		{"all", ruleDeny, Deny, false},
		{"all", unknown, Deny, false},
		{"all", user, Ask, false},
	}
	for _, tt := range tests {
		t.Run(tt.level+" "+tt.req.Action+" "+tt.req.Resource+" "+string(tt.req.Effect), func(t *testing.T) {
			level = tt.level
			ev, err := e.Decide(ctx, tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if ev.Decision != tt.want || (ev.Source == SourceAutoApprove) != tt.auto {
				t.Fatalf("got %+v, want %s (auto-approved %v)", ev, tt.want, tt.auto)
			}
			if tt.auto && !strings.Contains(ev.Reason, "bot tool access setting") {
				t.Fatalf("reason does not name the setting: %q", ev.Reason)
			}
		})
	}
}

func TestAutoApprovedCheckIsAuditedWithoutPrompt(t *testing.T) {
	e, db, _ := newEngine(t)
	ctx := context.Background()
	e.SetAutoApprove(func(context.Context, string) (Effect, string) { return EffectWrite, "Bot tools: reads and changes" })
	req := Request{Actor: "bot:b", Action: "mail.send", Resource: "mail/x", Effect: EffectWrite}
	if d, err := e.Check(ctx, req); err != nil || d != Allow {
		t.Fatalf("check: %s %v", d, err)
	}
	if p := e.Pending(); len(p) != 0 {
		t.Fatalf("auto-approved call queued an approval: %+v", p)
	}
	var out, detail string
	db.QueryRow(`SELECT outcome, detail FROM audit_events WHERE action='mail.send' ORDER BY id DESC LIMIT 1`).Scan(&out, &detail)
	if out != "allowed" || !strings.Contains(detail, SourceAutoApprove+": Bot tools: reads and changes") {
		t.Fatalf("audit: %s %s", out, detail)
	}
	// Removing the hook restores the prompt.
	e.SetAutoApprove(nil)
	if ev, err := e.Decide(ctx, req); err != nil || ev.Decision != Ask {
		t.Fatalf("without hook: %+v %v", ev, err)
	}
}

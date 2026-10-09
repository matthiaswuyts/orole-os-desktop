package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/chat"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
	"github.com/rayorole/orole-os-desktop/internal/skills"
	"github.com/rayorole/orole-os-desktop/migrations"
)

// fakeLLM is an llm.Service serving one provider; unrelated methods panic through the nil embed.
type fakeLLM struct {
	llm.Service
	mu       sync.Mutex
	prov     llm.Provider
	defaults llm.Defaults
}

func (f *fakeLLM) Provider(context.Context, string) (llm.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prov == nil {
		return nil, llm.ErrNotConfigured
	}
	return f.prov, nil
}

func (f *fakeLLM) Defaults(context.Context) (llm.Defaults, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.defaults, nil
}

type event struct {
	name    string
	payload any
}

type audit struct{ action, target, outcome, detail string }

type env struct {
	t      *testing.T
	db     *sql.DB
	dir    string
	s      *Store
	fake   *llmtest.Fake
	llm    *fakeLLM
	skills *skills.Store

	mu      sync.Mutex
	events  []event
	audits  []audit
	clock   time.Time
	ids     int
	answers []string // consolidation answers served in order; the last one repeats
}

var toolCaps = llm.Caps{Tools: true}

// newEnv opens a migrated temp database and a Store whose memory model is a scripted fake.
func newEnv(t *testing.T, mods ...func(*Config)) *env {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "x.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	sk, err := skills.NewStore(filepath.Join(dir, "skills"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db.DB, dir: dir, skills: sk, clock: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	e.fake = &llmtest.Fake{IDValue: "fake", Run: func(call int, _ llm.Request) []llm.Delta {
		e.mu.Lock()
		defer e.mu.Unlock()
		if len(e.answers) == 0 {
			return []llm.Delta{llmtest.Text(`{"facts":[],"invalidate":[]}`)}
		}
		a := e.answers[min(call, len(e.answers)-1)]
		return []llm.Delta{llmtest.Text(a)}
	}}
	e.llm = &fakeLLM{prov: e.fake, defaults: llm.Defaults{MemoryModel: "fake/mem"}}
	cfg := Config{
		DB: db.DB, DataDir: dir, LLM: e.llm, Skills: sk,
		Publish: func(name string, payload any) {
			e.mu.Lock()
			e.events = append(e.events, event{name, payload})
			e.mu.Unlock()
		},
		Audit: func(a, tg, o, d string) {
			e.mu.Lock()
			e.audits = append(e.audits, audit{a, tg, o, d})
			e.mu.Unlock()
		},
		Now: func() time.Time {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.clock
		},
	}
	for _, m := range mods {
		m(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	e.s = s
	return e
}

func (e *env) answer(a ...string) {
	e.mu.Lock()
	e.answers = a
	e.mu.Unlock()
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.clock = e.clock.Add(d)
	e.mu.Unlock()
}

func (e *env) published() []chat.MemoryChange {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []chat.MemoryChange
	for _, ev := range e.events {
		if c, ok := ev.payload.(chat.MemoryChange); ok && ev.name == "chat.memory" {
			out = append(out, c)
		}
	}
	return out
}

func (e *env) auditActions() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, a := range e.audits {
		out = append(out, a.action+":"+a.target+":"+a.outcome)
	}
	return out
}

// bot inserts an active bot with the given config.
func (e *env) bot(id, name string, mods ...func(*chat.BotConfig)) chat.Bot {
	e.t.Helper()
	var cfg chat.BotConfig
	for _, m := range mods {
		m(&cfg)
	}
	raw, _ := json.Marshal(cfg)
	_, err := e.db.Exec(`INSERT INTO bots(id, name, description, config, created_at, updated_at) VALUES (?,?,?,?,1,1)`,
		id, name, "desc of "+name, string(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	return e.loadBot(id)
}

func (e *env) loadBot(id string) chat.Bot {
	e.t.Helper()
	b, err := e.s.loadBot(context.Background(), e.db, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *env) setConfig(id string, mod func(*chat.BotConfig)) chat.Bot {
	e.t.Helper()
	b := e.loadBot(id)
	mod(&b.Config)
	raw, _ := json.Marshal(b.Config)
	if _, err := e.db.Exec(`UPDATE bots SET config = ? WHERE id = ?`, string(raw), id); err != nil {
		e.t.Fatal(err)
	}
	return e.loadBot(id)
}

func grantTo(scope string, read, write bool) func(*chat.BotConfig) {
	return func(c *chat.BotConfig) {
		c.Memory = append(c.Memory, chat.ScopeAccess{Scope: scope, Read: read, Write: write})
	}
}

func executive(c *chat.BotConfig) { c.Executive = true }

// shared creates a shared scope and returns its id.
func (e *env) shared(name string) string {
	e.t.Helper()
	sc, err := e.s.CreateScope(context.Background(), name)
	if err != nil {
		e.t.Fatal(err)
	}
	return sc.ID
}

// run is a fake run context for bot b; parts collects what the tools emit.
type run struct {
	chat.RunContext
	mu    sync.Mutex
	parts []chat.MsgPart
}

func (r *run) emitted() []chat.MsgPart {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]chat.MsgPart(nil), r.parts...)
}

func newRun(b chat.Bot) *run {
	r := &run{}
	r.RunContext = chat.RunContext{BotID: b.ID, Bot: b, MessageID: "msg-" + b.ID, Actor: "bot:" + b.ID, Caps: toolCaps}
	r.Emit = func(p chat.MsgPart) {
		r.mu.Lock()
		r.parts = append(r.parts, p)
		r.mu.Unlock()
	}
	return r
}

func (e *env) tool(id string) chat.Tool {
	e.t.Helper()
	for _, tl := range e.s.Tools() {
		if tl.ID() == id {
			return tl
		}
	}
	e.t.Fatalf("no tool %q", id)
	return nil
}

// call runs a tool and fails the test on a Go error (tool-level failures arrive as IsError).
func (e *env) call(id string, r *run, args any) chat.ToolResult {
	e.t.Helper()
	return e.callCtx(context.Background(), id, r, args)
}

func (e *env) callCtx(ctx context.Context, id string, r *run, args any) chat.ToolResult {
	e.t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := e.tool(id).Call(ctx, r.RunContext, id, raw)
	if err != nil {
		e.t.Fatalf("%s: unexpected Go error: %v", id, err)
	}
	return res
}

func (e *env) mem(r *run, args map[string]any) chat.ToolResult {
	e.t.Helper()
	return e.call("memory", r, args)
}

// addNote adds a core note through the memory tool and requires success.
func (e *env) addNote(r *run, text string) {
	e.t.Helper()
	res := e.mem(r, map[string]any{"action": "add", "content": text})
	if res.IsError {
		e.t.Fatalf("add %q: %s", text, res.Text)
	}
}

// fact inserts an active archival fact directly.
func (e *env) fact(scope, text, trust string) chat.MemoryEntry {
	e.t.Helper()
	now := e.s.nowMs()
	m, err := insertEntry(context.Background(), e.db, chat.MemoryEntry{Scope: scope, Kind: chat.KindFact, Text: text, Trust: trust,
		Author: "user", CreatedAt: now, UpdatedAt: now}, "")
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

func (e *env) entries(scope string, mods ...func(*chat.MemoryListRequest)) []chat.MemoryEntry {
	e.t.Helper()
	req := chat.MemoryListRequest{Scope: scope, IncludePending: true, IncludeInvalidated: true, Limit: 500}
	for _, m := range mods {
		m(&req)
	}
	out, err := e.s.ListEntries(context.Background(), req)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func texts(es []chat.MemoryEntry) []string {
	out := make([]string, len(es))
	for i, x := range es {
		out[i] = x.Text
	}
	return out
}

// msg inserts a conversation message.
func (e *env) msg(botID string, seq int, role, text string, mods ...func(*msgRow)) {
	e.t.Helper()
	m := msgRow{id: fmt.Sprintf("%s-m%d", botID, seq), status: chat.StatusComplete, parts: "[]", at: e.s.nowMs() + int64(seq)}
	for _, mod := range mods {
		mod(&m)
	}
	_, err := e.db.Exec(`INSERT INTO messages(id, bot_id, seq, role, author, parts, text, status, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		m.id, botID, seq, role, m.author, m.parts, text, m.status, m.at)
	if err != nil {
		e.t.Fatal(err)
	}
}

type msgRow struct {
	id, author, parts, status string
	at                        int64
}

func withParts(ps ...chat.MsgPart) func(*msgRow) {
	return func(m *msgRow) {
		raw, _ := json.Marshal(ps)
		m.parts = string(raw)
	}
}

func withStatus(s string) func(*msgRow) { return func(m *msgRow) { m.status = s } }
func withAt(ms int64) func(*msgRow)     { return func(m *msgRow) { m.at = ms } }

func (e *env) upto(botID string) int64 {
	e.t.Helper()
	var n int64
	err := e.db.QueryRow(`SELECT upto_seq FROM memory_consolidation WHERE bot_id = ?`, botID).Scan(&n)
	if err == sql.ErrNoRows {
		return 0
	}
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// attachment stores a blob and its chat_attachments row, returning the attachment id.
func (e *env) attachment(name, mime string, data []byte) string {
	e.t.Helper()
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	dir := filepath.Join(e.dir, "chat", "attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sha), data, 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.mu.Lock()
	e.ids++
	id := fmt.Sprintf("att-%d", e.ids)
	e.mu.Unlock()
	if _, err := e.db.Exec(`INSERT INTO chat_attachments(id, sha256, name, mime, size, created_at) VALUES (?,?,?,?,?,1)`,
		id, sha, name, mime, len(data)); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func contains(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("missing %q in:\n%s", sub, s)
		}
	}
}

func excludes(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			t.Errorf("unexpected %q in:\n%s", sub, s)
		}
	}
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

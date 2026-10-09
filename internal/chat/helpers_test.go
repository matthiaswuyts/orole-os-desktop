package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
	"github.com/rayorole/orole-os-desktop/migrations"
)


type (
	llmRequest = llm.Request
	llmDelta   = llm.Delta
)
// fakeLLM is an llm.Service that serves one provider; unrelated methods panic via the nil embed.
type fakeLLM struct {
	llm.Service
	mu       sync.Mutex
	prov     llm.Provider
	models   []llm.Model
	defaults llm.Defaults
	modelsN  int
}

func (f *fakeLLM) Provider(context.Context, string) (llm.Provider, error) {
	if f.prov == nil {
		return nil, llm.ErrNotConfigured
	}
	return f.prov, nil
}

func (f *fakeLLM) Models(context.Context, string) ([]llm.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modelsN++
	return f.models, nil
}

func (f *fakeLLM) Defaults(context.Context) (llm.Defaults, error) { return f.defaults, nil }

type recorder struct {
	mu     sync.Mutex
	events []recorded
}

type recorded struct {
	event   string
	payload any
}

func (r *recorder) publish(event string, payload any) {
	r.mu.Lock()
	r.events = append(r.events, recorded{event, payload})
	r.mu.Unlock()
}

// messages returns the chat.message snapshots for one bot id in publish order.
func (r *recorder) messages(botID string) []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Message
	for _, e := range r.events {
		if m, ok := e.payload.(Message); ok && e.event == "chat.message" && m.BotID == botID {
			out = append(out, m)
		}
	}
	return out
}

func (r *recorder) all() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.events...)
}

type env struct {
	t    *testing.T
	m    *Manager
	llm  *fakeLLM
	fake *llmtest.Fake
	rec  *recorder
	db   *database.DB
	dir  string
	now  time.Time
	mu   sync.Mutex
}

var testModel = llm.Model{ID: "m1", Name: "M1", Caps: llm.Caps{Tools: true, Reasoning: true}, Reasoning: []string{"low", "high"}}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

// newEnv opens a temp DB and a Manager backed by a scripted fake provider.
func newEnv(t *testing.T, script llmtest.Script, mods ...func(*Config)) *env {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "x.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	fake := &llmtest.Fake{IDValue: "fake", Run: script, ModelList: []llm.Model{testModel}}
	e := &env{t: t, fake: fake, rec: &recorder{}, db: db, dir: dir, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	e.llm = &fakeLLM{prov: fake, models: []llm.Model{testModel}, defaults: llm.Defaults{ChatModel: "fake/m1", MemoryModel: "fake/m1"}}
	cfg := Config{DB: db.DB, DataDir: dir, LLM: e.llm, Publish: e.rec.publish, Clock: e.clock}
	for _, mod := range mods {
		mod(&cfg)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	e.m = m
	return e
}

func (e *env) bot(name string, mods ...func(*BotInput)) Bot {
	e.t.Helper()
	in := BotInput{Name: name, Instructions: "Be " + name + "."}
	for _, mod := range mods {
		mod(&in)
	}
	b, err := e.m.SaveBot(context.Background(), in)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

func (e *env) send(botID, text string, atts ...string) Message {
	e.t.Helper()
	msg, err := e.m.Send(context.Background(), SendRequest{BotID: botID, Text: text, AttachmentIDs: atts})
	if err != nil {
		e.t.Fatal(err)
	}
	return msg
}

// idle waits until the bot has no active run.
func (e *env) idle(botID string) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.m.mu.Lock()
		_, running := e.m.runs[botID]
		e.m.mu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatal("run did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *env) history(botID string) []Message {
	e.t.Helper()
	page, err := e.m.Messages(context.Background(), MessagesRequest{BotID: botID, Limit: 200})
	if err != nil {
		e.t.Fatal(err)
	}
	return page.Messages
}

func (e *env) lastReply(botID string) Message {
	e.t.Helper()
	h := e.history(botID)
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Role == "assistant" {
			return h[i]
		}
	}
	e.t.Fatal("no assistant reply")
	return Message{}
}

func replyText(m Message) string { return messageText(m.Parts) }

func (e *env) upload(name string, data []byte, chunk int) (Attachment, error) {
	e.t.Helper()
	up, err := e.m.BeginAttachment(AttachmentBegin{Name: name, Size: int64(len(data))})
	if err != nil {
		return Attachment{}, err
	}
	for seq, off := 0, 0; off < len(data); seq++ {
		end := min(off+chunk, len(data))
		if err := e.m.AttachmentChunk(AttachmentChunk{ID: up.ID, Seq: seq, DataBase64: base64.StdEncoding.EncodeToString(data[off:end])}); err != nil {
			return Attachment{}, err
		}
		off = end
	}
	return e.m.CommitAttachment(context.Background(), AttachmentCommit{ID: up.ID})
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeTool is a configurable client tool.
type fakeTool struct {
	id    string
	specs func(Bot, llm.Caps) []llm.ToolSpec
	call  func(ctx context.Context, rc RunContext, name string, args []byte) (ToolResult, error)
	mu    sync.Mutex
	calls []string
}

func (f *fakeTool) ID() string { return f.id }
func (f *fakeTool) Specs(b Bot, c llm.Caps) []llm.ToolSpec {
	if f.specs != nil {
		return f.specs(b, c)
	}
	return []llm.ToolSpec{{Name: f.id, Description: f.id, Schema: []byte(`{"type":"object"}`)}}
}
func (f *fakeTool) Call(ctx context.Context, rc RunContext, name string, args []byte) (ToolResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, string(args))
	f.mu.Unlock()
	if f.call != nil {
		return f.call(ctx, rc, name, args)
	}
	return ToolResult{Text: "ok"}, nil
}
func (f *fakeTool) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// blockingProvider streams one text chunk, then waits for cancellation (or release).
type blockingProvider struct {
	started chan struct{}
	release chan struct{}
	text    string
}

func (p *blockingProvider) ID() string { return "fake" }
func (p *blockingProvider) Models(context.Context) ([]llm.Model, error) {
	return []llm.Model{testModel}, nil
}
func (p *blockingProvider) Validate(context.Context) (llm.Account, error) { return llm.Account{}, nil }
func (p *blockingProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	out := make(chan llm.Delta)
	go func() {
		defer close(out)
		select {
		case out <- llm.Delta{Kind: llm.DeltaText, Text: p.text}:
		case <-ctx.Done():
			return
		}
		select {
		case p.started <- struct{}{}:
		default:
		}
		select {
		case <-p.release:
			select {
			case out <- llm.Delta{Kind: llm.DeltaText, Text: " done"}:
			case <-ctx.Done():
				return
			}
			select {
			case out <- llm.Delta{Kind: llm.DeltaStop, Stop: llm.StopEnd}:
			case <-ctx.Done():
			}
		case <-ctx.Done():
		}
	}()
	return out, nil
}

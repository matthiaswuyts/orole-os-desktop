package automations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/events"
	"github.com/rayorole/orole-os-desktop/migrations"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type toolFunc func(context.Context, ToolRequest) (any, error)

func (f toolFunc) InvokeTool(ctx context.Context, r ToolRequest) (any, error) { return f(ctx, r) }

type notifyFunc func(context.Context, NotificationRequest) (any, error)

func (f notifyFunc) Notify(ctx context.Context, r NotificationRequest) (any, error) { return f(ctx, r) }
func openEngine(t *testing.T, path string, o Options) *Engine {
	t.Helper()
	db, err := database.Open(filepath.ToSlash(path), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	o.DB = db.DB
	o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	e := New(o)
	if err = e.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close(); db.Close() })
	return e
}
func saveDef(t *testing.T, e *Engine, d Definition) Definition {
	t.Helper()
	r, err := e.Save(context.Background(), d)
	if err != nil || !r.Saved {
		t.Fatalf("save %v issues %+v", err, r.Issues)
	}
	return r.Definition
}
func toolStep(id string) Step {
	return Step{ID: id, Type: "mcp.tool", Params: json.RawMessage(`{"server":"fake","tool":"test"}`)}
}
func awaitStatus(t *testing.T, e *Engine, id, status string) Detail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d, err := e.Detail(context.Background(), id)
		if err == nil && d.Run.Status == status {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	d, err := e.Detail(context.Background(), id)
	t.Fatalf("wanted %s: %+v (%v)", status, d, err)
	return Detail{}
}
func runDef(t *testing.T, e *Engine, d Definition) Run {
	t.Helper()
	d = saveDef(t, e, d)
	r, err := e.Run(context.Background(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestApprovalSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	var calls atomic.Int32
	e := openEngine(t, path, Options{})
	e.SetToolInvoker(toolFunc(func(context.Context, ToolRequest) (any, error) { calls.Add(1); return map[string]any{"ok": true}, nil }))
	r := runDef(t, e, Definition{Name: "approval", Steps: []Step{{ID: "approve", Type: "approval", Params: json.RawMessage(`{"message":"Ship it?"}`)}, toolStep("send")}})
	awaitStatus(t, e, r.ID, "waiting-approval")
	if calls.Load() != 0 {
		t.Fatal("approval did not block")
	}
	e.Close()
	e2 := openEngine(t, path, Options{})
	e2.SetToolInvoker(toolFunc(func(context.Context, ToolRequest) (any, error) { calls.Add(1); return true, nil }))
	list, err := e2.Approvals(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("approvals %v %v", list, err)
	}
	if err = e2.Approve(context.Background(), r.ID, "approve", true, "yes"); err != nil {
		t.Fatal(err)
	}
	awaitStatus(t, e2, r.ID, "succeeded")
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
func TestCrashResumeDoesNotRepeatFinishedSideEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	e := openEngine(t, path, Options{})
	e.SetToolInvoker(toolFunc(func(ctx context.Context, r ToolRequest) (any, error) { calls.Add(1); return "completed", nil }))
	e.notifier = notifyFunc(func(context.Context, NotificationRequest) (any, error) { close(entered); <-release; return true, nil })
	r := runDef(t, e, Definition{Name: "crash", Steps: []Step{toolStep("first"), {ID: "second", Type: "notify", Strategy: "repeatable", Params: json.RawMessage(`{"title":"Hi","level":"info"}`)}}})
	<-entered
	// Closing the runner's DB before releasing its in-flight operation simulates
	// death between side effect and journal completion, without graceful cleanup.
	e.db.Close()
	close(release)
	e.Close()
	e2 := openEngine(t, path, Options{Notifier: notifyFunc(func(context.Context, NotificationRequest) (any, error) { return true, nil })})
	e2.SetToolInvoker(toolFunc(func(context.Context, ToolRequest) (any, error) { calls.Add(1); return true, nil }))
	awaitStatus(t, e2, r.ID, "succeeded")
	if calls.Load() != 1 {
		t.Fatalf("finished side effect executed %d times", calls.Load())
	}
}
func TestCrashAtMostOnceBecomesUncertain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	entered := make(chan struct{})
	release := make(chan struct{})
	e := openEngine(t, path, Options{})
	e.SetToolInvoker(toolFunc(func(context.Context, ToolRequest) (any, error) { close(entered); <-release; return true, nil }))
	r := runDef(t, e, Definition{Name: "uncertain", Steps: []Step{toolStep("write")}})
	<-entered
	e.db.Close()
	close(release)
	e.Close()
	e2 := openEngine(t, path, Options{})
	d := awaitStatus(t, e2, r.ID, "uncertain")
	if d.Steps[0].Status != "uncertain" {
		t.Fatal(d)
	}
}
func TestRetriesTimeoutCancellation(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
		var calls atomic.Int32
		e.SetToolInvoker(toolFunc(func(context.Context, ToolRequest) (any, error) {
			if calls.Add(1) < 3 {
				return nil, fmt.Errorf("temporary")
			}
			return true, nil
		}))
		s := toolStep("write")
		s.Strategy = "repeatable"
		s.Retries = 2
		s.BackoffMs = 1
		r := runDef(t, e, Definition{Name: "retry", Steps: []Step{s}})
		d := awaitStatus(t, e, r.ID, "succeeded")
		if d.Steps[0].Attempt != 3 {
			t.Fatal(d)
		}
	})
	for _, mode := range []string{"timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
			entered := make(chan struct{})
			e.SetToolInvoker(toolFunc(func(ctx context.Context, _ ToolRequest) (any, error) {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			}))
			s := toolStep("wait")
			s.TimeoutSec = 1
			r := runDef(t, e, Definition{Name: mode, Steps: []Step{s}})
			<-entered
			status := "failed"
			if mode == "cancel" {
				status = "canceled"
				if err := e.Cancel(context.Background(), r.ID); err != nil {
					t.Fatal(err)
				}
			}
			awaitStatus(t, e, r.ID, status)
		})
	}
}
func TestConcurrencyAndRecursion(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	cfg := e.Config()
	cfg.GlobalConcurrency = 1
	cfg.PerAutomation = 1
	if _, err := e.SetConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	entered := make(chan struct{}, 3)
	var active, max atomic.Int32
	e.SetToolInvoker(toolFunc(func(ctx context.Context, _ ToolRequest) (any, error) {
		n := active.Add(1)
		if n > max.Load() {
			max.Store(n)
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		active.Add(-1)
		return true, nil
	}))
	d := saveDef(t, e, Definition{Name: "limited", Steps: []Step{toolStep("work")}})
	r1, _ := e.Run(context.Background(), d.ID)
	<-entered
	r2, _ := e.Run(context.Background(), d.ID)
	if r, err := e.loadRun(context.Background(), r2.ID); err != nil || r.Status != "queued" {
		t.Fatalf("not queued %+v %v", r, err)
	}
	if _, err := e.trigger(context.Background(), d.ID, "manual", nil, []string{d.ID}, ""); err == nil {
		t.Fatal("self recursion accepted")
	}
	if _, err := e.trigger(context.Background(), d.ID, "manual", nil, []string{"a", "b", "c"}, ""); err == nil {
		t.Fatal("depth >3 accepted")
	}
	close(release)
	awaitStatus(t, e, r1.ID, "succeeded")
	awaitStatus(t, e, r2.ID, "succeeded")
	if max.Load() != 1 {
		t.Fatal(max.Load())
	}
}

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []chan time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) After(time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, ch)
	return ch
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	for _, ch := range c.waiters {
		ch <- c.now
	}
	c.waiters = nil
	c.mu.Unlock()
}
func TestScheduleInjectedClockCatchUp(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)}
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{Clock: clock, Notifier: notifyFunc(func(context.Context, NotificationRequest) (any, error) { return true, nil })})
	d := saveDef(t, e, Definition{Name: "schedule", Enabled: true, Trigger: Trigger{Type: "schedule", Schedule: "every 15m", CatchUp: "run-once"}, Steps: []Step{{ID: "notify", Type: "notify", Params: json.RawMessage(`{"title":"Tick","level":"info"}`)}}})
	clock.advance(15 * time.Minute)
	e.wake()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ := e.Runs(context.Background(), d.ID)
		if len(runs) == 1 {
			awaitStatus(t, e, runs[0].ID, "succeeded")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	runs, _ := e.Runs(context.Background(), d.ID)
	if len(runs) != 1 {
		t.Fatal(len(runs))
	}
	clock.advance(24 * time.Hour)
	e.wake()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ = e.Runs(context.Background(), d.ID)
		if len(runs) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(runs) != 2 {
		t.Fatalf("catchup produced %d runs", len(runs))
	}
}

type memoryCredentials struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memoryCredentials) Get(k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return append([]byte{}, v...), nil
}
func (s *memoryCredentials) Set(k string, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = append([]byte{}, b...)
	return nil
}
func (s *memoryCredentials) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}
func TestWebhookHMAC(t *testing.T) {
	store := &memoryCredentials{m: map[string][]byte{webhookCredential: []byte(strings.Repeat("s", 32))}}
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{Credentials: store, Notifier: notifyFunc(func(context.Context, NotificationRequest) (any, error) { return true, nil })})
	d := saveDef(t, e, Definition{Name: "webhook", Enabled: true, Trigger: Trigger{Type: "webhook"}, Steps: []Step{{ID: "notify", Type: "notify", Params: json.RawMessage(`{"title":"Hook","level":"info"}`)}}})
	body := `{"value":"data"}`
	stamp := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(strings.Repeat("s", 32)))
	mac.Write([]byte(stamp + "." + body))
	signature := hex.EncodeToString(mac.Sum(nil))
	for _, sig := range []string{"bad", signature, signature} {
		r := httptest.NewRequest("POST", "/automations/"+d.ID, strings.NewReader(body))
		r.Header.Set("X-Orole-Timestamp", stamp)
		r.Header.Set("X-Orole-Signature", sig)
		w := httptest.NewRecorder()
		e.WebhookHandler().ServeHTTP(w, r)
		want := 202
		if sig == "bad" {
			want = 401
		}
		if w.Code != want {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	runs, _ := e.Runs(context.Background(), d.ID)
	if len(runs) != 1 {
		t.Fatalf("replay created %d runs", len(runs))
	}
}
func TestRedactionAndEventFilter(t *testing.T) {
	bus := events.NewBus()
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{Bus: bus})
	e.RegisterSecret("topsecret")
	e.SetToolInvoker(toolFunc(func(context.Context, ToolRequest) (any, error) {
		return map[string]any{"value": "topsecret Bearer abc.def", "token": "hidden"}, nil
	}))
	r := runDef(t, e, Definition{Name: "redact", Steps: []Step{toolStep("read")}})
	d := awaitStatus(t, e, r.ID, "succeeded")
	if strings.Contains(string(d.Steps[0].Output), "topsecret") || strings.Contains(string(d.Steps[0].Output), "abc.def") {
		t.Fatal(string(d.Steps[0].Output))
	}
	a := saveDef(t, e, Definition{Name: "event", Enabled: true, Trigger: Trigger{Type: "event", Event: "github.*", Filter: "event.data.conclusion == 'failure'"}, Steps: []Step{toolStep("call")}})
	e.event(events.Event{Topic: "github.ci.completed", Data: map[string]any{"conclusion": "success"}})
	runs, _ := e.Runs(context.Background(), a.ID)
	if len(runs) != 0 {
		t.Fatal("filter ignored")
	}
	e.event(events.Event{Topic: "github.ci.completed", Data: map[string]any{"conclusion": "failure"}})
	runs, _ = e.Runs(context.Background(), a.ID)
	if len(runs) != 1 {
		t.Fatal("event did not trigger")
	}
}

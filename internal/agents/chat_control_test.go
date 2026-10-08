package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/migrations"
)

type chatTestProcess struct {
	mu          sync.Mutex
	sent        []string
	answers     int
	permissions int
	sendFn      func(string) error
	answerFn    func(string, []QuestionAnswer, bool) error
	stopFn      func() error
}

func (*chatTestProcess) Pid() int             { return 1 }
func (*chatTestProcess) TerminalID() string   { return "" }
func (*chatTestProcess) Events() <-chan Event { return nil }
func (p *chatTestProcess) Send(text string) error {
	p.mu.Lock()
	p.sent = append(p.sent, text)
	fn := p.sendFn
	p.mu.Unlock()
	if fn != nil {
		return fn(text)
	}
	return nil
}
func (p *chatTestProcess) Respond(string, bool, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.permissions++
	return nil
}
func (p *chatTestProcess) AnswerInput(id string, answers []QuestionAnswer, cancel bool) error {
	p.mu.Lock()
	p.answers++
	fn := p.answerFn
	p.mu.Unlock()
	if fn != nil {
		return fn(id, answers, cancel)
	}
	return nil
}
func (p *chatTestProcess) Stop() error {
	if p.stopFn != nil {
		return p.stopFn()
	}
	return nil
}
func (p *chatTestProcess) prompts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.sent...)
}

func chatFixture(t *testing.T, status Status) (*Manager, *liveSess, *chatTestProcess) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "chat.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(Config{Store: NewStore(db), Authorize: func(context.Context, string, string, string) error { return nil }})
	p := &chatTestProcess{}
	now := time.Now().UTC()
	s := Session{ID: "chat", Kind: "claude", Machine: "local", Mode: ModeStructured, Status: status, StartedAt: now, UpdatedAt: now}
	ls := &liveSess{id: s.ID, s: s, proc: p, done: make(chan struct{})}
	m.live[s.ID] = ls
	if err := m.st.SaveSession(s, ""); err != nil {
		t.Fatal(err)
	}
	p.stopFn = func() error {
		m.finalize(ls, Event{Kind: EvExit, Data: jraw(map[string]any{"code": 0, "stopped": true})})
		close(ls.done)
		return nil
	}
	t.Cleanup(func() {
		m.mu.Lock()
		m.closing = true
		m.mu.Unlock()
		m.bgCancel()
		m.bgWork.Wait()
		db.Close()
	})
	return m, ls, p
}

func chatWait(t *testing.T, m *Manager, pred func(Session) bool) Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := m.WaitFor(ctx, "chat", pred)
	if err != nil {
		t.Fatalf("waiting for chat state: %+v: %v", s, err)
	}
	return s
}

func chatQuestion(id string) QuestionRequest {
	return QuestionRequest{ID: id, At: time.Now().UTC(), Questions: []Question{
		{ID: "choice", Prompt: "Choose", Options: []QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}},
		{ID: "text", Prompt: "Explain", AllowText: true},
	}}
}

func chatAnswers() []QuestionAnswer {
	return []QuestionAnswer{{QuestionID: "choice", Values: []string{"a"}}, {QuestionID: "text", Values: []string{"because"}}}
}

func TestChatQueueFIFOAndCancellation(t *testing.T) {
	m, _, p := chatFixture(t, StatusWorking)
	ctx := context.Background()
	for _, text := range []string{"first", "remove", "third"} {
		if _, err := m.QueuePrompt(ctx, "chat", text); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := m.Get("chat")
	if len(p.prompts()) != 0 || len(s.Queue) != 3 {
		t.Fatal("busy queue dispatched")
	}
	if _, err := m.CancelQueuedPrompt(ctx, "chat", s.Queue[1].ID); err != nil {
		t.Fatal(err)
	}
	p.sendFn = func(text string) error {
		stored, err := m.st.GetSession("chat")
		if err != nil {
			return err
		}
		if len(stored.Queue) == 0 || stored.Queue[0].State != queueSending || stored.Queue[0].Text != text {
			return errors.New("sending marker was not durable before dispatch")
		}
		return nil
	}
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	chatWait(t, m, func(s Session) bool { return s.Prompts == 1 && len(s.Queue) == 1 })
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	chatWait(t, m, func(s Session) bool { return s.Prompts == 2 && len(s.Queue) == 0 })
	if got := strings.Join(p.prompts(), ","); got != "first,third" {
		t.Fatal(got)
	}
}

func TestChatQueueFailedTurnAndStopPause(t *testing.T) {
	m, _, p := chatFixture(t, StatusWorking)
	ctx := context.Background()
	if _, err := m.QueuePrompt(ctx, "chat", "next"); err != nil {
		t.Fatal(err)
	}
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": false})})
	s, _ := m.Get("chat")
	if len(p.prompts()) != 0 || s.Queue[0].State != queuePaused {
		t.Fatalf("not paused: %+v", s)
	}
	if _, err := m.ResumeQueue(ctx, "chat"); err != nil {
		t.Fatal(err)
	}
	if len(p.prompts()) != 1 {
		t.Fatal("explicit resume did not send")
	}
	if _, err := m.QueuePrompt(ctx, "chat", "after stop"); err != nil {
		t.Fatal(err)
	}
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(chatQuestion("pending"))})
	s, err := m.Stop(ctx, "chat")
	if err != nil {
		t.Fatal(err)
	}
	if s.Live || len(s.Questions) != 0 || len(s.Pending) != 0 || len(s.Queue) != 1 || s.Queue[0].State != queuePaused {
		t.Fatalf("stop state: %+v", s)
	}
	if _, err := m.ResumeQueue(ctx, "chat"); err == nil {
		t.Fatal("queue relaunched a stopped process")
	}
	if _, err := m.QueuePrompt(ctx, "chat", "durable only"); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Get("chat")
	if s.Live || len(s.Queue) != 2 || s.Queue[1].State != queuePaused || len(p.prompts()) != 1 {
		t.Fatal("nonlive queue launched")
	}
}

func TestChatRecoveryNeverReplaysQueue(t *testing.T) {
	m, _, p := chatFixture(t, StatusDone)
	m.mu.Lock()
	delete(m.live, "chat")
	m.mu.Unlock()
	s, _ := m.st.GetSession("chat")
	s.Queue = []QueuedPrompt{{ID: "in-flight", Text: "maybe sent", State: queueSending}, {ID: "next", Text: "never sent", State: queueQueued}}
	s.Questions = []QuestionRequest{chatQuestion("runtime")}
	s.Pending = []PermissionRequest{{ID: "runtime-permission"}}
	if err := m.st.SaveSession(s, ""); err != nil {
		t.Fatal(err)
	}
	stored, _ := m.st.GetSession("chat")
	if len(stored.Questions) != 0 || len(stored.Pending) != 0 {
		t.Fatal("runtime requests persisted")
	}
	if lost, err := m.Recover(); err != nil || lost != 0 {
		t.Fatalf("recover: %d %v", lost, err)
	}
	stored, _ = m.st.GetSession("chat")
	if stored.Queue[0].State != queueUncertain || stored.Queue[1].State != queuePaused || len(p.prompts()) != 0 {
		t.Fatalf("recovery replayed or unpaused: %+v", stored.Queue)
	}
	if _, err := m.Recover(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResumeQueue(context.Background(), "chat"); err == nil {
		t.Fatal("recovery restarted process")
	}
}

func TestChatUncertainDeliveryRequiresRemovalAndExplicitResume(t *testing.T) {
	m, _, p := chatFixture(t, StatusWorking)
	ctx := context.Background()
	for _, text := range []string{"maybe", "next"} {
		if _, err := m.QueuePrompt(ctx, "chat", text); err != nil {
			t.Fatal(err)
		}
	}
	p.sendFn = func(string) error { return errors.New("partial pipe write") }
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	s := chatWait(t, m, func(s Session) bool { return s.Queue[0].State == queueUncertain })
	if s.Queue[1].State != queuePaused {
		t.Fatal("remaining queue did not pause")
	}
	if _, err := m.ResumeQueue(ctx, "chat"); err == nil {
		t.Fatal("uncertain entry resent")
	}
	// A late successful result proves that the process is now idle, not that a
	// retry of the old queue entry is safe.
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	if _, err := m.ResumeQueue(ctx, "chat"); err == nil {
		t.Fatal("late completion allowed uncertain replay")
	}
	if _, err := m.CancelQueuedPrompt(ctx, "chat", s.Queue[0].ID); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.sendFn = nil
	p.mu.Unlock()
	if _, err := m.ResumeQueue(ctx, "chat"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.prompts(), ","); got != "maybe,next" {
		t.Fatal(got)
	}
}

func TestChatConcurrentPromptsQueueInsteadOfSteering(t *testing.T) {
	m, _, p := chatFixture(t, StatusIdle)
	entered, release := make(chan struct{}), make(chan struct{})
	p.sendFn = func(string) error { close(entered); <-release; return nil }
	first := make(chan error, 1)
	go func() { first <- m.Prompt(context.Background(), "chat", "first") }()
	<-entered
	second := make(chan error, 1)
	go func() { second <- m.Prompt(context.Background(), "chat", "second") }()
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	s, _ := m.Get("chat")
	if got := strings.Join(p.prompts(), ","); got != "first" || len(s.Queue) != 1 || s.Queue[0].Text != "second" {
		t.Fatalf("steered or lost concurrent prompt: %s %+v", got, s.Queue)
	}
}

func TestChatQuestionValidationAndOneShotResponse(t *testing.T) {
	m, _, p := chatFixture(t, StatusWorking)
	req := chatQuestion("request")
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(req)})
	for name, answers := range map[string][]QuestionAnswer{
		"missing":            {chatAnswers()[0]},
		"duplicate question": {chatAnswers()[0], chatAnswers()[0]},
		"unknown question":   {{QuestionID: "other", Values: []string{"a"}}, chatAnswers()[1]},
		"unknown option":     {{QuestionID: "choice", Values: []string{"not-a-choice"}}, chatAnswers()[1]},
		"multiple forbidden": {{QuestionID: "choice", Values: []string{"a", "b"}}, chatAnswers()[1]},
		"oversized":          {chatAnswers()[0], {QuestionID: "text", Values: []string{strings.Repeat("x", 8193)}}},
		"nul":                {chatAnswers()[0], {QuestionID: "text", Values: []string{"bad\x00text"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := m.AnswerInput(context.Background(), "chat", req.ID, answers, false); err == nil {
				t.Fatal("invalid answers accepted")
			}
		})
	}
	if err := m.Approve(context.Background(), "chat", req.ID, true, ""); err == nil {
		t.Fatal("question accepted as permission")
	}
	if _, err := m.AnswerInput(context.Background(), "chat", req.ID, chatAnswers(), true); err == nil {
		t.Fatal("cancel accepted answers")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.AnswerInput(context.Background(), "chat", req.ID, chatAnswers(), false)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 || p.answers != 1 || p.permissions != 0 {
		t.Fatalf("not one-shot: %d answers=%d approvals=%d", success, p.answers, p.permissions)
	}
	s, _ := m.Get("chat")
	if len(s.Questions) != 0 {
		t.Fatal("question not consumed")
	}
}

func TestChatExpiredCanceledAndUncertainQuestions(t *testing.T) {
	m, _, p := chatFixture(t, StatusWorking)
	ctx := context.Background()
	expired := chatQuestion("expired")
	past := time.Now().Add(-time.Second)
	expired.ExpiresAt = &past
	if err := validateAnswers(expired, chatAnswers(), false, time.Now()); err == nil {
		t.Fatal("expired answers accepted")
	}
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(chatQuestion("withdrawn"))})
	m.apply("chat", Event{Kind: EvQuestionCancel, Data: jraw(map[string]string{"id": "withdrawn"})})
	if _, err := m.AnswerInput(ctx, "chat", "withdrawn", chatAnswers(), false); err == nil {
		t.Fatal("withdrawn question accepted")
	}
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(chatQuestion("cancel"))})
	if _, err := m.AnswerInput(ctx, "chat", "cancel", nil, true); err != nil {
		t.Fatal(err)
	}
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(chatQuestion("uncertain"))})
	if _, err := m.QueuePrompt(ctx, "chat", "later"); err != nil {
		t.Fatal(err)
	}
	p.answerFn = func(string, []QuestionAnswer, bool) error { return errors.New("uncertain response write") }
	if _, err := m.AnswerInput(ctx, "chat", "uncertain", chatAnswers(), false); err == nil {
		t.Fatal("response write error hidden")
	}
	if _, err := m.AnswerInput(ctx, "chat", "uncertain", chatAnswers(), false); err == nil {
		t.Fatal("uncertain response replayed")
	}
	s, _ := m.Get("chat")
	if len(s.Questions) != 0 || s.Queue[0].State != queuePaused {
		t.Fatalf("response failure state: %+v", s)
	}
}

func TestChatPermissionWithdrawalDuringPolicyCheck(t *testing.T) {
	m, _, p := chatFixture(t, StatusWorking)
	entered, release := make(chan struct{}), make(chan struct{})
	m.cfg.PermissionDecision = func(context.Context, string, PermissionRequest) error { close(entered); <-release; return nil }
	m.apply("chat", Event{Kind: EvPermission, Data: jraw(PermissionRequest{ID: "permission", Tool: "Bash", At: time.Now()})})
	result := make(chan error, 1)
	go func() { result <- m.Approve(context.Background(), "chat", "permission", true, "") }()
	<-entered
	m.apply("chat", Event{Kind: EvPermCancel, Data: jraw(map[string]string{"id": "permission"})})
	close(release)
	if err := <-result; err == nil {
		t.Fatal("withdrawn permission dispatched")
	}
	if p.permissions != 0 {
		t.Fatal("permission response reached process")
	}
}

func TestChatUsageSnapshotsAndReadableEvents(t *testing.T) {
	m, _, _ := chatFixture(t, StatusWorking)
	contextTokens, window, output := int64(125), int64(1000), int64(20)
	m.apply("chat", Event{Kind: EvUsage, Data: jraw(ContextUsage{Source: "provider", ContextTokens: &contextTokens, ContextWindow: &window})})
	m.apply("chat", Event{Kind: EvUsage, Data: jraw(ContextUsage{Source: "provider", OutputTokens: &output})})
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(chatQuestion("immutable"))})
	s, _ := m.Get("chat")
	if s.Usage.ContextTokens == nil || *s.Usage.ContextTokens != contextTokens || *s.Usage.OutputTokens != output {
		t.Fatal("partial usage erased previous observation")
	}
	*s.Usage.ContextTokens = 999
	s.Questions[0].Questions[0].Options[0].ID = "changed"
	fresh, _ := m.Get("chat")
	if *fresh.Usage.ContextTokens != contextTokens || fresh.Questions[0].Questions[0].Options[0].ID != "a" {
		t.Fatal("snapshot aliases manager state")
	}
	for _, kind := range []string{EvSources, EvFile, EvImageStatus, EvSubagent, EvGuardrail} {
		m.apply("chat", Event{Kind: kind, Data: jraw(map[string]string{"id": kind})})
	}
	transcript, err := m.Transcript("chat", 100)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, e := range transcript {
		kinds[e.Kind] = true
	}
	for _, kind := range []string{EvUsage, EvQuestion, EvSources, EvFile, EvImageStatus, EvSubagent, EvGuardrail} {
		if !kinds[kind] {
			t.Fatalf("missing readable event %s", kind)
		}
	}
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	s, _ = m.Get("chat")
	if len(s.Questions) != 0 {
		t.Fatal("turn completion retained pending input")
	}
}

type chatFailDB struct {
	DB
	fail func(Session) bool
}

func (db chatFailDB) Exec(query string, args ...any) (sql.Result, error) {
	if strings.HasPrefix(query, "INSERT INTO agent_sessions") {
		var s Session
		if json.Unmarshal([]byte(args[13].(string)), &s) == nil && db.fail(s) {
			return nil, errors.New("injected persistence failure")
		}
	}
	return db.DB.Exec(query, args...)
}

func TestChatQueuePersistenceBeforeAndAfterDispatch(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[after], func(t *testing.T) {
			m, _, p := chatFixture(t, StatusIdle)
			base := m.st.db
			m.st.db = chatFailDB{DB: base, fail: func(s Session) bool {
				if after {
					return s.Prompts > 0 && len(s.Queue) == 0
				}
				return len(s.Queue) > 0 && s.Queue[0].State == queueSending
			}}
			if _, err := m.QueuePrompt(context.Background(), "chat", "durable"); err == nil {
				t.Fatal("persistence failure hidden")
			}
			want := 0
			if after {
				want = 1
			}
			if len(p.prompts()) != want {
				t.Fatalf("dispatch count %d want %d", len(p.prompts()), want)
			}
			m.st.db = base
			if after {
				s, _ := m.Get("chat")
				if s.Queue[0].State != queueUncertain {
					t.Fatal("accepted but uncommitted delivery was not uncertain")
				}
				stored, _ := m.st.GetSession("chat")
				if stored.Queue[0].State != queueSending {
					t.Fatal("durable sending receipt lost")
				}
			}
		})
	}
}

func TestChatQueueAuthorizationAndPendingRequestGates(t *testing.T) {
	m, _, p := chatFixture(t, StatusWorking)
	ctx := context.Background()
	m.cfg.Authorize = func(context.Context, string, string, string) error { return errors.New("denied") }
	if _, err := m.QueuePrompt(ctx, "chat", "not authorized"); err == nil {
		t.Fatal("queue bypassed authorization")
	}
	if _, err := m.ResumeQueue(ctx, "chat"); err == nil {
		t.Fatal("resume bypassed authorization")
	}
	s, _ := m.Get("chat")
	if len(s.Queue) != 0 || len(p.prompts()) != 0 {
		t.Fatal("denied task retained or dispatched")
	}
	m.cfg.Authorize = func(context.Context, string, string, string) error { return nil }
	m.apply("chat", Event{Kind: EvPermission, Data: jraw(PermissionRequest{ID: "permission", Tool: "Bash"})})
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(chatQuestion("question"))})
	if _, err := m.QueuePrompt(ctx, "chat", "after requests"); err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(ctx, "chat", "must not bypass"); err == nil {
		t.Fatal("direct prompt bypassed requests")
	}
	if _, err := m.ResumeQueue(ctx, "chat"); err == nil {
		t.Fatal("queue resumed past requests")
	}
	if _, err := m.AnswerInput(ctx, "chat", "permission", chatAnswers(), false); err == nil {
		t.Fatal("question API accepted permission id")
	}
	if err := m.Approve(ctx, "chat", "permission", true, ""); err != nil {
		t.Fatal(err)
	}
	s, _ = m.Get("chat")
	if s.Status != StatusNeedsYou || len(s.Questions) != 1 {
		t.Fatal("approval hid pending question")
	}
	if _, err := m.AnswerInput(ctx, "chat", "question", chatAnswers(), false); err != nil {
		t.Fatal(err)
	}
	if len(p.prompts()) != 0 {
		t.Fatal("requests being answered dispatched a follow-up mid-turn")
	}
	m.apply("chat", Event{Kind: EvTurnDone, Data: jraw(map[string]bool{"ok": true})})
	chatWait(t, m, func(s Session) bool { return s.Prompts == 1 && len(s.Queue) == 0 })
}

func TestChatSecretAnswerAuditAndUnsupportedResponder(t *testing.T) {
	m, ls, p := chatFixture(t, StatusWorking)
	ctx := context.Background()
	req := chatQuestion("secret")
	req.Questions[1].Secret = true
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(req)})
	// An interface wrapper intentionally exposes only Process, not InputResponder.
	m.mu.Lock()
	ls.proc = struct{ Process }{p}
	m.mu.Unlock()
	if _, err := m.AnswerInput(ctx, "chat", req.ID, chatAnswers(), false); err == nil {
		t.Fatal("unsupported responder silently accepted")
	}
	m.mu.Lock()
	ls.proc = p
	m.mu.Unlock()
	audits := []string{}
	m.cfg.Audit = func(action, _, outcome, detail string) { audits = append(audits, action+" "+outcome+" "+detail) }
	p.answerFn = func(_ string, answers []QuestionAnswer, _ bool) error {
		if answers[1].Values[0] != "because" {
			return errors.New("secret did not reach harness")
		}
		return nil
	}
	if _, err := m.AnswerInput(ctx, "chat", req.ID, chatAnswers(), false); err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || !strings.HasPrefix(audits[0], "agent.input answered") || strings.Contains(audits[0], "because") {
		t.Fatalf("input audit: %v", audits)
	}
	evs, err := m.Transcript("chat", 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Kind == EvAnswer {
			found = true
			if strings.Contains(string(e.Data), "because") || !strings.Contains(string(e.Data), "[redacted]") {
				t.Fatal("secret persisted in transcript")
			}
		}
	}
	if !found {
		t.Fatal("answer event missing")
	}
}

func TestChatQuestionShapesAndDuplicateRequests(t *testing.T) {
	now := time.Now().UTC()
	req := chatQuestion("multiple")
	req.Questions[0].Multiple = true
	answers := chatAnswers()
	answers[0].Values = []string{"a", "b"}
	if err := validateAnswers(req, answers, false, now); err != nil {
		t.Fatal(err)
	}
	answers[0].Values = []string{"a", "a"}
	if err := validateAnswers(req, answers, false, now); err == nil {
		t.Fatal("duplicate option accepted")
	}
	req.Questions[1].ID = req.Questions[0].ID
	if err := validateQuestionRequest(req); err == nil {
		t.Fatal("duplicate question IDs accepted")
	}
	req = chatQuestion("duplicate options")
	req.Questions[0].Options[1].ID = req.Questions[0].Options[0].ID
	if err := validateQuestionRequest(req); err == nil {
		t.Fatal("duplicate option IDs accepted")
	}
	m, _, p := chatFixture(t, StatusWorking)
	req = chatQuestion("duplicate request")
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(req)})
	m.apply("chat", Event{Kind: EvQuestion, Data: jraw(req)})
	if _, err := m.AnswerInput(context.Background(), "chat", req.ID, chatAnswers(), false); err == nil {
		t.Fatal("duplicate request retained stale authority")
	}
	if p.answers != 0 {
		t.Fatal("duplicate reached harness")
	}
}

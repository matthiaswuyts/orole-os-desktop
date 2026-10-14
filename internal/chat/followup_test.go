package chat

import (
	"context"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
)

// followUps records the follow-ups the hook was told about and answers with live.
type followUps struct {
	mu   sync.Mutex
	got  []FollowUp
	live bool
}

func (f *followUps) hook(fu FollowUp) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, fu)
	return f.live
}

func (f *followUps) all() []FollowUp {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FollowUp(nil), f.got...)
}

// chatEvents lists the chat timeline events (chat.message, chat.message.delta, chat.run) published for botID, and
// counts its call thread events (chat.call.message, chat.call.delta).
func (e *env) chatEvents(botID string) (chat []string, call int) {
	for _, r := range e.rec.all() {
		var bot string
		switch p := r.payload.(type) {
		case Message:
			bot = p.BotID
		case MessageDelta:
			bot = p.BotID
		case RunState:
			bot = p.BotID
		}
		if bot != botID {
			continue
		}
		switch r.event {
		case "chat.message", "chat.message.delta", "chat.run":
			chat = append(chat, r.event)
		case "chat.call.message", "chat.call.delta":
			call++
		}
	}
	return chat, call
}

func delegationScript(call int, req llm.Request) []llm.Delta {
	if strings.Contains(req.System, "Be Callee.") {
		return []llm.Delta{llmtest.Text("callee says 42")}
	}
	return []llm.Delta{llmtest.Text("caller relays it")}
}

// A task handed off on a phone call reports into that call's thread: the report and the caller's written answer carry
// the call id and are published as call events only, so nothing of it reaches the caller's chat. The callee's own
// thread is its work and stays in its chat.
func TestHandoffOnACallReportsIntoTheCallThread(t *testing.T) {
	e := newEnv(t, delegationScript)
	fu := &followUps{}
	e.m.SetFollowUpHook(fu.hook)
	callee := e.bot("Callee")
	caller := e.bot("Caller", executive)
	from := fromBot(caller)
	from.CallID = "call-1"

	if err := e.m.Handoff(context.Background(), from, callee.ID, "what is the answer?"); err != nil {
		t.Fatal(err)
	}
	e.until("the report to be answered in the call thread", func() bool {
		msgs := e.callMessages("call-1")
		return len(msgs) == 2 && msgs[1].Status == StatusComplete
	})
	e.idle(caller.ID)
	e.idle(callee.ID)

	msgs := e.callMessages("call-1")
	r, a := msgs[0], msgs[1]
	if r.BotID != caller.ID || r.Role != "user" || r.Author != ReportPrefix+callee.ID || r.CallID != "call-1" || messageText(r.Parts) != "callee says 42" {
		t.Fatalf("report: %+v", r)
	}
	if a.BotID != caller.ID || a.Role != "assistant" || a.CallID != "call-1" || replyText(a) != "caller relays it" {
		t.Fatalf("answer: %+v", a)
	}
	if h := e.history(caller.ID); len(h) != 0 {
		t.Fatalf("the call's follow-up reached the caller's chat: %+v", h)
	}
	if h := e.history(callee.ID); len(h) != 2 || h[0].CallID != "" || h[1].CallID != "" || replyText(h[1]) != "callee says 42" {
		t.Fatalf("callee conversation: %+v", h)
	}
	if chat, call := e.chatEvents(caller.ID); len(chat) != 0 || call == 0 {
		t.Fatalf("caller events: chat %v, %d call events", chat, call)
	}
	if bots, err := e.m.Bots(context.Background()); err != nil {
		t.Fatal(err)
	} else {
		for _, b := range bots {
			if b.ID == caller.ID && (b.LastMessage != nil || b.Running) {
				t.Fatalf("the caller's chat preview shows the call follow-up: %+v", b)
			}
		}
	}
	var answered llm.Request
	for _, c := range e.fake.Calls() {
		if !strings.Contains(c.System, "Be Callee.") {
			answered = c
		}
	}
	if !strings.Contains(answered.System, "reported back") {
		t.Fatalf("the answering turn needs the report instructions: %q", answered.System)
	}
	want := FollowUp{BotID: caller.ID, CallID: "call-1", Kind: FollowUpReport, From: "Callee", Status: "complete", Text: "callee says 42"}
	if got := fu.all(); len(got) != 1 || got[0] != want {
		t.Fatalf("follow-ups %+v, want %+v", got, want)
	}
}

// A report the hook handed to the live call it came from is stored in the call thread but gets no written answer.
// A report of a task handed off in chat is always answered in chat, whatever the hook says.
func TestFollowUpSpokenOnTheLiveCallIsOnlyStored(t *testing.T) {
	e := newEnv(t, delegationScript)
	fu := &followUps{live: true}
	e.m.SetFollowUpHook(fu.hook)
	callee := e.bot("Callee")
	caller := e.bot("Caller", executive)
	from := fromBot(caller)
	from.CallID = "call-2"

	if err := e.m.Handoff(context.Background(), from, callee.ID, "look it up"); err != nil {
		t.Fatal(err)
	}
	e.until("the report in the call thread", func() bool { return len(e.callMessages("call-2")) == 1 })
	// A chat handoff now takes the caller's bot lock after the report was stored: an answering turn queued with the
	// report would run (and land in the call thread) before this one is answered.
	if err := e.m.Handoff(context.Background(), fromBot(caller), callee.ID, "and this one"); err != nil {
		t.Fatal(err)
	}
	e.until("the chat report to be answered", func() bool {
		h := e.history(caller.ID)
		return len(h) == 2 && h[1].Status == StatusComplete
	})
	e.idle(caller.ID)
	e.idle(callee.ID)

	msgs := e.callMessages("call-2")
	if len(msgs) != 1 || msgs[0].Author != ReportPrefix+callee.ID || msgs[0].Status != StatusComplete || msgs[0].CallID != "call-2" {
		t.Fatalf("call thread: %+v", msgs)
	}
	h := e.history(caller.ID)
	if h[0].Author != ReportPrefix+callee.ID || h[0].CallID != "" || h[1].Role != "assistant" || replyText(h[1]) != "caller relays it" {
		t.Fatalf("a chat report must be answered in chat: %+v", h)
	}
	if n := len(e.fake.Calls()); n != 3 {
		t.Fatalf("%d model calls, want 3 (two callee tasks and the chat report's answer)", n)
	}
	got := fu.all()
	if len(got) != 2 || got[0].CallID != "call-2" || got[1].CallID != "" || got[1].Kind != FollowUpReport {
		t.Fatalf("follow-ups: %+v", got)
	}
}

// A code task started on a call reports into the call thread and the bot answers it there; the hook hears about it
// as a task, with the text clipped.
func TestTaskReportOnACallGoesToTheCallThread(t *testing.T) {
	e := newEnv(t, func(int, llm.Request) []llm.Delta { return []llm.Delta{llmtest.Text("Your login fix is done.")} })
	fu := &followUps{}
	e.m.SetFollowUpHook(fu.hook)
	b := e.bot("Coder")
	text := "Task finished. " + strings.Repeat("é", 2500)
	if err := e.m.ReportTask(context.Background(), TaskReport{BotID: b.ID, TaskID: "task-1", Title: "Fix login", Status: "finished", Text: text, CallID: "call-3"}); err != nil {
		t.Fatal(err)
	}
	e.until("the task report to be answered in the call thread", func() bool {
		msgs := e.callMessages("call-3")
		return len(msgs) == 2 && msgs[1].Status == StatusComplete
	})
	e.idle(b.ID)
	msgs := e.callMessages("call-3")
	if msgs[0].Author != TaskPrefix+"task-1" || msgs[0].CallID != "call-3" || messageText(msgs[0].Parts) != text || replyText(msgs[1]) != "Your login fix is done." || msgs[1].CallID != "call-3" {
		t.Fatalf("call thread: %+v", msgs)
	}
	if h := e.history(b.ID); len(h) != 0 {
		t.Fatalf("the task report reached the chat: %+v", h)
	}
	if chat, call := e.chatEvents(b.ID); len(chat) != 0 || call == 0 {
		t.Fatalf("events: chat %v, %d call events", chat, call)
	}
	got := fu.all()
	if len(got) != 1 {
		t.Fatalf("follow-ups: %+v", got)
	}
	f := got[0]
	if f.BotID != b.ID || f.CallID != "call-3" || f.Kind != FollowUpTask || f.From != "Fix login" || f.Status != "finished" {
		t.Fatalf("follow-up: %+v", f)
	}
	if n := utf8.RuneCountInString(f.Text); n != MaxFollowUpText || !strings.HasPrefix(f.Text, "Task finished. ") || !strings.HasSuffix(f.Text, "…") {
		t.Fatalf("follow-up text: %d runes", n)
	}

	// Spoken on the live call: stored in the call thread, not answered.
	fu.mu.Lock()
	fu.live = true
	fu.mu.Unlock()
	if err := e.m.ReportTask(context.Background(), TaskReport{BotID: b.ID, TaskID: "task-2", Title: "Fix logout", Status: "failed", Text: "it broke", CallID: "call-3"}); err != nil {
		t.Fatal(err)
	}
	e.send(b.ID, "hi") // queued behind anything the report started
	e.idle(b.ID)
	if msgs := e.callMessages("call-3"); len(msgs) != 3 || msgs[2].Author != TaskPrefix+"task-2" || msgs[2].Status != StatusComplete {
		t.Fatalf("call thread after a live report: %+v", msgs)
	}
}

// A call follow-up still waiting in the bot's queue when the run is stopped is canceled in the call thread: its new
// state is published as a call event, never on the chat timeline.
func TestCanceledCallFollowUpStaysInTheCallThread(t *testing.T) {
	bp := &blockingProvider{started: make(chan struct{}, 1), release: make(chan struct{}), text: "working"}
	e := newEnv(t, nil)
	e.llm.prov = bp
	b := e.bot("Coder")
	e.send(b.ID, "do the thing")
	<-bp.started
	if err := e.m.ReportTask(context.Background(), TaskReport{BotID: b.ID, TaskID: "task-1", Title: "Fix", Status: "finished", Text: "done", CallID: "call-4"}); err != nil {
		t.Fatal(err)
	}
	msgs := e.callMessages("call-4")
	if len(msgs) != 1 || msgs[0].Status != StatusPending {
		t.Fatalf("the report must wait in the call thread: %+v", msgs)
	}
	report := msgs[0].ID
	if err := e.m.Cancel(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}
	close(bp.release)
	e.idle(b.ID)
	if msgs := e.callMessages("call-4"); len(msgs) != 1 || msgs[0].Status != StatusCanceled {
		t.Fatalf("call thread: %+v", msgs)
	}
	canceled := false
	for _, r := range e.rec.all() {
		m, ok := r.payload.(Message)
		if !ok || m.ID != report {
			continue
		}
		if r.event != "chat.call.message" {
			t.Fatalf("the call follow-up was published as %s", r.event)
		}
		canceled = canceled || m.Status == StatusCanceled
	}
	if !canceled {
		t.Fatal("the canceled follow-up was not published")
	}
}

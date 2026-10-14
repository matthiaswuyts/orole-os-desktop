package chat

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func textScript(replies ...string) llmtest.Script {
	return func(call int, _ llm.Request) []llm.Delta {
		return []llm.Delta{llmtest.Text(replies[min(call, len(replies)-1)])}
	}
}

func (e *env) lastRequest() llm.Request {
	e.t.Helper()
	calls := e.fake.Calls()
	if len(calls) == 0 {
		e.t.Fatal("no model request was made")
	}
	return calls[len(calls)-1]
}

func TestVoiceTurnStreamsAndStores(t *testing.T) {
	e := newEnv(t, textScript("Two tasks are running."))
	b := e.bot("Atlas")
	var mu sync.Mutex
	var spoken strings.Builder
	reply, err := e.m.VoiceTurn(context.Background(), VoiceRequest{
		BotID: b.ID, CallID: "c1", Text: "what is running?", Brief: "Reason: you asked me to call.",
		OnText: func(s string) { mu.Lock(); spoken.WriteString(s); mu.Unlock() },
	})
	if err != nil || reply != "Two tasks are running." {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if spoken.String() != reply {
		t.Fatalf("streamed %q", spoken.String())
	}
	if h := e.history(b.ID); len(h) != 0 {
		t.Fatalf("spoken turns leaked into the chat page: %+v", h)
	}
	h := e.callMessages("c1")
	if len(h) != 2 || h[0].Role != "user" || replyText(h[1]) != reply || h[0].CallID != "c1" || h[1].CallID != "c1" {
		t.Fatalf("call transcript %+v", h)
	}
	if !strings.Contains(e.lastRequest().System, "you asked me to call") {
		t.Error("brief missing from the system prompt")
	}
	e.m.mu.Lock()
	_, left := e.m.runs[callPrefix+"c1"]
	e.m.mu.Unlock()
	if left {
		t.Error("call run key not released")
	}
}

func TestVoiceTurnCarriesCallHistory(t *testing.T) {
	e := newEnv(t, textScript("sure"))
	b := e.bot("Atlas")
	_, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "and now?",
		History: []VoiceLine{{Role: "assistant", Text: "Hi, Atlas here."}, {Role: "user", Text: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, m := range e.lastRequest().Messages {
		for _, p := range m.Parts {
			all.WriteString(p.Text + "|")
		}
	}
	if !strings.Contains(all.String(), "Hi, Atlas here.") || !strings.Contains(all.String(), "and now?") {
		t.Errorf("history not sent: %q", all.String())
	}
}

func TestVoiceTurnUsesVoicePromptAndNoReasoning(t *testing.T) {
	e := newEnv(t, textScript("ok"))
	b := e.bot("Atlas", func(in *BotInput) { in.Reasoning = "high" })
	if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	req := e.lastRequest()
	if req.Reasoning != "low" {
		t.Errorf("voice turn reasoning = %q, want the cheapest level", req.Reasoning)
	}
	if !strings.Contains(req.System, "phone call") || strings.Contains(req.System, chatStyle) {
		t.Error("voice turn did not swap chat style for voice style")
	}
	e.send(b.ID, "typed")
	e.idle(b.ID)
	if r := e.lastRequest(); r.Reasoning != "high" || !strings.Contains(r.System, chatStyle) {
		t.Errorf("chat turn affected by voice mode: reasoning=%q", r.Reasoning)
	}
}

func TestVoiceTurnRunsWhileChatTurnBusy(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	e := newEnv(t, func(call int, _ llm.Request) []llm.Delta {
		if call == 0 {
			<-release // the chat turn blocks inside its model call
			return []llm.Delta{llmtest.Text("late chat reply")}
		}
		return []llm.Delta{llmtest.Text("on the phone")}
	})
	b := e.bot("Atlas")
	e.send(b.ID, "long job")
	e.until("chat turn to reach the model", func() bool { return len(e.fake.Calls()) == 1 })
	reply, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "hey"})
	if err != nil || reply != "on the phone" {
		t.Fatalf("voice turn waited behind chat turn: %q %v", reply, err)
	}
	unblock()
	e.idle(b.ID)
	h := append(e.history(b.ID), e.callMessages("c1")...)
	if len(h) != 4 {
		t.Fatalf("want 4 messages, got %d", len(h))
	}
	seen := map[int64]bool{}
	for _, m := range h {
		if seen[m.Seq] {
			t.Fatalf("duplicate seq %d", m.Seq)
		}
		seen[m.Seq] = true
	}
}

func TestVoiceTurnRejectsConcurrentTurnAndReportsCancel(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	e := newEnv(t, func(call int, _ llm.Request) []llm.Delta {
		<-release
		return nil
	})
	b := e.bot("Atlas")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := e.m.VoiceTurn(ctx, VoiceRequest{BotID: b.ID, CallID: "c1", Text: "one"})
		done <- err
	}()
	e.until("first turn", func() bool { return len(e.fake.Calls()) == 1 })
	if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "two"}); err == nil {
		t.Error("second turn of the same call was accepted")
	}
	cancel() // the owner talks over the bot
	once.Do(func() { close(release) })
	if err := <-done; err == nil {
		t.Error("canceled turn reported success")
	}
}

func TestVoiceTurnCallsOnToolBeforeRunning(t *testing.T) {
	ft := &fakeTool{id: "probe"}
	e := newEnv(t, func(call int, _ llm.Request) []llm.Delta {
		if call == 0 {
			return llmtest.Call("t1", "probe", `{}`)
		}
		return []llm.Delta{llmtest.Text("done")}
	})
	e.m.AddTools(ft)
	b := e.bot("Atlas", func(in *BotInput) { in.Config.ToolAccess = ToolAccessAll })
	var tools []string
	var seenAtCall int
	req := VoiceRequest{BotID: b.ID, CallID: "c1", Text: "go", OnTool: func(n string) { tools = append(tools, n); seenAtCall = ft.callCount() }}
	if _, err := e.m.VoiceTurn(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0] != "probe" || ft.callCount() != 1 || seenAtCall != 0 {
		t.Fatalf("tools=%v calls=%d beforeRun=%d", tools, ft.callCount(), seenAtCall)
	}
}

func TestComposeDoesNotStore(t *testing.T) {
	e := newEnv(t, textScript("Hey, Atlas here."))
	b := e.bot("Atlas")
	got, err := e.m.Compose(context.Background(), b.ID, "Write the opener.")
	if err != nil || got != "Hey, Atlas here." || len(e.history(b.ID)) != 0 {
		t.Fatalf("got=%q err=%v history=%d", got, err, len(e.history(b.ID)))
	}
	if r := e.lastRequest(); len(r.Tools) != 0 || !strings.Contains(r.System, "phone call") {
		t.Errorf("compose sent tools or the chat style: %+v", r.Tools)
	}
}

func TestCallCardPostAndUpdate(t *testing.T) {
	e := newEnv(t, nil)
	b := e.bot("Atlas")
	id, err := e.m.PostCallCard(context.Background(), b.ID, MsgPart{Type: PartCall, ID: "c1", Name: "outbound", Status: "dialing", Title: "deploy failed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.UpdateCallCard(context.Background(), id, MsgPart{Type: PartCall, ID: "c1", Name: "outbound", Status: "completed", DurationMs: 61_000}); err != nil {
		t.Fatal(err)
	}
	h := e.history(b.ID)
	if len(h) != 1 || h[0].Parts[0].Status != "completed" || h[0].Parts[0].DurationMs != 61_000 {
		t.Fatalf("history %+v", h)
	}
	if got := e.rec.messages(b.ID); len(got) != 2 || got[1].Parts[0].Status != "completed" {
		t.Errorf("card events not published: %+v", got)
	}
	plain := e.send(b.ID, "hi")
	e.idle(b.ID)
	if err := e.m.UpdateCallCard(context.Background(), plain.ID, MsgPart{}); err == nil {
		t.Error("overwrote a normal message with a call card")
	}
}

func TestJarvisConfigValidationAndSaveCheck(t *testing.T) {
	e := newEnv(t, nil)
	b := e.bot("Atlas")
	in := BotInput{ID: b.ID, Name: "Atlas", Config: BotConfig{Jarvis: BotJarvis{Enabled: true, Policy: strings.Repeat("x", MaxJarvisPolicy+1)}}}
	if _, err := e.m.SaveBot(context.Background(), in); err == nil {
		t.Error("accepted an over-long policy")
	}
	in.Config.Jarvis = BotJarvis{VoiceID: "has space"}
	if _, err := e.m.SaveBot(context.Background(), in); err == nil {
		t.Error("accepted a voice id with whitespace")
	}
	var sawStored []bool
	e.m.SetSaveCheck(func(_ context.Context, stored *Bot, in BotInput) (BotInput, error) {
		sawStored = append(sawStored, stored != nil && stored.Config.Jarvis.Enabled)
		if in.Config.ToolAccess != ToolAccessAll {
			if stored != nil && stored.Config.Jarvis.Enabled {
				in.Config.Jarvis.Enabled = false // already on: keep the save, switch Jarvis off
				return in, nil
			}
			return in, invalidf("Jarvis needs this bot's tool access set to All.")
		}
		return in, nil
	})
	in.Config.Jarvis = BotJarvis{Enabled: true, Policy: "only failures"}
	if _, err := e.m.SaveBot(context.Background(), in); err == nil || !strings.Contains(err.Error(), "tool access") {
		t.Errorf("save check not applied: %v", err)
	}
	in.Config.Jarvis.Enabled = false
	if _, err := e.m.SaveBot(context.Background(), in); err != nil {
		t.Errorf("disabled jarvis rejected: %v", err)
	}
	in.Config.ToolAccess, in.Config.Jarvis.Enabled = ToolAccessAll, true
	if _, err := e.m.SaveBot(context.Background(), in); err != nil {
		t.Fatalf("enable with full access: %v", err)
	}
	in.Config.ToolAccess = ToolAccessWrite
	got, err := e.m.SaveBot(context.Background(), in)
	if err != nil || got.Config.Jarvis.Enabled || got.Config.ToolAccess != ToolAccessWrite {
		t.Fatalf("adjusted save: %+v %v", got.Config, err)
	}
	if len(sawStored) != 3 || sawStored[0] || sawStored[1] || !sawStored[2] {
		t.Fatalf("stored bot passed to the check: %v", sawStored)
	}
}

func TestVoiceLimitsNeverWiden(t *testing.T) {
	cases := []struct{ in, want BotLimits }{
		{BotLimits{}, BotLimits{MaxSteps: voiceMaxSteps, MaxToolCalls: voiceMaxToolCalls}},
		{BotLimits{MaxSteps: 3, MaxToolCalls: 2}, BotLimits{MaxSteps: 3, MaxToolCalls: 2}},
		{BotLimits{MaxSteps: 50, MaxToolCalls: 50}, BotLimits{MaxSteps: voiceMaxSteps, MaxToolCalls: voiceMaxToolCalls}},
	}
	for _, c := range cases {
		if got := voiceLimits(c.in); got != c.want {
			t.Errorf("voiceLimits(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
	// End to end: a bot limited to 1 step gets a final, tool-less call on the phone.
	ft := &fakeTool{id: "probe"}
	e := newEnv(t, func(call int, _ llm.Request) []llm.Delta { return []llm.Delta{llmtest.Text("hi")} })
	e.m.AddTools(ft)
	b := e.bot("Atlas", func(in *BotInput) { in.Config.Limits = BotLimits{MaxSteps: 1}; in.Config.ToolAccess = ToolAccessAll })
	if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "go"}); err != nil {
		t.Fatal(err)
	}
	if len(e.lastRequest().Tools) != 0 {
		t.Error("a 1-step bot was offered tools on a call")
	}
}

func blockingEnv(t *testing.T, release *func()) *env {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	*release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(*release)
	return newEnv(t, func(call int, _ llm.Request) []llm.Delta {
		<-gate
		return []llm.Delta{llmtest.Text("late")}
	})
}

func TestArchiveAndClearStopVoiceTurn(t *testing.T) {
	for _, name := range []string{"archive", "clear"} {
		t.Run(name, func(t *testing.T) {
			var release func()
			e := blockingEnv(t, &release)
			b := e.bot("Atlas")
			done := make(chan error, 1)
			go func() {
				_, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "go"})
				done <- err
			}()
			e.until("voice turn to reach the model", func() bool { return len(e.fake.Calls()) == 1 })
			finished := make(chan error, 1)
			go func() {
				if name == "archive" {
					finished <- e.m.ArchiveBot(context.Background(), b.ID)
				} else {
					finished <- e.m.Clear(context.Background(), b.ID)
				}
			}()
			time.Sleep(150 * time.Millisecond) // let Archive/Clear cancel the turn; the fake ignores ctx, the engine must not
			release()
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if err := <-done; err == nil {
				t.Error("voice turn was not canceled")
			}
			e.m.mu.Lock()
			n := len(e.m.runs)
			e.m.mu.Unlock()
			if n != 0 {
				t.Errorf("%d runs left", n)
			}
			if name == "clear" {
				if h := append(e.history(b.ID), e.callMessages("c1")...); len(h) != 0 {
					t.Errorf("orphan messages after clear: %+v", h)
				}
			} else if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c2", Text: "hi"}); err == nil {
				t.Error("voice turn started for an archived bot")
			}
		})
	}
}

// A voice turn cannot wait for an approval: a tool whose policy asks fails at once with a spoken-friendly error.
func TestVoiceTurnPolicyAskFailsAtOnce(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "p.db"), migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	pe := policy.New(db.DB, nil, nil)
	var toolErr error
	ft := &fakeTool{id: "send_mail", call: func(ctx context.Context, rc RunContext, _ string, _ json.RawMessage) (ToolResult, error) {
		toolErr = pe.Require(ctx, policy.Request{Actor: rc.Actor, Action: "mail.send", Resource: "mail/s", Effect: policy.EffectWrite})
		return ToolResult{}, toolErr
	}}
	e := newEnv(t, func(call int, _ llm.Request) []llm.Delta {
		if call == 0 {
			return llmtest.Call("t1", "send_mail", `{}`)
		}
		return []llm.Delta{llmtest.Text("It is waiting in the app.")}
	})
	e.m.AddTools(ft)
	b := e.bot("Atlas", func(in *BotInput) { in.Config.ToolAccess = ToolAccessAll })
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "mail Bob"})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("voice turn waited for an approval")
	}
	if !errors.Is(toolErr, policy.ErrApprovalUnavailable) || len(pe.Pending()) != 0 {
		t.Fatalf("tool err = %v, pending = %d", toolErr, len(pe.Pending()))
	}
	if !strings.Contains(e.lastRequest().System, "say nothing about the reason") {
		t.Fatal("voice prompt lacks the voicemail rule")
	}
}

func (e *env) callMessages(callID string) []Message {
	e.t.Helper()
	msgs, err := e.m.CallMessages(context.Background(), callID)
	if err != nil {
		e.t.Fatal(err)
	}
	return msgs
}

// Spoken turns stay in the bot's history and memory but never reach the chat UI: not the page, the bot preview,
// regenerate, or the chat.message events; the call transcript returns them in order.
func TestCallTurnsHiddenFromChatButInHistory(t *testing.T) {
	e := newEnv(t, textScript("typed reply", "spoken reply one", "spoken reply two", "next typed reply"))
	b := e.bot("Atlas")
	e.send(b.ID, "typed hello")
	e.idle(b.ID)
	if _, err := e.m.PostCallCard(context.Background(), b.ID, MsgPart{ID: "c1", Name: "outbound", Status: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	for _, said := range []string{"first spoken", "second spoken"} {
		if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: said}); err != nil {
			t.Fatal(err)
		}
	}

	page := e.history(b.ID)
	if len(page) != 3 || page[2].Parts[0].Type != PartCall {
		t.Fatalf("chat page = %+v, want the typed exchange and the call card", page)
	}
	got, err := e.m.Bot(context.Background(), b.ID)
	if err != nil || got.LastMessage == nil || got.LastMessage.Text != "" || got.LastMessage.Role != "assistant" {
		t.Fatalf("preview = %+v (err %v), want the call card", got.LastMessage, err)
	}
	bots, err := e.m.Bots(context.Background())
	if err != nil || len(bots) != 1 || bots[0].LastMessage == nil || strings.Contains(bots[0].LastMessage.Text, "spoken") {
		t.Fatalf("list preview = %+v (err %v)", bots, err)
	}
	if err := e.m.Regenerate(context.Background(), RegenerateRequest{BotID: b.ID, MessageID: e.callMessages("c1")[3].ID}); err == nil {
		t.Error("regenerated a spoken reply")
	}
	for _, r := range e.rec.all() {
		if m, ok := r.payload.(Message); ok && r.event == "chat.message" && m.CallID != "" {
			t.Fatalf("spoken turn published as chat.message: %+v", m)
		}
		if d, ok := r.payload.(MessageDelta); ok && r.event == "chat.message.delta" {
			for _, m := range e.callMessages("c1") {
				if m.ID == d.MessageID {
					t.Fatal("spoken turn streamed as chat.message.delta")
				}
			}
		}
	}
	var callEvents int
	for _, r := range e.rec.all() {
		if m, ok := r.payload.(Message); ok && r.event == "chat.call.message" && m.CallID == "c1" {
			callEvents++
		}
	}
	if callEvents == 0 {
		t.Error("no chat.call.message events")
	}

	tr := e.callMessages("c1")
	want := []string{"first spoken", "spoken reply one", "second spoken", "spoken reply two"}
	if len(tr) != len(want) {
		t.Fatalf("transcript has %d messages", len(tr))
	}
	for i, w := range want {
		if replyText(tr[i]) != w || (i > 0 && tr[i].Seq <= tr[i-1].Seq) {
			t.Fatalf("transcript[%d] = %q (seq %d)", i, replyText(tr[i]), tr[i].Seq)
		}
	}

	// The model still sees the call in the next typed turn.
	e.send(b.ID, "what did we discuss?")
	e.idle(b.ID)
	var all strings.Builder
	for _, m := range e.lastRequest().Messages {
		for _, p := range m.Parts {
			all.WriteString(p.Text + "|")
		}
	}
	for _, w := range want {
		if !strings.Contains(all.String(), w) {
			t.Errorf("model history misses %q: %s", w, all.String())
		}
	}
	tail, err := e.m.store.tail(context.Background(), b.ID, 0, 1<<62, 0, 100)
	if err != nil || len(tail.msgs) != 9 {
		t.Fatalf("history tail = %d messages (err %v), want all 9", len(tail.msgs), err)
	}
}

func TestVoiceTurnSpeaksTheCallLanguage(t *testing.T) {
	for _, c := range []struct{ lang, want string }{{"nl", "Speak Dutch"}, {"en", "Speak English"}, {"auto", "language the owner speaks"}, {"", ""}} {
		e := newEnv(t, textScript("ok"))
		b := e.bot("Atlas")
		if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "hoi", Language: c.lang}); err != nil {
			t.Fatal(err)
		}
		sys := e.lastRequest().System
		if c.want != "" && !strings.Contains(sys, c.want) {
			t.Errorf("%q: voice prompt lacks %q", c.lang, c.want)
		}
		if c.want == "" && strings.Contains(sys, "Speak ") {
			t.Errorf("no language set, but the prompt names one")
		}
	}
}

// The call card is the newest visible message after a call; regenerating it would wipe the card.
func TestRegenerateRefusesCallCardAndLiveCall(t *testing.T) {
	e := newEnv(t, textScript("typed reply"))
	b := e.bot("Atlas")
	e.send(b.ID, "hi")
	e.idle(b.ID)
	reply := e.lastReply(b.ID)

	e.m.mu.Lock()
	e.m.runs[callPrefix+"c9"] = &runState{botID: b.ID, done: make(chan struct{})}
	e.m.mu.Unlock()
	err := e.m.Regenerate(context.Background(), RegenerateRequest{BotID: b.ID, MessageID: reply.ID})
	if err == nil || !strings.Contains(err.Error(), "phone call") {
		t.Fatalf("regenerate during a live call: %v", err)
	}
	e.m.mu.Lock()
	delete(e.m.runs, callPrefix+"c9")
	e.m.mu.Unlock()

	id, err := e.m.PostCallCard(context.Background(), b.ID, MsgPart{ID: "c1", Name: "outbound", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	err = e.m.Regenerate(context.Background(), RegenerateRequest{BotID: b.ID, MessageID: id})
	if err == nil || !strings.Contains(err.Error(), "phone call card") {
		t.Fatalf("regenerated the call card: %v", err)
	}
	if err := e.m.UpdateCallCard(context.Background(), id, MsgPart{ID: "c1", Status: "completed", DurationMs: 1000}); err != nil {
		t.Fatalf("card damaged: %v", err)
	}
}

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

// A cue is an instruction from the daemon, not something the owner said: the call transcript leaves it out, the model
// (this call's later turns and the bot's typed chat) still sees it.
func TestCueTurnIsHiddenFromTheTranscriptButInModelHistory(t *testing.T) {
	e := newEnv(t, textScript("Hi, Atlas here.", "Sure.", "typed reply"))
	b := e.bot("Atlas")
	const cue = "Greet the owner now and say why you are calling."
	reply, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: cue, Cue: true})
	if err != nil || reply != "Hi, Atlas here." {
		t.Fatalf("cue turn: reply=%q err=%v", reply, err)
	}
	if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	tr := e.callMessages("c1")
	want := []string{"Hi, Atlas here.", "hello", "Sure."}
	if len(tr) != len(want) {
		t.Fatalf("transcript has %d messages, want %d: %+v", len(tr), len(want), tr)
	}
	for i, w := range want {
		if replyText(tr[i]) != w {
			t.Errorf("transcript[%d] = %q, want %q", i, replyText(tr[i]), w)
		}
		if replyText(tr[i]) == cue || tr[i].Author == cueAuthor {
			t.Errorf("transcript[%d] is the cue", i)
		}
	}
	if h := e.history(b.ID); len(h) != 0 {
		t.Fatalf("cue leaked into the chat page: %+v", h)
	}

	// The cue is stored like any call message: user role, author "cue", tagged with the call.
	tail, err := e.m.store.tail(context.Background(), b.ID, 0, 1<<62, 0, 100)
	if err != nil || len(tail.msgs) != 4 {
		t.Fatalf("stored messages = %d (err %v), want 4", len(tail.msgs), err)
	}
	if first := tail.msgs[0]; first.Role != "user" || first.Author != cueAuthor || historyText(first) != cue {
		t.Fatalf("stored cue = %+v", first)
	}
	if tail.msgs[2].Author != "user" {
		t.Errorf("a normal utterance was stored with author %q", tail.msgs[2].Author)
	}
	cues := 0
	for _, r := range e.rec.all() {
		if m, ok := r.payload.(Message); ok && r.event == "chat.message" && m.CallID != "" {
			t.Fatalf("cue published as chat.message: %+v", m)
		}
		if m, ok := r.payload.(Message); ok && r.event == "chat.call.message" && m.Author == cueAuthor {
			cues++
		}
	}
	if cues == 0 {
		t.Error("the cue was not published as chat.call.message")
	}

	// The model still sees it.
	e.send(b.ID, "what did we say?")
	e.idle(b.ID)
	var all strings.Builder
	for _, m := range e.lastRequest().Messages {
		for _, p := range m.Parts {
			all.WriteString(p.Text + "|")
		}
	}
	if !strings.Contains(all.String(), cue) {
		t.Errorf("model history misses the cue: %s", all.String())
	}
}

func TestVoicePromptNamesTheBotAndSetsTheCallRules(t *testing.T) {
	e := newEnv(t, textScript("ok"))
	b := e.bot("Atlas")
	if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: b.ID, CallID: "c1", Text: "hi", Language: "en"}); err != nil {
		t.Fatal(err)
	}
	sys := e.lastRequest().System
	for _, want := range []string{
		"Your name is Atlas. Use only that name for yourself.",
		"say who is doing it and why in one sentence",
		"then ask whether there is anything else you can do in the meantime.",
		"When the owner asks how a handed-off or started task is going, check it with your tools before answering.",
		"save a call rule for it with jarvis_rule_add (trigger bot_reported for work you handed to a bot, task_finished for a code task) and confirm it.",
		"When you have done what the owner asked, ask briefly whether there is anything else.\n",
		"Only end the call when the owner clearly says they are done (no, that's all, thanks, bye): say a short friendly goodbye and call end_call in the same reply. Never end the call for any other reason.",
		// the lines that stay
		"Answer in one to three short sentences.",
		"No markdown, lists, code, links or emoji.",
		"Never name the app you run in",
		"say nothing about the reason",
		"it is waiting in the app",
		"Speak English",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("voice prompt lacks %q", want)
		}
	}
	for _, gone := range []string{"You may say your own name", "When the owner is done (no, that's all"} {
		if strings.Contains(sys, gone) {
			t.Errorf("voice prompt still has the replaced line %q", gone)
		}
	}
}

func TestBuildSystemVoiceName(t *testing.T) {
	voice := buildSystem(systemInput{Bot: Bot{Name: "  Atlas \n Prime "}, Voice: true})
	if !strings.Contains(voice, "- Your name is Atlas Prime. Use only that name for yourself.") {
		t.Errorf("name line missing or not normalised: %q", voice)
	}
	if got := buildSystem(systemInput{Bot: Bot{}, Voice: true}); strings.Contains(got, "Your name is") {
		t.Errorf("a bot without a name got a name line: %q", got)
	}
	if got := buildSystem(systemInput{Bot: Bot{Name: "Atlas"}}); strings.Contains(got, "Your name is") {
		t.Errorf("the chat prompt got the voice name line")
	}
}

// While a handoff is out, a voice turn's system prompt lists it after the brief, so the bot can answer "how is that
// task going?"; once the task has reported back (or when there is none) the prompt ends with the brief alone.
func TestVoiceTurnSeesTheHandoffsThatAreOut(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	e := newEnv(t, func(call int, req llm.Request) []llm.Delta {
		if strings.Contains(req.System, "Be Callee.") {
			<-release
			return []llm.Delta{llmtest.Text("findings")}
		}
		return []llm.Delta{llmtest.Text("caller here")}
	})
	callee := e.bot("Callee")
	caller := e.bot("Caller", executive)
	const brief = "Reason: you asked me to call."

	voiceSystem := func(callID string) string {
		t.Helper()
		if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: caller.ID, CallID: callID, Text: "how is it going?", Brief: brief}); err != nil {
			t.Fatal(err)
		}
		return e.lastRequest().System
	}

	if sys := voiceSystem("c0"); !strings.HasSuffix(sys, brief) || strings.Contains(sys, "have not reported back") {
		t.Fatalf("no handoff is out, yet the prompt is %q", sys)
	}

	if err := e.m.Handoff(context.Background(), fromBot(caller), callee.ID, "research the thing"); err != nil {
		t.Fatal(err)
	}
	sys := voiceSystem("c1")
	note := e.m.handoffsNote(caller.ID)
	if note == "" || !strings.HasSuffix(sys, brief+"\n\n"+note) {
		t.Fatalf("the brief and the handoffs note must close the prompt, joined by a blank line: %q", sys)
	}
	if !strings.Contains(sys, "have not reported back yet") || !strings.Contains(sys, `"Callee"`) || !strings.Contains(sys, "research the thing") {
		t.Fatalf("the handoff is not in the voice prompt: %q", sys)
	}

	// Without a brief the note stands alone, with no stray blank lines in front of it.
	if _, err := e.m.VoiceTurn(context.Background(), VoiceRequest{BotID: caller.ID, CallID: "c2", Text: "and now?"}); err != nil {
		t.Fatal(err)
	}
	if sys := e.lastRequest().System; !strings.HasSuffix(sys, "\n\n"+note) || strings.Contains(sys, brief) {
		t.Fatalf("note without a brief: %q", sys)
	}

	unblock()
	e.until("the report to arrive", func() bool { return len(e.reports(caller.ID)) == 1 })
	e.idle(caller.ID)
	e.idle(callee.ID)
	if sys := voiceSystem("c3"); !strings.HasSuffix(sys, brief) || strings.Contains(sys, "have not reported back") {
		t.Fatalf("a finished task is still listed: %q", sys)
	}
}

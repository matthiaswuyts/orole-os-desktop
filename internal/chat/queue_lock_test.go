package chat

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/llmtest"
)

// lastUserText is the text of the message the request asks to answer: the last part of the last
// user message (a trailing history message of the same role is merged in front of the current one).
func lastUserText(req llm.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if m := req.Messages[i]; m.Role == llm.RoleUser && len(m.Parts) > 0 {
			return m.Parts[len(m.Parts)-1].Text
		}
	}
	return ""
}

// holdWriter checks every connection out of the database pool until release is called, so any
// store call blocks waiting for a connection, as one stuck behind a slow disk would.
func holdWriter(t *testing.T, e *env) (release func()) {
	t.Helper()
	ctx := context.Background()
	var conns []*sql.Conn
	for range 4 { // database.Open allows four connections
		conn, err := e.db.DB.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
	}
	var once sync.Once
	release = func() {
		once.Do(func() {
			for _, c := range conns {
				c.Close()
			}
		})
	}
	t.Cleanup(release)
	return release
}

func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s blocked for more than %s while a message was being stored: the global lock is held across SQLite work", what, d)
	}
}

func TestSlowStoreOfOneSendDoesNotBlockTheManager(t *testing.T) {
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta { return []llm.Delta{llmtest.Text("ok")} })
	a, b := e.bot("A"), e.bot("B")
	release := holdWriter(t, e)

	sent := make(chan error, 1)
	go func() {
		_, err := e.m.Send(context.Background(), SendRequest{BotID: a.ID, Text: "hello"})
		sent <- err
	}()
	time.Sleep(150 * time.Millisecond) // Send is now waiting for the write lock inside its insert
	select {
	case err := <-sent:
		t.Fatalf("Send returned while the database was unavailable: %v", err)
	default:
	}

	within(t, time.Second, "Tools", func() { e.m.Tools() })
	within(t, time.Second, "AddPromptSection", func() { e.m.AddPromptSection(func(Bot) string { return "" }) })
	within(t, time.Second, "Cancel", func() { e.m.Cancel(context.Background(), b.ID) })

	release()
	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send never completed")
	}
	e.idle(a.ID)
	if got := replyText(e.lastReply(a.ID)); got != "ok" {
		t.Fatalf("reply = %q", got)
	}
}

func TestConcurrentSendsKeepMessageOrderAndTurnOrder(t *testing.T) {
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta {
		time.Sleep(2 * time.Millisecond)
		return []llm.Delta{llmtest.Text("echo:" + lastUserText(req))}
	})
	b := e.bot("Ordered")
	const n = maxQueued
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.m.Send(context.Background(), SendRequest{BotID: b.ID, Text: fmt.Sprintf("msg-%02d", i)})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	e.until("every turn to be answered", func() bool {
		e.m.mu.Lock()
		defer e.m.mu.Unlock()
		return e.m.runs[b.ID] == nil
	})

	var users, replies []string
	for _, m := range e.history(b.ID) {
		switch m.Role {
		case "user":
			users = append(users, messageText(m.Parts))
			if m.Status != StatusComplete {
				t.Errorf("user message %q ended %s", messageText(m.Parts), m.Status)
			}
		case "assistant":
			if m.Status != StatusComplete {
				t.Errorf("reply ended %s: %s", m.Status, m.Error)
			}
			replies = append(replies, strings.TrimPrefix(replyText(m), "echo:"))
		}
	}
	if len(users) != n || len(replies) != n {
		t.Fatalf("%d user messages, %d replies, want %d each", len(users), len(replies), n)
	}
	for i := range users {
		if replies[i] != users[i] {
			t.Fatalf("reply %d answers %q but follows user message %q: turn order differs from message order", i, replies[i], users[i])
		}
	}
}

func TestSendAfterCloseStoresNothing(t *testing.T) {
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta { return []llm.Delta{llmtest.Text("ok")} })
	b := e.bot("Closing")
	e.m.Close()
	if _, err := e.m.Send(context.Background(), SendRequest{BotID: b.ID, Text: "late"}); err == nil || !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("Send after Close: %v", err)
	}
	if n := len(e.history(b.ID)); n != 0 {
		t.Fatalf("%d messages stored after Close", n)
	}
}

func TestClearAndArchiveWaitForTheirBotOnly(t *testing.T) {
	e := newEnv(t, func(_ int, req llm.Request) []llm.Delta { return []llm.Delta{llmtest.Text("ok")} })
	a, b := e.bot("A"), e.bot("B")
	e.send(a.ID, "hi")
	e.idle(a.ID)
	release := holdWriter(t, e)
	cleared := make(chan error, 1)
	go func() { cleared <- e.m.Clear(context.Background(), a.ID) }()
	time.Sleep(150 * time.Millisecond)
	// Another bot's Send and the run map stay usable while A's conversation is being deleted.
	within(t, time.Second, "Cancel(B)", func() { e.m.Cancel(context.Background(), b.ID) })
	within(t, time.Second, "Tools", func() { e.m.Tools() })
	release()
	if err := <-cleared; err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if n := len(e.history(a.ID)); n != 0 {
		t.Fatalf("%d messages after Clear", n)
	}
}

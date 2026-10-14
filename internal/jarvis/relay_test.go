package jarvis

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func fakeRelay(t *testing.T, onConn func(c *websocket.Conn)) (*httptest.Server, *atomic.Int32) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conns.Add(1)
		onConn(c)
	}))
	t.Cleanup(srv.Close)
	return srv, &conns
}

func TestRelayClientRoundTripAndReconnect(t *testing.T) {
	got := make(chan Frame, 4)
	srv, conns := fakeRelay(t, func(c *websocket.Conn) {
		ctx := context.Background()
		b, _ := json.Marshal(Frame{Type: FrameLLMRequest, ID: "r1", CallID: "c1", Messages: []LLMMessage{{Role: "user", Content: "hi"}}})
		_ = c.Write(ctx, websocket.MessageText, b)
		_, data, err := c.Read(ctx)
		if err == nil {
			var f Frame
			_ = json.Unmarshal(data, &f)
			got <- f
		}
		c.Close(websocket.StatusGoingAway, "bye") // force a reconnect
	})
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	var rc *RelayClient
	rc = NewRelayClient(url, "tok", func(f Frame) {
		if f.Type == FrameLLMRequest {
			_ = rc.Send(context.Background(), Frame{Type: FrameLLMDelta, ID: f.ID, CallID: f.CallID, Text: "hello"})
		}
	}, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go rc.Run(ctx)
	for i := 0; i < 2; i++ {
		select {
		case f := <-got:
			if f.Type != FrameLLMDelta || f.Text != "hello" || f.ID != "r1" {
				t.Fatalf("frame %+v", f)
			}
		case <-ctx.Done():
			t.Fatal("timeout")
		}
	}
	if conns.Load() < 2 {
		t.Fatalf("did not reconnect: %d", conns.Load())
	}
}

func TestRelayClientSendWhileDisconnected(t *testing.T) {
	rc := NewRelayClient("ws://127.0.0.1:1", "tok", func(Frame) {}, slog.Default())
	if rc.Connected() {
		t.Fatal("connected without Run")
	}
	if err := rc.Send(context.Background(), Frame{Type: FrameLLMDone}); err != ErrRelayDown {
		t.Fatalf("err=%v", err)
	}
}

// The relay (relay/src/hub.ts) reads llm.tool by these exact field names.
func TestToolFrameJSONShape(t *testing.T) {
	b, err := json.Marshal(Frame{Type: FrameLLMTool, ID: "r1", CallID: "c1", Tool: "skip_turn", Args: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"llm.tool","id":"r1","callId":"c1","tool":"skip_turn","args":"{}"}`; string(b) != want {
		t.Fatalf("frame = %s, want %s", b, want)
	}
	var back Frame
	if err := json.Unmarshal(b, &back); err != nil || back.Tool != "skip_turn" || back.Args != "{}" || back.Type != "llm.tool" {
		t.Fatalf("round trip %+v err=%v", back, err)
	}
	// Other frames never carry the tool fields.
	b, _ = json.Marshal(Frame{Type: FrameLLMDone, ID: "r1"})
	if strings.Contains(string(b), "tool") || strings.Contains(string(b), "args") {
		t.Fatalf("done frame carries tool fields: %s", b)
	}
}

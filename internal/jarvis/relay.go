package jarvis

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	FrameLLMRequest    = "llm.request"    // relay -> daemon: a custom-LLM turn
	FrameLLMDelta      = "llm.delta"      // daemon -> relay: streamed text
	FrameLLMDone       = "llm.done"       // daemon -> relay: turn finished
	FrameLLMError      = "llm.error"      // daemon -> relay: turn failed (Text is spoken)
	FrameInboundLookup = "inbound.lookup" // relay -> daemon: inbound call webhook
	FrameInboundAnswer = "inbound.answer" // daemon -> relay: accept/reject + variables
	FrameCallStatus    = "call.status"    // relay -> daemon: post-call webhook
	FrameHangup        = "call.hangup"    // daemon -> relay: end the call after the current turn
)

var ErrRelayDown = errors.New("jarvis: relay not connected")

type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Frame struct {
	Type           string            `json:"type"`
	ID             string            `json:"id,omitempty"`     // request id, echoed in replies
	CallID         string            `json:"callId,omitempty"` // our call id (ElevenLabs dynamic variable call_id)
	Text           string            `json:"text,omitempty"`
	Messages       []LLMMessage      `json:"messages,omitempty"`
	Vars           map[string]string `json:"vars,omitempty"`
	Caller         string            `json:"caller,omitempty"`
	Called         string            `json:"called,omitempty"`
	CallSID        string            `json:"callSid,omitempty"`
	Accept         bool              `json:"accept,omitempty"`
	Status         string            `json:"status,omitempty"`
	ConversationID string            `json:"conversationId,omitempty"`
	DurationSecs   int               `json:"durationSecs,omitempty"`
	Overrides      json.RawMessage   `json:"overrides,omitempty"`
}

// RelayClient keeps one authenticated WebSocket to the relay open and hands every received frame to handle.
// handle runs on its own goroutine per frame so a slow model turn never blocks the read loop.
type RelayClient struct {
	url, token string
	handle     func(Frame)
	log        *slog.Logger
	// OnState, if set before Run, is called (and must not block) when the connection opens or closes.
	OnState func(connected bool)

	mu   sync.Mutex
	conn *websocket.Conn
}

func NewRelayClient(url, token string, handle func(Frame), log *slog.Logger) *RelayClient {
	return &RelayClient{url: url, token: token, handle: handle, log: log}
}

func (c *RelayClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *RelayClient) Send(ctx context.Context, f Frame) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return ErrRelayDown
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func (c *RelayClient) state(connected bool) {
	if c.OnState != nil {
		c.OnState(connected)
	}
}

func (c *RelayClient) Run(ctx context.Context) {
	backoff := 500 * time.Millisecond
	for ctx.Err() == nil {
		start := time.Now()
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		c.log.Warn("jarvis: relay connection ended", "err", err)
		if time.Since(start) > 30*time.Second {
			backoff = 500 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 10*time.Second)
	}
}

func (c *RelayClient) session(ctx context.Context) error {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dctx, c.url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}}})
	cancel()
	if err != nil {
		return err
	}
	conn.SetReadLimit(1 << 20)
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.state(true)
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		conn.CloseNow()
		c.state(false)
	}()
	pctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-pctx.Done():
				return
			case <-t.C:
				p, cancel := context.WithTimeout(pctx, 5*time.Second)
				err := conn.Ping(p)
				cancel()
				if err != nil {
					conn.CloseNow()
					return
				}
			}
		}
	}()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var f Frame
		if json.Unmarshal(data, &f) != nil || f.Type == "" {
			continue
		}
		go c.handle(f)
	}
}

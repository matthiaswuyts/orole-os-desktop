package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Dialer opens a transport connection.
type Dialer func(ctx context.Context) (net.Conn, error)

// Client is a reconnecting RPC client.
type Client struct {
	dial   Dialer
	token  func() string
	name   string
	nextID atomic.Uint64

	mu      sync.Mutex
	nc      net.Conn
	pending map[uint64]chan Message
	topics  []string
	onEvent func(event string, data json.RawMessage)
	closed  bool
	Hello   HelloResult
}

// NewClient creates a client; call Connect before use.
func NewClient(dial Dialer, token, name string) *Client {
	return NewClientFunc(dial, func() string { return token }, name)
}

// NewClientFunc is like NewClient but resolves the token on each connect, so a
// restarted daemon (which issues a fresh token) is picked up automatically.
func NewClientFunc(dial Dialer, token func() string, name string) *Client {
	return &Client{dial: dial, token: token, name: name, pending: map[uint64]chan Message{}}
}

// OnEvent installs the event callback (called on the reader goroutine; keep it short).
func (c *Client) OnEvent(f func(event string, data json.RawMessage)) {
	c.mu.Lock()
	c.onEvent = f
	c.mu.Unlock()
}

// Connect dials and performs the hello handshake.
func (c *Client) Connect(ctx context.Context) error {
	nc, err := c.dial(ctx)
	if err != nil {
		return err
	}
	br := bufio.NewReaderSize(nc, 64<<10)
	req, _ := json.Marshal(Message{ID: 1, Method: "hello", Params: mustJSON(HelloParams{MinVersion: 1, Version: ProtocolVersion, Token: c.token(), Client: c.name})})
	nc.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := nc.Write(append(req, '\n')); err != nil {
		nc.Close()
		return err
	}
	line, err := readLine(br)
	if err != nil {
		nc.Close()
		return err
	}
	var resp Message
	if err := json.Unmarshal(line, &resp); err != nil {
		nc.Close()
		return err
	}
	if resp.Error != nil {
		nc.Close()
		return resp.Error
	}
	var hr HelloResult
	_ = json.Unmarshal(resp.Result, &hr)
	nc.SetDeadline(time.Time{})
	c.mu.Lock()
	c.nc, c.Hello, c.closed = nc, hr, false
	topics := c.topics
	c.mu.Unlock()
	c.nextID.Store(1)
	go c.read(nc, br)
	if len(topics) > 0 {
		return c.call(ctx, "subscribe", SubscribeParams{Topics: topics}, nil)
	}
	return nil
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func (c *Client) read(nc net.Conn, br *bufio.Reader) {
	for {
		line, err := readLine(br)
		if err != nil {
			break
		}
		var m Message
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m.Event != "" {
			c.mu.Lock()
			f := c.onEvent
			c.mu.Unlock()
			if f != nil {
				f(m.Event, m.Data)
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[m.ID]
		delete(c.pending, m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
	c.mu.Lock()
	if c.nc == nc {
		c.nc = nil
	}
	for id, ch := range c.pending {
		ch <- Message{ID: id, Error: &Error{Code: "disconnected", Message: "connection lost"}}
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

// Connected reports whether a live connection exists.
func (c *Client) Connected() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.nc != nil }

// Subscribe registers topics (re-applied after reconnect).
func (c *Client) Subscribe(ctx context.Context, topics ...string) error {
	c.mu.Lock()
	c.topics = append(c.topics, topics...)
	c.mu.Unlock()
	return c.call(ctx, "subscribe", SubscribeParams{Topics: topics}, nil)
}

// Call invokes method; if disconnected it reconnects once. ctx cancellation
// sends a "cancel" for the in-flight call.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	if !c.Connected() {
		if err := c.Connect(ctx); err != nil {
			return err
		}
	}
	err := c.call(ctx, method, params, out)
	var e *Error
	if errors.As(err, &e) && e.Code == "disconnected" {
		if cerr := c.Connect(ctx); cerr != nil {
			return cerr
		}
		return c.call(ctx, method, params, out)
	}
	return err
}

func (c *Client) call(ctx context.Context, method string, params, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan Message, 1)
	c.mu.Lock()
	nc := c.nc
	if nc == nil {
		c.mu.Unlock()
		return &Error{Code: "disconnected", Message: "not connected"}
	}
	c.pending[id] = ch
	c.mu.Unlock()
	req, err := json.Marshal(Message{ID: id, Method: method, Params: mustJSON(params)})
	if err != nil {
		return err
	}
	if _, err := nc.Write(append(req, '\n')); err != nil {
		return &Error{Code: "disconnected", Message: err.Error()}
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if out != nil {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		cid := c.nextID.Add(1)
		cr, _ := json.Marshal(Message{ID: cid, Method: "cancel", Params: mustJSON(map[string]uint64{"id": id})})
		nc.Write(append(cr, '\n'))
		return ctx.Err()
	}
}

// Close shuts the connection permanently.
func (c *Client) Close() {
	c.mu.Lock()
	c.closed = true
	nc := c.nc
	c.mu.Unlock()
	if nc != nil {
		nc.Close()
	}
}

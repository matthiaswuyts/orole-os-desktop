package ipc

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Handler serves one method.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Server is the daemon-side RPC endpoint.
type Server struct {
	Token         string
	DaemonVersion string
	PID           int
	CallTimeout   time.Duration // default per-call deadline
	Log           *slog.Logger
	OutQueue      int // per-connection outbound queue bound
	mu            sync.RWMutex
	methods       map[string]Handler
	conns         map[*conn]struct{}
}

// NewServer returns a Server with sane defaults.
func NewServer(token, daemonVersion string, pid int, log *slog.Logger) *Server {
	return &Server{Token: token, DaemonVersion: daemonVersion, PID: pid, CallTimeout: 30 * time.Second,
		Log: log, OutQueue: 256, methods: map[string]Handler{}, conns: map[*conn]struct{}{}}
}

// Handle registers a method.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	s.methods[method] = h
	s.mu.Unlock()
}

// Publish sends an event to every subscribed connection. Connections whose
// queue is full are closed (backpressure: a stuck client never blocks the daemon).
// Events too large for one frame are dropped (a client would reject the frame
// and drop the link) and logged; nothing is marshalled when no connection is
// subscribed.
func (s *Server) Publish(event string, data any) {
	var targets []*conn
	s.mu.RLock()
	for c := range s.conns {
		if c.wants(event) {
			targets = append(targets, c)
		}
	}
	s.mu.RUnlock()
	if len(targets) == 0 {
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		s.Log.Warn("ipc: event not marshalled", "event", event, "err", err)
		return
	}
	if !fitsFrame(len(raw) + len(event)) {
		s.Log.Warn("ipc: oversized event dropped", "event", event, "bytes", len(raw), "max", MaxMessageBytes)
		return
	}
	msg := Message{Event: event, Data: raw}
	for _, c := range targets {
		if !c.enqueue(msg) {
			s.Log.Warn("ipc: slow subscriber dropped", "event", event)
			go c.close()
		}
	}
}

// frameEnvelope over-approximates the bytes a frame adds around its payload
// (`{"id":<20 digits>,"result":` + `}`, or `{"event":"` + `","data":` + `}`
// minus the event name, which callers count themselves).
const frameEnvelope = 64

// maxErrorMessage caps Error.Message so an error reply can never itself
// exceed a frame.
const maxErrorMessage = 16 << 10

// fitsFrame reports whether a frame whose variable content is n bytes stays
// within MaxMessageBytes (the limit the peer enforces when reading).
func fitsFrame(n int) bool { return n+frameEnvelope <= MaxMessageBytes }

// Serve accepts connections until ln is closed.
func (s *Server) Serve(ln net.Listener) error {
	for {
		nc, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		c := &conn{s: s, nc: nc, out: make(chan Message, s.OutQueue), done: make(chan struct{}), inflight: map[uint64]context.CancelFunc{}}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		go c.run()
	}
}

// CloseAll closes every client connection.
func (s *Server) CloseAll() {
	s.mu.RLock()
	cs := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		cs = append(cs, c)
	}
	s.mu.RUnlock()
	for _, c := range cs {
		c.close()
	}
}

type conn struct {
	s        *Server
	nc       net.Conn
	out      chan Message
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	authed   bool
	topics   []string
	inflight map[uint64]context.CancelFunc
}

func (c *conn) close() {
	c.once.Do(func() {
		close(c.done)
		c.nc.Close()
		c.mu.Lock()
		for _, cancel := range c.inflight {
			cancel()
		}
		c.mu.Unlock()
		c.s.mu.Lock()
		delete(c.s.conns, c)
		c.s.mu.Unlock()
	})
}

func (c *conn) enqueue(m Message) bool {
	select {
	case c.out <- m:
		return true
	default:
		return false
	}
}

func (c *conn) wants(event string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.topics {
		if t == "*" || t == event || (strings.HasSuffix(t, ".*") && strings.HasPrefix(event, strings.TrimSuffix(t, "*"))) {
			return true
		}
	}
	return false
}

func (c *conn) run() {
	defer c.close()
	go func() {
		enc := json.NewEncoder(c.nc)
		for {
			select {
			case m := <-c.out:
				c.nc.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if enc.Encode(m) != nil {
					c.close()
					return
				}
			case <-c.done:
				return
			}
		}
	}()
	r := bufio.NewReaderSize(c.nc, 64<<10)
	// Unauthenticated peers get a short window to say hello.
	c.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		line, err := readLine(r)
		if err != nil {
			return
		}
		var m Message
		if json.Unmarshal(line, &m) != nil || m.Method == "" {
			c.reply(m.ID, nil, &Error{CodeBadRequest, "malformed request"})
			if !c.isAuthed() {
				return
			}
			continue
		}
		if !c.isAuthed() {
			if m.Method != "hello" || !c.hello(m) {
				return
			}
			c.nc.SetReadDeadline(time.Time{})
			continue
		}
		c.dispatch(m)
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		part, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, part...)
		if len(buf) > MaxMessageBytes {
			return nil, errors.New("ipc: message too large")
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

func (c *conn) isAuthed() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.authed }

func (c *conn) hello(m Message) bool {
	var p HelloParams
	if json.Unmarshal(m.Params, &p) != nil {
		c.reply(m.ID, nil, &Error{CodeBadRequest, "bad hello"})
		return false
	}
	if subtle.ConstantTimeCompare([]byte(p.Token), []byte(c.s.Token)) != 1 {
		c.reply(m.ID, nil, &Error{CodeUnauthenticated, "invalid token"})
		time.Sleep(50 * time.Millisecond) // let the reply flush before closing
		return false
	}
	if p.MinVersion > ProtocolVersion || (p.Version != 0 && p.Version < 1) {
		c.reply(m.ID, nil, &Error{CodeVersion, "no common protocol version"})
		time.Sleep(50 * time.Millisecond)
		return false
	}
	c.mu.Lock()
	c.authed = true
	c.mu.Unlock()
	c.reply(m.ID, HelloResult{Version: ProtocolVersion, DaemonVersion: c.s.DaemonVersion, PID: c.s.PID}, nil)
	return true
}

// reply queues the response for request id. A result that cannot fit in one
// frame is replaced by a too_large error: the peer would otherwise reject the
// frame, drop the link and (historically) re-send the request.
func (c *conn) reply(id uint64, result any, e *Error) {
	msg := Message{ID: id, Error: e}
	if e == nil {
		raw, err := json.Marshal(result)
		switch {
		case err != nil:
			msg.Error = &Error{CodeInternal, err.Error()}
		case !fitsFrame(len(raw)):
			c.s.Log.Warn("ipc: oversized reply replaced", "id", id, "bytes", len(raw), "max", MaxMessageBytes)
			msg.Error = &Error{CodeTooLarge, "result of " + strconv.Itoa(len(raw)) + " bytes exceeds the " + strconv.Itoa(MaxMessageBytes) + " byte frame limit"}
		default:
			msg.Result = raw
		}
	}
	if msg.Error != nil && len(msg.Error.Message) > maxErrorMessage {
		// Copy: handlers may return a shared *Error.
		msg.Error = &Error{msg.Error.Code, strings.ToValidUTF8(msg.Error.Message[:maxErrorMessage], "") + "…"}
	}
	if !c.enqueue(msg) {
		c.close()
	}
}

func (c *conn) dispatch(m Message) {
	switch m.Method {
	case "subscribe":
		var p SubscribeParams
		if json.Unmarshal(m.Params, &p) != nil {
			c.reply(m.ID, nil, &Error{CodeBadRequest, "bad params"})
			return
		}
		c.mu.Lock()
		c.topics = append(c.topics, p.Topics...)
		c.mu.Unlock()
		c.reply(m.ID, map[string]bool{"ok": true}, nil)
		return
	case "cancel":
		var p struct {
			ID uint64 `json:"id"`
		}
		json.Unmarshal(m.Params, &p)
		c.mu.Lock()
		if cancel, ok := c.inflight[p.ID]; ok {
			cancel()
		}
		c.mu.Unlock()
		c.reply(m.ID, map[string]bool{"ok": true}, nil)
		return
	case "ping":
		c.reply(m.ID, map[string]int64{"t": time.Now().UnixMilli()}, nil)
		return
	}
	c.s.mu.RLock()
	h := c.s.methods[m.Method]
	c.s.mu.RUnlock()
	if h == nil {
		c.reply(m.ID, nil, &Error{CodeNotFound, "unknown method " + m.Method})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.s.CallTimeout)
	c.mu.Lock()
	c.inflight[m.ID] = cancel
	c.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			c.mu.Lock()
			delete(c.inflight, m.ID)
			c.mu.Unlock()
			if r := recover(); r != nil {
				c.s.Log.Error("ipc: handler panic", "method", m.Method, "panic", r)
				c.reply(m.ID, nil, &Error{CodeInternal, "internal error"})
			}
		}()
		res, err := h(ctx, m.Params)
		if err != nil {
			var ie *Error
			switch {
			case errors.As(err, &ie):
				c.reply(m.ID, nil, ie)
			case errors.Is(err, context.DeadlineExceeded):
				c.reply(m.ID, nil, &Error{CodeTimeout, "call timed out"})
			case errors.Is(err, context.Canceled):
				c.reply(m.ID, nil, &Error{CodeCanceled, "canceled"})
			default:
				c.reply(m.ID, nil, &Error{CodeInternal, err.Error()})
			}
			return
		}
		c.reply(m.ID, res, nil)
	}()
}

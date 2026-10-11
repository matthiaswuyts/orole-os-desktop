package machines

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sessionIdleTimeout is how long a pooled SSH+SFTP session with no active
// operation stays open.
const sessionIdleTimeout = time.Minute

// sessionKey is the transport identity a pooled session was authenticated for.
// A session is reused only for exactly this identity.
type sessionKey struct{ address, username, fingerprint, hostKey string }

func keyFor(v Machine) sessionKey {
	return sessionKey{v.Address, v.Username, v.Fingerprint, v.hostKey}
}

type pooledSession struct {
	id       string
	key      sessionKey
	client   *ssh.Client
	sftp     *sftp.Client
	users    int
	timer    *time.Timer
	lastUsed time.Time
	closed   bool
}

func (e *pooledSession) close() {
	_ = e.sftp.Close()
	_ = e.client.Close()
}

type sessionFlight struct {
	done chan struct{}
}

// sessionPool keeps at most one authenticated SSH connection with an SFTP
// subsystem per machine, so consecutive file operations skip the TCP and SSH
// handshake, credential read, key parse and SFTP start-up.
//
// Authorization is never pooled: every operation still takes an access
// snapshot, policy decision and root checks. A pooled session only holds the
// transport, which is exactly what SetAccess, Remove and Close revoke, and it
// closes itself after idle, on connection loss, and when an operation using it
// is cancelled or times out (the shared socket cannot cancel just one request).
type sessionPool struct {
	mu      sync.Mutex
	idle    time.Duration
	entries map[string]*pooledSession
	flights map[string]*sessionFlight
	// gen changes whenever a machine's sessions are dropped, so a dial that was
	// in flight during the change is discarded instead of cached.
	gen    map[string]uint64
	closed bool
	dial   func(context.Context, Machine) (*ssh.Client, *sftp.Client, error)
}

func newSessionPool(dial func(context.Context, Machine) (*ssh.Client, *sftp.Client, error)) *sessionPool {
	return &sessionPool{idle: sessionIdleTimeout, entries: map[string]*pooledSession{}, flights: map[string]*sessionFlight{}, gen: map[string]uint64{}, dial: dial}
}

// dialSession authenticates against the pinned identity and starts SFTP. The
// returned connection is detached from ctx: ctx only bounds the dial.
func (m *Manager) dialSession(ctx context.Context, v Machine) (*ssh.Client, *sftp.Client, error) {
	cfg, err := m.machineConfig(v)
	if err != nil {
		return nil, nil, err
	}
	client, stop, err := handshakeSSH(ctx, v.Address, cfg, false)
	if err != nil {
		return nil, nil, err
	}
	// ctx still closes the socket here, bounding a stalled SFTP subsystem start.
	fs, err := sftp.NewClient(client)
	if err != nil {
		stop()
		_ = client.Close()
		return nil, nil, err
	}
	if !stop() {
		_ = fs.Close()
		_ = client.Close()
		return nil, nil, context.Cause(ctx)
	}
	return client, fs, nil
}

// sessionLease is one operation's use of a pooled session.
type sessionLease struct {
	FS      *sftp.Client
	pool    *sessionPool
	entry   *pooledSession
	stop    func() bool
	release sync.Once
}

// Release returns the session to the pool. It must run before the operation's
// context is cancelled, otherwise the session is discarded as cancelled.
func (l *sessionLease) Release() {
	l.release.Do(func() {
		l.stop()
		l.pool.release(l.entry)
	})
}

// acquire returns a session for v, dialing one when none is pooled. If ctx ends
// while the lease is held the session is closed and evicted.
func (p *sessionPool) acquire(ctx context.Context, v Machine) (*sessionLease, error) {
	key := keyFor(v)
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, context.Canceled
		}
		var stale *pooledSession
		if e := p.entries[v.ID]; e != nil {
			if e.key == key {
				e.users++
				if e.timer != nil {
					e.timer.Stop()
				}
				p.mu.Unlock()
				return p.lease(ctx, e), nil
			}
			// The machine's pinned identity changed underneath the session.
			delete(p.entries, v.ID)
			e.closed = true
			stale = e
		}
		if f := p.flights[v.ID]; f != nil {
			p.mu.Unlock()
			if stale != nil {
				stale.close()
			}
			select {
			case <-f.done:
				continue
			case <-ctx.Done():
				return nil, context.Cause(ctx)
			}
		}
		f := &sessionFlight{done: make(chan struct{})}
		p.flights[v.ID] = f
		gen := p.gen[v.ID]
		p.mu.Unlock()
		if stale != nil {
			stale.close()
		}
		client, fs, err := p.dial(ctx, v)
		p.mu.Lock()
		delete(p.flights, v.ID)
		close(f.done)
		if err == nil && (p.closed || p.gen[v.ID] != gen) {
			p.mu.Unlock()
			_ = fs.Close()
			_ = client.Close()
			if cause := context.Cause(ctx); cause != nil {
				return nil, cause
			}
			return nil, ErrAccessChanged
		}
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		e := &pooledSession{id: v.ID, key: key, client: client, sftp: fs, users: 1}
		p.entries[v.ID] = e
		p.mu.Unlock()
		// Losing the transport or the SFTP subsystem evicts the session so the
		// next operation dials again instead of failing on a dead handle.
		go func() { _ = client.Wait(); p.evict(e) }()
		go func() { _ = fs.Wait(); p.evict(e) }()
		return p.lease(ctx, e), nil
	}
}

func (p *sessionPool) lease(ctx context.Context, e *pooledSession) *sessionLease {
	return &sessionLease{FS: e.sftp, pool: p, entry: e, stop: context.AfterFunc(ctx, func() { p.evict(e) })}
}

func (p *sessionPool) release(e *pooledSession) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e.users--
	e.lastUsed = time.Now()
	if e.users > 0 || e.closed || p.entries[e.id] != e || p.closed {
		return
	}
	idle := p.idle
	e.timer = time.AfterFunc(idle, func() { p.expire(e, idle) })
}

// expire closes an unused session once it has really been idle for the timeout.
func (p *sessionPool) expire(e *pooledSession, idle time.Duration) {
	p.mu.Lock()
	if e.users > 0 || e.closed || p.entries[e.id] != e || time.Since(e.lastUsed) < idle {
		p.mu.Unlock()
		return
	}
	delete(p.entries, e.id)
	e.closed = true
	p.mu.Unlock()
	e.close()
}

// evict removes and closes one session regardless of use.
func (p *sessionPool) evict(e *pooledSession) {
	p.mu.Lock()
	if p.entries[e.id] == e {
		delete(p.entries, e.id)
	}
	if e.timer != nil {
		e.timer.Stop()
	}
	already := e.closed
	e.closed = true
	p.mu.Unlock()
	if !already {
		e.close()
	}
}

// drop closes the machine's session, including any operation still using it,
// and prevents a concurrent dial from being cached. Callers hold accessMu, so
// the sockets are closed in the background rather than inline.
func (p *sessionPool) drop(id string) {
	p.mu.Lock()
	p.gen[id]++
	e := p.entries[id]
	delete(p.entries, id)
	if e != nil {
		if e.timer != nil {
			e.timer.Stop()
		}
		e.closed = true
	}
	p.mu.Unlock()
	if e != nil {
		go e.close()
	}
}

// closeAll closes every session and refuses new ones.
func (p *sessionPool) closeAll() {
	p.mu.Lock()
	p.closed = true
	entries := p.entries
	p.entries = map[string]*pooledSession{}
	for _, e := range entries {
		if e.timer != nil {
			e.timer.Stop()
		}
		e.closed = true
	}
	p.mu.Unlock()
	for _, e := range entries {
		e.close()
	}
}

// size reports pooled sessions; for tests.
func (p *sessionPool) size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

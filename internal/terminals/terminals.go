// Package terminals manages interactive pseudoterminal sessions behind a
// platform-neutral Backend (ConPTY on Windows).
//
// Lifecycle honesty: sessions live in the daemon process. They survive GUI
// restarts. They do NOT survive daemon termination or a Windows reboot; after
// either, sessions are simply gone and the UI must offer to start new ones.
package terminals

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Backend starts pseudoterminal processes.
type Backend interface {
	Start(spec StartSpec) (Process, error)
	// Shells lists shells detected on this machine.
	Shells() []Shell
}

// Process is a running pty-attached process.
type Process interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Wait(ctx context.Context) (exitCode int, err error)
	Pid() int
	Close() error
}

// StartSpec describes a process to start.
type StartSpec struct {
	Command    string // full command line
	Cwd        string
	Env        []string
	Cols, Rows int
}

// Shell is a selectable shell.
type Shell struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// State of a session.
type State string

const (
	Running State = "running"
	Exited  State = "exited"
)

// Info is a session snapshot.
type Info struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Shell     string     `json:"shell"`
	Cwd       string     `json:"cwd"`
	Cols      int        `json:"cols"`
	Rows      int        `json:"rows"`
	Pid       int        `json:"pid"`
	State     State      `json:"state"`
	ExitCode  *int       `json:"exitCode,omitempty"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Owner     string     `json:"owner,omitempty"` // e.g. "agent:<id>" for agent-owned terminals
}

// Sink receives live output and state changes.
type Sink interface {
	Output(id string, seq uint64, data []byte)
	Changed(info Info)
}

// Manager owns all sessions.
type Manager struct {
	be   Backend
	sink Sink
	max  int
	mu   sync.RWMutex
	sess map[string]*Session
}

// NewManager creates a manager. scrollbackBytes bounds per-session history.
func NewManager(be Backend, sink Sink) *Manager {
	return &Manager{be: be, sink: sink, max: 64, sess: map[string]*Session{}}
}

// Shells lists detected shells.
func (m *Manager) Shells() []Shell { return m.be.Shells() }

// ScrollbackBytes is the per-session history bound.
const ScrollbackBytes = 1 << 20

// Session is one terminal.
type Session struct {
	m    *Manager
	p    Process
	mu   sync.Mutex
	info Info
	buf  []byte // ring of most recent output
	seq  uint64 // total bytes emitted
}

// CreateOptions configures a new session.
type CreateOptions struct {
	Shell   string // Shell.ID, or "" for default
	Cwd     string
	Cols    int
	Rows    int
	Title   string
	Owner   string
	Command string // optional explicit command line overriding Shell
}

// Create starts a session.
func (m *Manager) Create(o CreateOptions) (Info, error) {
	m.mu.RLock()
	n := len(m.sess)
	m.mu.RUnlock()
	if n >= m.max {
		return Info{}, errors.New("terminals: session limit reached")
	}
	if o.Cols <= 0 {
		o.Cols = 120
	}
	if o.Rows <= 0 {
		o.Rows = 30
	}
	cmd := o.Command
	shellName := o.Shell
	if cmd == "" {
		shells := m.be.Shells()
		var sel *Shell
		for i := range shells {
			if (o.Shell == "" && i == 0) || shells[i].ID == o.Shell {
				sel = &shells[i]
				break
			}
		}
		if sel == nil {
			return Info{}, fmt.Errorf("terminals: unknown or unavailable shell %q", o.Shell)
		}
		cmd, shellName = sel.Command, sel.ID
	}
	p, err := m.be.Start(StartSpec{Command: cmd, Cwd: o.Cwd, Cols: o.Cols, Rows: o.Rows})
	if err != nil {
		return Info{}, err
	}
	s := &Session{m: m, p: p, info: Info{ID: uuid.NewString(), Title: o.Title, Shell: shellName, Cwd: o.Cwd,
		Cols: o.Cols, Rows: o.Rows, Pid: p.Pid(), State: Running, StartedAt: time.Now().UTC(), Owner: o.Owner}}
	if s.info.Title == "" {
		s.info.Title = shellName
	}
	m.mu.Lock()
	m.sess[s.info.ID] = s
	m.mu.Unlock()
	go s.pump()
	return s.info, nil
}

func (s *Session) pump() {
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		chunk := make([]byte, 32<<10)
		for {
			n, err := s.p.Read(chunk)
			if n > 0 {
				data := append([]byte(nil), chunk[:n]...)
				s.mu.Lock()
				s.buf = append(s.buf, data...)
				if len(s.buf) > ScrollbackBytes {
					s.buf = append([]byte(nil), s.buf[len(s.buf)-ScrollbackBytes:]...)
				}
				s.seq += uint64(n)
				seq := s.seq
				s.mu.Unlock()
				if s.m.sink != nil {
					s.m.sink.Output(s.info.ID, seq, data)
				}
			}
			if err != nil {
				return
			}
		}
	}()
	// A pty's output pipe stays open after the child exits, so process exit,
	// not read EOF, is the end-of-session signal. Give the reader a moment to
	// drain trailing output, then close the pty, which unblocks Read.
	code, _ := s.p.Wait(context.Background())
	time.Sleep(150 * time.Millisecond)
	s.p.Close()
	<-readDone
	now := time.Now().UTC()
	s.mu.Lock()
	s.info.State, s.info.ExitCode, s.info.EndedAt = Exited, &code, &now
	info := s.info
	s.mu.Unlock()
	if s.m.sink != nil {
		s.m.sink.Changed(info)
	}
}

func (m *Manager) get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sess[id]
	if !ok {
		return nil, fmt.Errorf("terminals: no session %q", id)
	}
	return s, nil
}

// List returns all sessions.
func (m *Manager) List() []Info {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Info, 0, len(m.sess))
	for _, s := range m.sess {
		s.mu.Lock()
		out = append(out, s.info)
		s.mu.Unlock()
	}
	return out
}

// Write sends input (keystrokes, pasted text, or Ctrl-C as "\x03").
func (m *Manager) Write(id string, data []byte) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	running := s.info.State == Running
	s.mu.Unlock()
	if !running {
		return errors.New("terminals: session has exited")
	}
	_, err = s.p.Write(data)
	return err
}

// Resize changes the terminal size.
func (m *Manager) Resize(id string, cols, rows int) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return errors.New("terminals: invalid size")
	}
	if err := s.p.Resize(cols, rows); err != nil {
		return err
	}
	s.mu.Lock()
	s.info.Cols, s.info.Rows = cols, rows
	s.mu.Unlock()
	return nil
}

// Attachment is a scrollback snapshot plus the sequence number it ends at.
// Live Output events with seq <= Seq are already contained in Data.
type Attachment struct {
	Info Info   `json:"info"`
	Data []byte `json:"data"`
	Seq  uint64 `json:"seq"`
}

// Attach returns the scrollback so a (re)connecting client can restore the screen.
func (m *Manager) Attach(id string) (Attachment, error) {
	s, err := m.get(id)
	if err != nil {
		return Attachment{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Attachment{Info: s.info, Data: append([]byte(nil), s.buf...), Seq: s.seq}, nil
}

// Close kills the process (if running) and forgets the session.
func (m *Manager) Close(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.sess, id)
	m.mu.Unlock()
	return s.p.Close()
}

// Shutdown closes every session.
func (m *Manager) Shutdown() {
	for _, i := range m.List() {
		_ = m.Close(i.ID)
	}
}

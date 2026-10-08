package terminals

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// PersistentSession identifies a live tmux session, not a saved process image.
// IDs are scoped to the current Orole home and disappear when the tmux server exits.
type PersistentSession struct {
	ID string `json:"id"`
}

type persistentBackend interface {
	StartPersistent(StartSpec) (Process, error)
	PersistentSessions() ([]PersistentSession, error)
	RestorePersistent(id string, cols, rows int) (Process, error)
}

type persistentProcess interface {
	PersistentID() string
	Terminate() error
}

// PersistentSessions enumerates reconnectable sessions in this home's tmux server.
// Unsupported platforms and missing tmux installations return an explicit error.
func (m *Manager) PersistentSessions() ([]PersistentSession, error) {
	be, ok := m.be.(persistentBackend)
	if !ok {
		return nil, errors.New("terminals: tmux persistence is unavailable on this platform")
	}
	return be.PersistentSessions()
}

// Restore attaches a new PTY to an existing tmux session without relaunching its
// command. Attach remains the separate API for reconnecting a GUI to a daemon PTY.
func (m *Manager) Restore(id string, cols, rows int) (Info, error) {
	be, ok := m.be.(persistentBackend)
	if !ok {
		return Info{}, errors.New("terminals: tmux persistence is unavailable on this platform")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sess {
		s.mu.Lock()
		info := s.info
		s.mu.Unlock()
		if info.PersistentID == id && info.State == Running {
			return info, nil
		}
	}
	if len(m.sess) >= m.max {
		return Info{}, errors.New("terminals: session limit reached")
	}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 30
	}
	p, err := be.RestorePersistent(id, cols, rows)
	if err != nil {
		return Info{}, err
	}
	info := Info{ID: uuid.NewString(), Title: "tmux", Shell: "tmux", Cols: cols, Rows: rows,
		Pid: p.Pid(), State: Running, StartedAt: time.Now().UTC(), PersistentID: id}
	s := &Session{m: m, p: p, info: info}
	m.sess[info.ID] = s
	go s.pump()
	return info, nil
}

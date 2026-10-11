package agents

import "errors"

// Rename gives a task the title the user chose. A user's name is final: automatic naming (the
// harness's own, or Orole's one-shot request) never replaces it afterwards.
func (m *Manager) Rename(id, title string) (Session, error) {
	title = cleanTitle(title)
	if title == "" {
		return Session{}, errors.New("a task title cannot be empty")
	}
	s, err := m.mutate(id, func(s *Session) { s.Title = title })
	if err != nil {
		return s, err
	}
	m.forgetTitle(id)
	m.cfg.Audit("agent.rename", id, "allowed", "")
	return s, nil
}

// SetPinned keeps a task above the others in its project's list.
func (m *Manager) SetPinned(id string, pinned bool) (Session, error) {
	return m.mutate(id, func(s *Session) { s.Pinned = pinned })
}

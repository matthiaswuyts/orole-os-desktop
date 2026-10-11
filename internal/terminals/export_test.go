package terminals

import "time"

// SetExitedPolicy overrides how many exited sessions are kept and for how long.
func (m *Manager) SetExitedPolicy(keep int, ttl time.Duration) {
	m.mu.Lock()
	m.maxExited, m.exitedTTL = keep, ttl
	m.mu.Unlock()
}

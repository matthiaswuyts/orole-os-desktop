package sandbox

import (
	"context"
	"errors"
	"io/fs"
	"os"
)

// Delete stops the bot's VM if any (waiting for a boot in progress), then removes its overlay disk, socket and
// record. Ensure for the bot fails with errDeleting meanwhile. The sandbox log is kept.
func (m *Manager) Delete(ctx context.Context, botID string) error {
	if err := checkBotID(botID); err != nil {
		return err
	}
	m.mu.Lock()
	if m.deleting[botID] {
		m.mu.Unlock()
		return errDeleting
	}
	m.deleting[botID] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.deleting, botID)
		m.mu.Unlock()
	}()

	for {
		m.mu.Lock()
		inst := m.vms[botID]
		if inst == nil {
			m.mu.Unlock()
			break
		}
		switch inst.state {
		case StateStarting:
			ready := inst.ready
			m.mu.Unlock()
			select {
			case <-ready:
			case <-ctx.Done():
				return ctx.Err()
			}
		case StateRunning:
			m.beginStopLocked(inst)
			st := m.statusLocked(botID)
			m.mu.Unlock()
			m.publish(st)
			m.finishStop(inst, errDeleting)
		default:
			gone := inst.gone
			m.mu.Unlock()
			select {
			case <-gone:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	paths := []string{m.cfg.Paths.Overlay(botID), m.cfg.Paths.Socket(botID)}
	rec, found, err := m.store.Get(ctx, botID)
	if err != nil {
		return err
	}
	if found && rec.OverlayPath != paths[0] {
		paths = append(paths, rec.OverlayPath)
	}
	var errs []error
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := m.store.Delete(ctx, botID); err != nil {
		errs = append(errs, err)
	}
	m.mu.Lock()
	delete(m.last, botID)
	delete(m.viewers, botID)
	st := m.statusLocked(botID)
	m.mu.Unlock()
	m.publish(st)
	return errors.Join(errs...)
}

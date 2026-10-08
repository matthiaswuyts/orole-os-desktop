package daemon

import (
	"fmt"
	"os"
	"sync"
)

// RotatingFile is an io.WriteCloser that appends to path and rotates it when it
// would exceed MaxBytes: path -> path.1 -> ... -> path.<Keep>, dropping the oldest.
type RotatingFile struct {
	path     string
	maxBytes int64
	keep     int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// OpenRotating opens (creating) path for appending. keep is the number of rotated files retained.
func OpenRotating(path string, maxBytes int64, keep int) (*RotatingFile, error) {
	r := &RotatingFile{path: path, maxBytes: maxBytes, keep: keep}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

// Write implements io.Writer; a write is never split across files.
func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			// Keep logging to the current file rather than lose lines.
			fmt.Fprintln(os.Stderr, "orole-daemon: log rotation failed:", err)
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *RotatingFile) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	// Shift from the oldest down; rename failures on a missing file are expected.
	_ = os.Remove(fmt.Sprintf("%s.%d", r.path, r.keep))
	for i := r.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	if r.keep >= 1 {
		if err := os.Rename(r.path, r.path+".1"); err != nil {
			_ = r.open() // reopen so logging continues
			return err
		}
	} else if err := os.Remove(r.path); err != nil {
		_ = r.open()
		return err
	}
	return r.open()
}

// Close closes the current file.
func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

//go:build windows

package credentials

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/danieljoos/wincred"
)

type winStore struct{}

// New returns the Windows Credential Manager store (persistence: local machine, per-user).
func New() Store { return winStore{} }

// Credential Manager rejects a blob over CRED_MAX_CREDENTIAL_BLOB_SIZE (2560 bytes) with
// "The stub received bad data". OAuth token sets (access + refresh + client) can exceed that,
// so a larger secret is split: the base credential holds chunkMarker and the count, and the
// pieces live under "<key>/chunk/<i>". Secrets that fit are stored as before, unchanged.
const (
	maxBlob     = 2560
	chunkSize   = 2048
	chunkMarker = "orole-chunked/v1:"
)

func chunkKey(key string, i int) string { return key + "/chunk/" + strconv.Itoa(i) }

func write(key string, blob []byte) error {
	c := wincred.NewGenericCredential(TargetName(key))
	c.CredentialBlob = blob
	c.Persist = wincred.PersistLocalMachine
	return c.Write()
}

func read(key string) ([]byte, error) {
	c, err := wincred.GetGenericCredential(TargetName(key))
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c.CredentialBlob, nil
}

func remove(key string) error {
	c, err := wincred.GetGenericCredential(TargetName(key))
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return nil
		}
		return err
	}
	return c.Delete()
}

// chunkCount reports how many chunks a stored base blob refers to (0: not chunked).
func chunkCount(base []byte) int {
	rest, ok := bytes.CutPrefix(base, []byte(chunkMarker))
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(string(rest))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// dropChunks deletes chunks from..old-1 left behind by a previous, longer value.
func dropChunks(key string, from, old int) error {
	var errs []error
	for i := from; i < old; i++ {
		errs = append(errs, remove(chunkKey(key, i)))
	}
	return errors.Join(errs...)
}

func (winStore) Set(key string, secret []byte) error {
	var old int
	if prev, err := read(key); err == nil {
		old = chunkCount(prev)
	}
	if len(secret) <= maxBlob {
		if err := write(key, secret); err != nil {
			return err
		}
		return dropChunks(key, 0, old)
	}
	n := (len(secret) + chunkSize - 1) / chunkSize
	// Pieces first, base last: a failure part-way never leaves a base pointing at missing pieces.
	for i := range n {
		if err := write(chunkKey(key, i), secret[i*chunkSize:min((i+1)*chunkSize, len(secret))]); err != nil {
			_ = dropChunks(key, 0, i)
			return err
		}
	}
	if err := write(key, []byte(chunkMarker+strconv.Itoa(n))); err != nil {
		_ = dropChunks(key, 0, n)
		return err
	}
	return dropChunks(key, n, old)
}

func (winStore) Get(key string) ([]byte, error) {
	base, err := read(key)
	if err != nil {
		return nil, err
	}
	n := chunkCount(base)
	if n == 0 {
		return base, nil
	}
	out := make([]byte, 0, n*chunkSize)
	for i := range n {
		piece, err := read(chunkKey(key, i))
		if err != nil {
			return nil, fmt.Errorf("credentials: chunk %d of %d for %s: %w", i, n, key, err)
		}
		out = append(out, piece...)
	}
	return out, nil
}

func (winStore) Delete(key string) error {
	base, err := read(key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.Join(dropChunks(key, 0, chunkCount(base)), remove(key))
}

//go:build windows

package credentials

import (
	"errors"

	"github.com/danieljoos/wincred"
)

type winStore struct{}

// New returns the Windows Credential Manager store (persistence: local machine, per-user).
func New() Store { return winStore{} }

func (winStore) Set(key string, secret []byte) error {
	c := wincred.NewGenericCredential(TargetName(key))
	c.CredentialBlob = secret
	c.Persist = wincred.PersistLocalMachine
	return c.Write()
}

func (winStore) Get(key string) ([]byte, error) {
	c, err := wincred.GetGenericCredential(TargetName(key))
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c.CredentialBlob, nil
}

func (winStore) Delete(key string) error {
	c, err := wincred.GetGenericCredential(TargetName(key))
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return nil
		}
		return err
	}
	return c.Delete()
}

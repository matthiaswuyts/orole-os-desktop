//go:build !windows

package credentials

import "errors"

type unsupported struct{}

// New returns a store that reports the platform has no implementation yet.
// Linux (Secret Service) and macOS (Keychain) backends are tracked separately.
func New() Store { return unsupported{} }

var errUnsupported = errors.New("credentials: no OS credential store implemented for this platform")

func (unsupported) Set(string, []byte) error   { return errUnsupported }
func (unsupported) Get(string) ([]byte, error) { return nil, errUnsupported }
func (unsupported) Delete(string) error        { return errUnsupported }

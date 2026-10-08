// Package credentials stores secrets in the OS-protected credential store.
// On Windows that is Credential Manager (DPAPI-protected, per-user).
package credentials

import "errors"

// ErrNotFound is returned when no secret exists for a key.
var ErrNotFound = errors.New("credentials: not found")

// Store is the secret storage abstraction. Implementations never log values.
type Store interface {
	Set(key string, secret []byte) error
	Get(key string) ([]byte, error)
	Delete(key string) error
}

const target = "Orole OS/"

// TargetName returns the OS-level name used for key.
func TargetName(key string) string { return target + key }

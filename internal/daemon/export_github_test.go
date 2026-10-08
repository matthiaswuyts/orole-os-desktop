package daemon

import "github.com/rayorole/orole-os-desktop/internal/credentials"

// SetGitHubTestSeams points the GitHub service at a fake API and credential store.
// It returns a restore func.
func SetGitHubTestSeams(base string, store credentials.Store) func() {
	ob, oc := ghAPIBase, ghCredentialStore
	ghAPIBase, ghCredentialStore = base, func() credentials.Store { return store }
	return func() { ghAPIBase, ghCredentialStore = ob, oc }
}

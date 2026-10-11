package daemon

import (
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/dictation"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// UseDictationBackend makes daemons created afterwards keep provider keys in the returned in-memory
// store and send dictation audio to baseURL instead of OpenAI. restore undoes both.
func UseDictationBackend(baseURL string, doer llm.HTTPDoer) (store *credentials.Memory, restore func()) {
	prevCreds, prevNew := llmCredentialStore, newDictation
	store = credentials.NewMemory()
	llmCredentialStore = func() credentials.Store { return store }
	newDictation = func(s credentials.Store) (*dictation.Service, error) {
		return dictation.New(dictation.Config{Credentials: s, HTTP: doer, BaseURL: baseURL})
	}
	return store, func() { llmCredentialStore, newDictation = prevCreds, prevNew }
}

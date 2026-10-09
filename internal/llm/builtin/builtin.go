// Package builtin assembles the providers that ship with Orole OS.
package builtin

import (
	"github.com/rayorole/orole-os-desktop/internal/llm"
	"github.com/rayorole/orole-os-desktop/internal/llm/openai"
	"github.com/rayorole/orole-os-desktop/internal/llm/openrouter"
)

// Descriptors returns OpenRouter and OpenAI in display order.
func Descriptors() []llm.Descriptor {
	return []llm.Descriptor{openrouter.Descriptor(), openai.Descriptor()}
}

// Registry returns a registry holding the built-in providers.
func Registry() *llm.Registry {
	r, err := llm.NewRegistry(Descriptors()...)
	if err != nil {
		panic(err) // static descriptors: a failure is a programming error
	}
	return r
}

// Package llmtest provides a scripted llm.Provider for tests. Never import it from production code.
package llmtest

import (
	"context"
	"sync"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// Script returns the deltas for one request. The fake appends DeltaStop{StopEnd}
// when the script does not end with DeltaStop or DeltaError.
type Script func(call int, req llm.Request) []llm.Delta

// Fake is a scripted provider. Safe for concurrent use.
type Fake struct {
	IDValue     string
	ModelList   []llm.Model
	Run         Script
	ValidateErr error

	mu       sync.Mutex
	Requests []llm.Request
}

func (f *Fake) ID() string {
	if f.IDValue == "" {
		return "fake"
	}
	return f.IDValue
}

func (f *Fake) Models(context.Context) ([]llm.Model, error) { return f.ModelList, nil }

func (f *Fake) Validate(context.Context) (llm.Account, error) {
	if f.ValidateErr != nil {
		return llm.Account{}, f.ValidateErr
	}
	return llm.Account{Label: "fake"}, nil
}

// Calls returns a copy of the recorded requests.
func (f *Fake) Calls() []llm.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llm.Request(nil), f.Requests...)
}

func (f *Fake) Stream(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	f.mu.Lock()
	n := len(f.Requests)
	f.Requests = append(f.Requests, req)
	f.mu.Unlock()
	var deltas []llm.Delta
	if f.Run != nil {
		deltas = f.Run(n, req)
	}
	if k := len(deltas); k == 0 || (deltas[k-1].Kind != llm.DeltaStop && deltas[k-1].Kind != llm.DeltaError) {
		deltas = append(deltas, llm.Delta{Kind: llm.DeltaStop, Stop: llm.StopEnd})
	}
	out := make(chan llm.Delta)
	go func() {
		defer close(out)
		for _, d := range deltas {
			select {
			case out <- d:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Text is shorthand for a text delta.
func Text(s string) llm.Delta { return llm.Delta{Kind: llm.DeltaText, Text: s} }

// Call is shorthand for a client tool call delta that ends with StopToolCalls.
func Call(id, name, argsJSON string) []llm.Delta {
	return []llm.Delta{
		{Kind: llm.DeltaToolCall, Call: &llm.ToolCall{ID: id, Name: name, Args: []byte(argsJSON)}},
		{Kind: llm.DeltaStop, Stop: llm.StopToolCalls},
	}
}

// Static adapts a fixed llm.Service-like resolver for tests: it returns p for every id.
type Static struct{ P llm.Provider }

func (s Static) Provider(context.Context, string) (llm.Provider, error) { return s.P, nil }

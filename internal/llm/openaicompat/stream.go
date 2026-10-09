package openaicompat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

// errTerminal ends the SSE loop after a terminal delta was already sent.
var errTerminal = errors.New("terminal")

// Stream starts a generation. Failures before the first byte (auth, bad request, a revoked
// manifest) are returned as *llm.Error; later failures are delivered as the terminal DeltaError.
func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	if req.Model == "" {
		return nil, &llm.Error{Code: llm.ErrBadRequest, Message: "no model selected"}
	}
	built, err := p.buildRequest(req)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(built)
	if err != nil {
		return nil, &llm.Error{Code: llm.ErrBadRequest, Message: "request could not be encoded"}
	}
	parent := ctx
	ctx, release := p.bind(ctx)
	hreq, err := p.newRequest(ctx, http.MethodPost, p.m.ChatURL(), payload)
	if err != nil {
		release()
		return nil, err
	}
	hreq.Header.Set("Accept", "text/event-stream")
	resp, err := llm.Do(p.http, hreq, p.key)
	if err != nil {
		release()
		return nil, p.lifeError(err)
	}
	out := make(chan llm.Delta)
	go func() {
		defer close(out)
		defer release()
		defer resp.Body.Close()
		s := &stream{ctx: ctx, emit: llm.Emitter(ctx, out)}
		err := llm.ReadSSE(ctx, resp.Body, 0, s.event)
		if ctx.Err() != nil {
			if parent.Err() == nil && !s.finished {
				// The manifest was revoked or changed mid-stream; the consumer is still listening.
				llm.Emitter(parent, out)(llm.Delta{Kind: llm.DeltaError, Err: &llm.Error{Code: llm.ErrCanceled, Message: "the provider was disabled or changed"}})
			}
			return
		}
		switch {
		case err == nil:
			s.finalize(false)
		case errors.Is(err, bufio.ErrTooLong):
			s.fail(&llm.Error{Code: llm.ErrServer, Message: "the provider sent an oversized stream event"})
		case errors.Is(err, errTerminal):
		default:
			s.fail(llm.NetworkError(err, p.key))
		}
	}()
	return out, nil
}

type chunk struct {
	Error   json.RawMessage `json:"error"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			Refusal          string `json:"refusal"`
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
		PromptDetails    *struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails *struct {
			ReasoningTokens *int64 `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

// stream converts Chat Completions chunks to deltas.
type stream struct {
	ctx      context.Context
	emit     func(llm.Delta) bool
	tools    llm.ToolAssembler
	finish   string
	usage    *llm.Usage
	calls    int
	finished bool // terminal delta sent
}

// send emits d, ending the SSE loop when the consumer is gone.
func (s *stream) send(d llm.Delta) error {
	if !s.emit(d) {
		return s.ctx.Err()
	}
	return nil
}

func (s *stream) fail(err *llm.Error) {
	if s.finished {
		return
	}
	s.finished = true
	s.emit(llm.Delta{Kind: llm.DeltaError, Err: err})
}

// event handles one SSE event.
func (s *stream) event(ev llm.SSEEvent) error {
	data := bytes.TrimSpace(ev.Data)
	if len(data) == 0 {
		return nil
	}
	if string(data) == "[DONE]" {
		s.finalize(true)
		return errTerminal
	}
	var c chunk
	if err := json.Unmarshal(data, &c); err != nil {
		return nil // keep-alive or provider noise; the stream is not corrupt
	}
	if len(c.Error) > 0 && string(c.Error) != "null" {
		s.fail(llm.MapHTTPError(0, nil, data))
		return errTerminal
	}
	if u := c.Usage; u != nil {
		s.usage = &llm.Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens}
		if u.PromptDetails != nil {
			s.usage.CachedTokens = u.PromptDetails.CachedTokens
		}
		if u.CompletionDetails != nil {
			s.usage.ReasoningTokens = u.CompletionDetails.ReasoningTokens
		}
	}
	if len(c.Choices) == 0 {
		return nil
	}
	ch := &c.Choices[0]
	d := &ch.Delta
	reasoning := d.Reasoning
	if reasoning == "" {
		reasoning = d.ReasoningContent
	}
	if reasoning != "" {
		if err := s.send(llm.Delta{Kind: llm.DeltaReasoning, Text: reasoning}); err != nil {
			return err
		}
	}
	if d.Content != "" {
		if err := s.send(llm.Delta{Kind: llm.DeltaText, Text: d.Content}); err != nil {
			return err
		}
	}
	if d.Refusal != "" {
		if err := s.send(llm.Delta{Kind: llm.DeltaText, Text: d.Refusal}); err != nil {
			return err
		}
	}
	for _, tc := range d.ToolCalls {
		key := strconv.Itoa(tc.Index)
		s.tools.Begin(key, tc.ID, tc.Function.Name)
		s.tools.Append(key, tc.Function.Arguments)
	}
	if ch.FinishReason != "" {
		s.finish = ch.FinishReason
		return s.flushCalls()
	}
	return nil
}

// flushCalls emits every completed client tool call.
func (s *stream) flushCalls() error {
	calls, err := s.tools.Drain()
	for i := range calls {
		if e := s.send(llm.Delta{Kind: llm.DeltaToolCall, Call: &calls[i]}); e != nil {
			return e
		}
		s.calls++
	}
	if err != nil {
		s.fail(llm.AsError(err))
		return errTerminal
	}
	return nil
}

// finalize ends a stream that reached [DONE] (done) or hit EOF without it.
func (s *stream) finalize(done bool) {
	if s.finished {
		return
	}
	if !done && s.finish == "" && s.usage == nil && s.calls == 0 {
		s.fail(&llm.Error{Code: llm.ErrNetwork, Message: "the stream ended before the response completed"})
		return
	}
	if s.flushCalls() != nil {
		return
	}
	if s.usage != nil {
		if s.send(llm.Delta{Kind: llm.DeltaUsage, Usage: s.usage}) != nil {
			return
		}
	}
	s.finished = true
	s.emit(llm.Delta{Kind: llm.DeltaStop, Stop: s.stopReason()})
}

func (s *stream) stopReason() llm.StopReason {
	if s.calls > 0 {
		return llm.StopToolCalls
	}
	switch strings.ToLower(s.finish) {
	case "length":
		return llm.StopLength
	case "content_filter":
		return llm.StopFilter
	}
	return llm.StopEnd
}

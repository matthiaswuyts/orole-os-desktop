package openrouter

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

const snippetMax = 1000

// Stream starts a generation. Failures before the first byte (auth, quota, bad request)
// are returned as *llm.Error; later failures are delivered as the terminal DeltaError.
func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	if req.Model == "" {
		return nil, &llm.Error{Code: llm.ErrBadRequest, Message: "no model selected"}
	}
	payload, err := json.Marshal(buildRequest(req))
	if err != nil {
		return nil, &llm.Error{Code: llm.ErrBadRequest, Message: "request could not be encoded"}
	}
	hreq, err := p.newRequest(ctx, http.MethodPost, "/chat/completions", payload)
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Accept", "text/event-stream")
	resp, err := llm.Do(p.http, hreq, p.key)
	if err != nil {
		return nil, err
	}
	out := make(chan llm.Delta)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		s := newStream(ctx, out)
		err := llm.ReadSSE(ctx, resp.Body, 0, s.event)
		if ctx.Err() != nil {
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

// errTerminal ends the SSE loop after a terminal delta was already sent.
var errTerminal = errors.New("terminal")

type chunk struct {
	Error   json.RawMessage `json:"error"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			Refusal          string `json:"refusal"`
			Reasoning        string `json:"reasoning"`
			ReasoningDetails []struct {
				Text    string `json:"text"`
				Summary string `json:"summary"`
			} `json:"reasoning_details"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			Annotations []struct {
				Type        string `json:"type"`
				URLCitation struct {
					URL     string `json:"url"`
					Title   string `json:"title"`
					Content string `json:"content"`
				} `json:"url_citation"`
			} `json:"annotations"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     *int64   `json:"prompt_tokens"`
		CompletionTokens *int64   `json:"completion_tokens"`
		Cost             *float64 `json:"cost"`
		PromptDetails    *struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails *struct {
			ReasoningTokens *int64 `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
		ServerToolUse map[string]int64 `json:"server_tool_use"`
	} `json:"usage"`
}

// stream converts Chat Completions chunks to deltas.
type stream struct {
	ctx      context.Context
	emit     func(llm.Delta) bool
	tools    llm.ToolAssembler
	cites    map[string]struct{}
	finish   string
	usage    *llm.Usage
	served   []llm.ServerTool
	calls    int
	finished bool // terminal delta sent
}

func newStream(ctx context.Context, out chan<- llm.Delta) *stream {
	return &stream{ctx: ctx, emit: llm.Emitter(ctx, out)}
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
		s.usage = &llm.Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, CostUSD: u.Cost}
		if u.PromptDetails != nil {
			s.usage.CachedTokens = u.PromptDetails.CachedTokens
		}
		if u.CompletionDetails != nil {
			s.usage.ReasoningTokens = u.CompletionDetails.ReasoningTokens
		}
		s.served = serverTools(u.ServerToolUse)
	}
	if len(c.Choices) == 0 {
		return nil
	}
	ch := &c.Choices[0]
	d := &ch.Delta
	reasoning := d.Reasoning
	if reasoning == "" {
		for _, rd := range d.ReasoningDetails {
			reasoning += rd.Text + rd.Summary
		}
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
	for _, a := range d.Annotations {
		if a.Type != "url_citation" || a.URLCitation.URL == "" {
			continue
		}
		if _, dup := s.cites[a.URLCitation.URL]; dup {
			continue
		}
		if s.cites == nil {
			s.cites = map[string]struct{}{}
		}
		s.cites[a.URLCitation.URL] = struct{}{}
		cit := &llm.Citation{URL: a.URLCitation.URL, Title: a.URLCitation.Title, Snippet: llm.Truncate(a.URLCitation.Content, snippetMax)}
		if err := s.send(llm.Delta{Kind: llm.DeltaCitation, Citation: cit}); err != nil {
			return err
		}
	}
	if ch.FinishReason != "" {
		s.finish = ch.FinishReason
		return s.flushCalls()
	}
	return nil
}

// serverTools reports hosted tool activity from usage.server_tool_use (the stream exposes
// no per-call events for server tools).
func serverTools(m map[string]int64) []llm.ServerTool {
	var out []llm.ServerTool
	for _, name := range []string{llm.HostedWebSearch, llm.HostedWebFetch, llm.HostedDatetime} {
		n := m[name+"_requests"]
		if n <= 0 {
			continue
		}
		detail := strconv.FormatInt(n, 10) + " call"
		if n != 1 {
			detail += "s"
		}
		out = append(out, llm.ServerTool{ID: name, Tool: name, Status: "done", Detail: detail})
	}
	return out
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
	for i := range s.served {
		if s.send(llm.Delta{Kind: llm.DeltaServer, Server: &s.served[i]}) != nil {
			return
		}
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
	case "tool_calls", "function_call":
		// finish_reason without any assembled call: nothing for the engine to run.
		return llm.StopEnd
	}
	return llm.StopEnd
}

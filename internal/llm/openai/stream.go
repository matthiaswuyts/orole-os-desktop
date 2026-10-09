package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rayorole/orole-os-desktop/internal/llm"
)

const snippetMax = 1000

// Stream starts a generation. Failures before the first byte are returned as *llm.Error;
// later failures are delivered as the terminal DeltaError.
func (p *Provider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	if req.Model == "" {
		return nil, &llm.Error{Code: llm.ErrBadRequest, Message: "no model selected"}
	}
	resp, err := p.open(ctx, req, true)
	var le *llm.Error
	if errors.As(err, &le) && le.Code == llm.ErrBadRequest && strings.Contains(strings.ToLower(le.Message), "summar") {
		// The organization may not be allowed reasoning summaries; retry without them.
		resp, err = p.open(ctx, req, false)
	}
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
			s.fail(&llm.Error{Code: llm.ErrNetwork, Message: "the stream ended before the response completed"})
		case errors.Is(err, bufio.ErrTooLong):
			s.fail(&llm.Error{Code: llm.ErrServer, Message: "the provider sent an oversized stream event"})
		case errors.Is(err, errTerminal):
		default:
			s.fail(llm.NetworkError(err, p.key))
		}
	}()
	return out, nil
}

func (p *Provider) open(ctx context.Context, req llm.Request, summary bool) (*http.Response, error) {
	payload, err := json.Marshal(buildRequest(req, summary))
	if err != nil {
		return nil, &llm.Error{Code: llm.ErrBadRequest, Message: "request could not be encoded"}
	}
	hreq, err := p.newRequest(ctx, http.MethodPost, "/responses", payload)
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Accept", "text/event-stream")
	return llm.Do(p.http, hreq, p.key)
}

var errTerminal = errors.New("terminal")

type annotation struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type outputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Status    string `json:"status"`
	Action    *struct {
		Type    string   `json:"type"`
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
		URL     string   `json:"url"`
	} `json:"action"`
	Content []struct {
		Annotations []annotation `json:"annotations"`
	} `json:"content"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type event struct {
	Type         string      `json:"type"`
	Delta        string      `json:"delta"`
	ItemID       string      `json:"item_id"`
	SummaryIndex int         `json:"summary_index"`
	Arguments    string      `json:"arguments"`
	Item         *outputItem `json:"item"`
	Annotation   *annotation `json:"annotation"`
	Response     *struct {
		Error             *apiError `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Usage *struct {
			InputTokens        *int64 `json:"input_tokens"`
			OutputTokens       *int64 `json:"output_tokens"`
			InputTokensDetails *struct {
				CachedTokens *int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputTokensDetails *struct {
				ReasoningTokens *int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	} `json:"response"`
	// "error" events carry code and message at the top level.
	Code    string `json:"code"`
	Message string `json:"message"`
}

type stream struct {
	ctx      context.Context
	emit     func(llm.Delta) bool
	tools    llm.ToolAssembler
	cites    map[string]struct{}
	calls    int
	finished bool
}

func newStream(ctx context.Context, out chan<- llm.Delta) *stream {
	return &stream{ctx: ctx, emit: llm.Emitter(ctx, out)}
}

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

func (s *stream) event(ev llm.SSEEvent) error {
	var e event
	if err := json.Unmarshal(ev.Data, &e); err != nil {
		return nil
	}
	switch e.Type {
	case "response.output_text.delta", "response.refusal.delta":
		if e.Delta != "" {
			return s.send(llm.Delta{Kind: llm.DeltaText, Text: e.Delta})
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if e.Delta != "" {
			return s.send(llm.Delta{Kind: llm.DeltaReasoning, Text: e.Delta})
		}
	case "response.reasoning_summary_part.added":
		if e.SummaryIndex > 0 { // separate summary paragraphs
			return s.send(llm.Delta{Kind: llm.DeltaReasoning, Text: "\n\n"})
		}
	case "response.output_item.added":
		return s.itemAdded(e.Item)
	case "response.function_call_arguments.delta":
		s.tools.Append(e.ItemID, e.Delta)
	case "response.function_call_arguments.done":
		if e.Arguments != "" {
			s.tools.SetArgs(e.ItemID, e.Arguments)
		}
	case "response.output_item.done":
		return s.itemDone(e.Item)
	case "response.output_text.annotation.added":
		if e.Annotation != nil {
			return s.cite(*e.Annotation)
		}
	case "response.completed":
		return s.complete(&e, llm.StopEnd)
	case "response.incomplete":
		stop := llm.StopLength
		if e.Response != nil && e.Response.IncompleteDetails != nil && e.Response.IncompleteDetails.Reason == "content_filter" {
			stop = llm.StopFilter
		}
		return s.complete(&e, stop)
	case "response.failed":
		var ae *apiError
		if e.Response != nil {
			ae = e.Response.Error
		}
		s.fail(mapAPIError(ae))
		return errTerminal
	case "error":
		s.fail(mapAPIError(&apiError{Code: e.Code, Message: e.Message}))
		return errTerminal
	}
	return nil
}

func mapAPIError(ae *apiError) *llm.Error {
	if ae == nil {
		return &llm.Error{Code: llm.ErrServer, Message: "the response failed"}
	}
	body, _ := json.Marshal(map[string]any{"error": ae})
	return llm.MapHTTPError(0, nil, body)
}

func (s *stream) itemAdded(it *outputItem) error {
	if it == nil {
		return nil
	}
	switch it.Type {
	case "function_call":
		s.tools.Begin(it.ID, it.CallID, it.Name)
		if it.Arguments != "" {
			s.tools.SetArgs(it.ID, it.Arguments)
		}
	case "web_search_call":
		return s.send(llm.Delta{Kind: llm.DeltaServer, Server: &llm.ServerTool{ID: it.ID, Tool: llm.HostedWebSearch, Status: "running", Detail: searchDetail(it)}})
	}
	return nil
}

func (s *stream) itemDone(it *outputItem) error {
	if it == nil {
		return nil
	}
	switch it.Type {
	case "function_call":
		s.tools.Begin(it.ID, it.CallID, it.Name)
		if it.Arguments != "" {
			s.tools.SetArgs(it.ID, it.Arguments)
		}
		call, ok, err := s.tools.Take(it.ID)
		if err != nil {
			s.fail(llm.AsError(err))
			return errTerminal
		}
		if ok {
			s.calls++
			return s.send(llm.Delta{Kind: llm.DeltaToolCall, Call: &call})
		}
	case "web_search_call":
		status := "done"
		if it.Status == "failed" {
			status = "error"
		}
		return s.send(llm.Delta{Kind: llm.DeltaServer, Server: &llm.ServerTool{ID: it.ID, Tool: llm.HostedWebSearch, Status: status, Detail: searchDetail(it)}})
	case "message":
		for _, c := range it.Content {
			for _, a := range c.Annotations {
				if err := s.cite(a); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func searchDetail(it *outputItem) string {
	if it.Action == nil {
		return ""
	}
	switch {
	case it.Action.Query != "":
		return it.Action.Query
	case len(it.Action.Queries) > 0:
		return strings.Join(it.Action.Queries, ", ")
	}
	return it.Action.URL
}

func (s *stream) cite(a annotation) error {
	if a.Type != "url_citation" || a.URL == "" {
		return nil
	}
	if _, dup := s.cites[a.URL]; dup {
		return nil
	}
	if s.cites == nil {
		s.cites = map[string]struct{}{}
	}
	s.cites[a.URL] = struct{}{}
	return s.send(llm.Delta{Kind: llm.DeltaCitation, Citation: &llm.Citation{URL: a.URL, Title: llm.Truncate(a.Title, snippetMax)}})
}

// complete flushes unfinished calls, reports usage and ends the stream.
func (s *stream) complete(e *event, stop llm.StopReason) error {
	calls, err := s.tools.Drain()
	for i := range calls {
		s.calls++
		if sendErr := s.send(llm.Delta{Kind: llm.DeltaToolCall, Call: &calls[i]}); sendErr != nil {
			return sendErr
		}
	}
	if err != nil {
		s.fail(llm.AsError(err))
		return errTerminal
	}
	if e.Response != nil && e.Response.Usage != nil {
		u := e.Response.Usage
		usage := &llm.Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
		if u.InputTokensDetails != nil {
			usage.CachedTokens = u.InputTokensDetails.CachedTokens
		}
		if u.OutputTokensDetails != nil {
			usage.ReasoningTokens = u.OutputTokensDetails.ReasoningTokens
		}
		if err := s.send(llm.Delta{Kind: llm.DeltaUsage, Usage: usage}); err != nil {
			return err
		}
	}
	if s.calls > 0 && stop == llm.StopEnd {
		stop = llm.StopToolCalls
	}
	s.finished = true
	s.emit(llm.Delta{Kind: llm.DeltaStop, Stop: stop})
	return errTerminal
}

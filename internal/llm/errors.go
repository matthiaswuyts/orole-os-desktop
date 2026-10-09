package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxErrorMessage = 500

// keyLike matches provider API keys that might be echoed back inside an error body.
var keyLike = regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`)

// Redact removes every secret (and anything shaped like a provider key) from s.
func Redact(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "[redacted]")
		}
	}
	return keyLike.ReplaceAllString(s, "[redacted]")
}

// MapHTTPError converts a failed provider HTTP response into an *Error. status may be 0
// when only an in-stream error body is known; a numeric error.code is then used. h may be nil.
// Both OpenAI ({"error":{"message","type","code"}}) and OpenRouter ({"error":{"code":402,...}})
// shapes are understood. secrets are scrubbed from the resulting message.
func MapHTTPError(status int, h http.Header, body []byte, secrets ...string) *Error {
	msg, code, typ := parseErrorBody(body)
	if status == 0 {
		if n, err := strconv.Atoi(code); err == nil {
			status = n
		}
	}
	low := strings.ToLower(msg + " " + code + " " + typ)
	e := &Error{Status: status}
	switch {
	case contextLengthMessage(low):
		e.Code = ErrContextLength
	case status == http.StatusUnauthorized:
		e.Code = ErrAuth
	case status == http.StatusPaymentRequired, strings.Contains(low, "insufficient_quota"), strings.Contains(low, "exceeded your current quota"), strings.Contains(low, "billing"), strings.Contains(low, "insufficient credits"):
		e.Code = ErrQuota
	case status == http.StatusTooManyRequests:
		e.Code = ErrRateLimit
	case strings.Contains(low, "content_filter"), strings.Contains(low, "content_policy"), strings.Contains(low, "moderation"), strings.Contains(low, "flagged"), strings.Contains(low, "guardrail"):
		e.Code = ErrFilter
	case strings.Contains(low, "rate_limit"):
		e.Code = ErrRateLimit
	case strings.Contains(low, "invalid_api_key"), strings.Contains(low, "authentication"):
		e.Code = ErrAuth
	case status == http.StatusForbidden:
		e.Code = ErrAuth
	case status >= 500, status == http.StatusRequestTimeout, status == 529:
		e.Code = ErrServer
	case status == 0:
		e.Code = ErrServer
	default:
		e.Code = ErrBadRequest
	}
	if e.Code == ErrRateLimit || e.Code == ErrServer {
		e.RetryAfter = retryAfter(h)
	}
	if msg == "" {
		if status != 0 {
			msg = fmt.Sprintf("HTTP %d %s", status, http.StatusText(status))
		} else {
			msg = "provider error"
		}
	}
	e.Message = clip(Redact(msg, secrets...))
	return e
}

func contextLengthMessage(low string) bool {
	for _, k := range []string{
		"context_length_exceeded", "context length", "context window", "maximum context",
		"prompt is too long", "input is too long", "too many tokens", "reduce the length",
		"exceeds the maximum", "token limit",
	} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

func clip(s string) string { return Truncate(s, maxErrorMessage) }

// parseErrorBody extracts message, code and type from the supported error shapes.
func parseErrorBody(body []byte) (msg, code, typ string) {
	var top struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  string          `json:"detail"`
	}
	if json.Unmarshal(body, &top) != nil {
		return strings.TrimSpace(string(body[:min(len(body), maxErrorMessage)])), "", ""
	}
	if len(top.Error) > 0 {
		var s string
		if json.Unmarshal(top.Error, &s) == nil {
			return s, "", ""
		}
		var inner struct {
			Message  string          `json:"message"`
			Type     string          `json:"type"`
			Code     json.RawMessage `json:"code"`
			Metadata struct {
				Raw          json.RawMessage `json:"raw"`
				ProviderName string          `json:"provider_name"`
			} `json:"metadata"`
		}
		if json.Unmarshal(top.Error, &inner) == nil {
			msg, typ = inner.Message, inner.Type
			code = strings.Trim(string(inner.Code), `"`)
			if code == "null" {
				code = ""
			}
			// OpenRouter hides the upstream explanation in metadata.raw; it carries the
			// real reason (for example the context-length text) for generic 400s.
			if raw := rawText(inner.Metadata.Raw); raw != "" && !strings.Contains(msg, raw) {
				msg = strings.TrimSpace(msg + ": " + raw)
			}
			return msg, code, typ
		}
	}
	if top.Message != "" {
		return top.Message, "", ""
	}
	return top.Detail, "", ""
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		// raw is frequently itself a JSON document with an error message.
		if m, _, _ := parseErrorBody([]byte(s)); m != "" && m != s {
			return m
		}
		return s
	}
	if m, _, _ := parseErrorBody(raw); m != "" {
		return m
	}
	return ""
}

func retryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
		return time.Duration(n * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// NetworkError converts a transport failure into an *Error without leaking secrets.
func NetworkError(err error, secrets ...string) *Error {
	if err == nil {
		return nil
	}
	var le *Error
	if errors.As(err, &le) {
		return le
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: ErrCanceled, Message: "canceled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: ErrNetwork, Message: "the request timed out"}
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return &Error{Code: ErrNetwork, Message: clip(Redact(err.Error(), secrets...))}
}

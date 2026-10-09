package llm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// RequestTimeout bounds short non-streaming calls (catalogue, validation, key exchange).
	RequestTimeout = 30 * time.Second
	maxJSONBody    = 16 << 20
	maxErrorBody   = 64 << 10
)

// NewHTTPClient returns a client with connect/TLS/header timeouts and deliberately no
// total timeout: streams may legitimately run for minutes; callers cancel via context.
func NewHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute,
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
	}}
}

// Do sends req and returns a 2xx response whose body the caller must close. Transport
// failures and non-2xx statuses become *Error with secrets scrubbed.
func Do(doer HTTPDoer, req *http.Request, secrets ...string) (*http.Response, error) {
	resp, err := doer.Do(req)
	if err != nil {
		return nil, NetworkError(err, secrets...)
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return nil, MapHTTPError(resp.StatusCode, resp.Header, body, secrets...)
}

// DoJSON sends req, requires 2xx and decodes the (size-bounded) JSON body into out.
// A nil out discards the body.
func DoJSON(doer HTTPDoer, req *http.Request, out any, secrets ...string) error {
	resp, err := Do(doer, req, secrets...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxJSONBody))
		return nil
	}
	lr := &io.LimitedReader{R: resp.Body, N: maxJSONBody + 1}
	if err := json.NewDecoder(lr).Decode(out); err != nil {
		if lr.N <= 0 {
			return &Error{Code: ErrServer, Message: "provider response too large"}
		}
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return NetworkError(ctxErr)
		}
		return &Error{Code: ErrServer, Message: "provider returned an unreadable response", Status: resp.StatusCode}
	}
	return nil
}

// NewPKCE returns a fresh RFC 7636 verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", fmt.Errorf("pkce: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b[:])
	return verifier, PKCEChallenge(verifier), nil
}

// PKCEChallenge is base64url(sha256(verifier)) without padding.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Emitter delivers deltas to a consumer that may stop reading: emit reports false once
// ctx is done, so producer goroutines always terminate.
func Emitter(ctx context.Context, out chan<- Delta) func(Delta) bool {
	return func(d Delta) bool {
		select {
		case out <- d:
			return true
		case <-ctx.Done():
			return false
		}
	}
}

// ErrorDelta wraps err as the terminal error delta.
func ErrorDelta(err error) Delta { return Delta{Kind: DeltaError, Err: AsError(err)} }

// DataURL encodes data as a base64 data URL, sniffing the media type when mime is empty.
func DataURL(mime string, data []byte) string {
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	var b strings.Builder
	b.Grow(len("data:") + len(mime) + len(";base64,") + base64.StdEncoding.EncodedLen(len(data)))
	b.WriteString("data:")
	b.WriteString(mime)
	b.WriteString(";base64,")
	enc := base64.NewEncoder(base64.StdEncoding, &b)
	enc.Write(data)
	enc.Close()
	return b.String()
}

// Truncate shortens s to at most n bytes on a rune boundary, appending an ellipsis when cut.
func Truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

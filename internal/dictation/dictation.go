// Package dictation turns a short microphone recording into text for the composers.
//
// The audio is sent to OpenAI's /audio/transcriptions endpoint with the user's own OpenAI API
// key, which is read from the OS credential store for every request and never leaves this
// package: not in results, errors, logs or the database. Neither the audio nor the transcript
// is ever logged or persisted here. The webview's Web Speech API is not used because the
// packaged WebView2 has no speech service behind it.
package dictation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

const (
	// ModelPrimary is tried first; ModelFallback is used when the account cannot use it.
	ModelPrimary  = "gpt-4o-mini-transcribe"
	ModelFallback = "whisper-1"

	// DefaultBaseURL is the OpenAI API root. Only tests change it.
	DefaultBaseURL = "https://api.openai.com/v1"

	// MaxAudioBytes bounds one recording. It keeps the base64 request inside the IPC frame limit
	// (4 MiB) with room to spare; 120 s of speech-grade Opus is a fraction of it.
	MaxAudioBytes = 2 << 20
	// MaxSeconds bounds one recording's length. The frontend stops recording there.
	MaxSeconds = 120
	// durationGrace absorbs the gap between the UI timer and the recorder's own clock.
	durationGrace = 5 * time.Second

	// requestTimeout stays under the daemon's 30 s per-call deadline so a slow upstream is
	// reported as such instead of as a generic timeout.
	requestTimeout = 25 * time.Second
	maxResponse    = 1 << 20
	maxErrorBody   = 64 << 10
	maxInFlight    = 2
)

// Code classifies a failure for the UI.
type Code string

const (
	CodeNotConnected Code = "not_connected" // no OpenAI key stored
	CodeAuth         Code = "auth"          // OpenAI rejected the key
	CodeQuota        Code = "quota"
	CodeRateLimit    Code = "rate_limit"
	CodeNetwork      Code = "network"
	CodeTimeout      Code = "timeout"
	CodeServer       Code = "server"
	CodeRejected     Code = "rejected"    // OpenAI refused this recording
	CodeTooLarge     Code = "too_large"   // over MaxAudioBytes
	CodeTooLong      Code = "too_long"    // over MaxSeconds
	CodeNoAudio      Code = "no_audio"    // nothing was recorded
	CodeUnsupported  Code = "unsupported" // audio format not accepted
	CodeBusy         Code = "busy"
	CodeCanceled     Code = "canceled"
	CodeCredentials  Code = "credentials" // the credential store failed
)

// Error is the only error type Transcribe returns. Message is safe to show as is and never
// contains the API key.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }

func fail(code Code, msg string) *Error { return &Error{Code: code, Message: msg} }

// ErrNotConnected is returned when no OpenAI key is stored.
var ErrNotConnected = fail(CodeNotConnected, "Dictation needs an OpenAI API key. Connect OpenAI in Settings > AI providers.")

// Availability tells the UI whether dictation can run and what limits apply.
type Availability struct {
	Ready      bool   `json:"ready"`
	Provider   string `json:"provider"`
	MaxSeconds int    `json:"maxSeconds"`
	MaxBytes   int    `json:"maxBytes"`
}

// TranscribeRequest is one recording. DataBase64 is the raw container (webm, ogg, mp4, ...) the
// browser's MediaRecorder produced.
type TranscribeRequest struct {
	MimeType   string `json:"mimeType"`
	DataBase64 string `json:"dataBase64"`
	DurationMs int    `json:"durationMs"`
}

// Transcript is the recognized text. Text is empty when nothing intelligible was said.
type Transcript struct {
	Text  string `json:"text"`
	Model string `json:"model"`
}

// Config wires a Service.
type Config struct {
	Credentials credentials.Store
	// HTTP is the transport; nil = llm.NewHTTPClient().
	HTTP llm.HTTPDoer
	// BaseURL is the API root; empty = DefaultBaseURL.
	BaseURL string
}

// Service transcribes recordings. It keeps no key: it is read from the credential store per call.
type Service struct {
	creds credentials.Store
	http  llm.HTTPDoer
	base  string
	slots chan struct{}
}

// New builds a Service. It performs no network call.
func New(cfg Config) (*Service, error) {
	if cfg.Credentials == nil {
		return nil, errors.New("dictation: a credential store is required")
	}
	s := &Service{creds: cfg.Credentials, http: cfg.HTTP, base: strings.TrimRight(cfg.BaseURL, "/"), slots: make(chan struct{}, maxInFlight)}
	if s.http == nil {
		s.http = llm.NewHTTPClient()
	}
	if s.base == "" {
		s.base = DefaultBaseURL
	}
	return s, nil
}

// key reads the stored OpenAI key just now.
func (s *Service) key() (string, error) {
	raw, err := s.creds.Get(llm.CredentialKey(llm.ProviderOpenAI))
	if errors.Is(err, credentials.ErrNotFound) {
		return "", ErrNotConnected
	}
	if err != nil {
		return "", fail(CodeCredentials, "The credential store is unavailable, so dictation cannot use your OpenAI key.")
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", ErrNotConnected
	}
	return key, nil
}

// Status reports whether an OpenAI key is stored. It does not contact OpenAI.
func (s *Service) Status() (Availability, error) {
	a := Availability{Provider: "OpenAI", MaxSeconds: MaxSeconds, MaxBytes: MaxAudioBytes}
	_, err := s.key()
	var e *Error
	switch {
	case err == nil:
		a.Ready = true
	case errors.As(err, &e) && e.Code == CodeNotConnected:
	default:
		return a, err
	}
	return a, nil
}

// extensions maps the audio containers OpenAI accepts and browsers record to a file extension;
// OpenAI sniffs the format from the file name.
var extensions = map[string]string{
	"audio/webm":  "webm",
	"video/webm":  "webm",
	"audio/ogg":   "ogg",
	"audio/mp4":   "mp4",
	"audio/mpeg":  "mp3",
	"audio/mp3":   "mp3",
	"audio/wav":   "wav",
	"audio/x-wav": "wav",
	"audio/wave":  "wav",
	"audio/flac":  "flac",
}

// prepare validates a request and returns the decoded audio, its content type and extension.
func prepare(req TranscribeRequest) (audio []byte, contentType, ext string, err *Error) {
	mt, _, perr := mime.ParseMediaType(strings.TrimSpace(req.MimeType))
	if perr != nil {
		return nil, "", "", fail(CodeUnsupported, "This recording format is not supported for dictation.")
	}
	ext, ok := extensions[strings.ToLower(mt)]
	if !ok {
		return nil, "", "", fail(CodeUnsupported, "This recording format is not supported for dictation.")
	}
	if time.Duration(req.DurationMs)*time.Millisecond > MaxSeconds*time.Second+durationGrace {
		return nil, "", "", fail(CodeTooLong, fmt.Sprintf("That recording is longer than %d seconds. Record a shorter message.", MaxSeconds))
	}
	tooLarge := fail(CodeTooLarge, fmt.Sprintf("That recording is larger than %d MB. Record a shorter message.", MaxAudioBytes>>20))
	if len(req.DataBase64) > base64.StdEncoding.EncodedLen(MaxAudioBytes) {
		return nil, "", "", tooLarge
	}
	audio, derr := base64.StdEncoding.DecodeString(req.DataBase64)
	if derr != nil {
		return nil, "", "", fail(CodeNoAudio, "The recording could not be read. Try again.")
	}
	if len(audio) == 0 {
		return nil, "", "", fail(CodeNoAudio, "No audio was recorded. Try again.")
	}
	if len(audio) > MaxAudioBytes {
		return nil, "", "", tooLarge
	}
	return audio, mt, ext, nil
}

func multipartBody(model, contentType, ext string, audio []byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", model) // writes to a bytes.Buffer cannot fail
	_ = mw.WriteField("response_format", "json")
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="dictation.%s"`, ext))
	h.Set("Content-Type", contentType)
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(audio)
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

// Transcribe sends one recording to OpenAI and returns the text. gpt-4o-mini-transcribe is
// tried first; when the account cannot use that model, whisper-1 is used instead.
func (s *Service) Transcribe(ctx context.Context, req TranscribeRequest) (Transcript, error) {
	audio, contentType, ext, perr := prepare(req)
	if perr != nil {
		return Transcript{}, perr
	}
	key, err := s.key()
	if err != nil {
		return Transcript{}, err
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return Transcript{}, fail(CodeBusy, "Dictation is already transcribing other recordings. Try again in a moment.")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	for _, model := range []string{ModelPrimary, ModelFallback} {
		text, unavailable, err := s.post(ctx, key, model, contentType, ext, audio)
		if unavailable {
			continue
		}
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return Transcript{}, fail(CodeTimeout, "OpenAI took too long to transcribe. Try a shorter recording.")
			}
			return Transcript{}, err
		}
		return Transcript{Text: text, Model: model}, nil
	}
	return Transcript{}, fail(CodeRejected, "OpenAI has no transcription model available for this account.")
}

// post makes one transcription request. unavailable reports that the model is not usable by this
// account, so the caller may try another one.
func (s *Service) post(ctx context.Context, key, model, contentType, ext string, audio []byte) (text string, unavailable bool, err error) {
	body, formType := multipartBody(model, contentType, ext, audio)
	req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/audio/transcriptions", body)
	if rerr != nil {
		return "", false, userError(llm.NetworkError(rerr, key))
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", formType)
	resp, derr := s.http.Do(req)
	if derr != nil {
		return "", false, userError(llm.NetworkError(derr, key))
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if modelUnavailable(resp.StatusCode, raw) {
			return "", true, nil
		}
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			return "", false, fail(CodeTooLarge, "OpenAI rejected the recording as too large. Record a shorter message.")
		}
		return "", false, userError(llm.MapHTTPError(resp.StatusCode, resp.Header, raw, key))
	}
	var out struct {
		Text string `json:"text"`
	}
	lr := &io.LimitedReader{R: resp.Body, N: maxResponse + 1}
	if err := json.NewDecoder(lr).Decode(&out); err != nil {
		if ctx.Err() != nil {
			return "", false, userError(llm.NetworkError(ctx.Err()))
		}
		return "", false, fail(CodeServer, "OpenAI returned a response dictation could not read. Try again.")
	}
	return strings.TrimSpace(out.Text), false, nil
}

// modelUnavailable recognises "this account has no access to / there is no such model".
func modelUnavailable(status int, body []byte) bool {
	switch status {
	case http.StatusNotFound:
		return true
	case http.StatusBadRequest, http.StatusForbidden:
	default:
		return false
	}
	low := strings.ToLower(string(body))
	return strings.Contains(low, "model_not_found") ||
		strings.Contains(low, "does not have access to model") ||
		(strings.Contains(low, "model") && strings.Contains(low, "does not exist"))
}

// userError turns a provider failure into a sentence for the user. The provider's own message
// (already scrubbed of the key) is appended only for refusals, where it explains the cause.
func userError(le *llm.Error) *Error {
	switch le.Code {
	case llm.ErrAuth:
		return fail(CodeAuth, "OpenAI rejected your API key. Reconnect OpenAI in Settings > AI providers.")
	case llm.ErrQuota:
		return fail(CodeQuota, "Your OpenAI account is out of credit or over its quota. Check billing at platform.openai.com.")
	case llm.ErrRateLimit:
		return fail(CodeRateLimit, "OpenAI is rate limiting requests. Wait a moment and try again.")
	case llm.ErrNetwork:
		return fail(CodeNetwork, "Could not reach OpenAI to transcribe. Check your connection and try again.")
	case llm.ErrServer:
		return fail(CodeServer, "OpenAI had a problem transcribing this recording. Try again.")
	case llm.ErrCanceled:
		return fail(CodeCanceled, "Transcription was cancelled.")
	}
	return fail(CodeRejected, "OpenAI could not transcribe this recording: "+le.Message)
}

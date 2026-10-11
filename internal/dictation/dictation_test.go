package dictation

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

const testKey = "sk-proj-0123456789abcdefSECRET1234"

var audio = []byte("OggS-not-really-opus-but-bytes-are-bytes")

func request(data []byte) TranscribeRequest {
	return TranscribeRequest{MimeType: "audio/webm;codecs=opus", DataBase64: base64.StdEncoding.EncodeToString(data), DurationMs: 4000}
}

// setup returns a service pointed at an httptest server running h, with the key stored.
func setup(t *testing.T, h http.HandlerFunc) (*Service, *credentials.Memory) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	store := credentials.NewMemory()
	if err := store.Set(llm.CredentialKey(llm.ProviderOpenAI), []byte(testKey)); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{Credentials: store, HTTP: srv.Client(), BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return s, store
}

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func code(t *testing.T, err error) Code {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	return e.Code
}

func noSecret(t *testing.T, err error) {
	t.Helper()
	if err != nil && (strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "SECRET1234") || strings.Contains(err.Error(), "sk-")) {
		t.Errorf("error leaks the key: %q", err)
	}
}

func TestTranscribeSendsAuthenticatedMultipartAndReturnsTrimmedText(t *testing.T) {
	var (
		mu     sync.Mutex
		path   string
		auth   string
		fields = map[string]string{}
		fname  string
		ftype  string
		fdata  []byte
	)
	s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Error(err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FormName() == "file" {
				fname, ftype, fdata = p.FileName(), p.Header.Get("Content-Type"), b
			} else {
				fields[p.FormName()] = string(b)
			}
		}
		reply(w, 200, `{"text":"  Hello from the microphone.\n"}`)
	})
	got, err := s.Transcribe(context.Background(), request(audio))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Hello from the microphone." || got.Model != ModelPrimary {
		t.Fatalf("got %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if path != "/v1/audio/transcriptions" || auth != "Bearer "+testKey {
		t.Errorf("path %q auth %q", path, auth)
	}
	if fields["model"] != ModelPrimary || fields["response_format"] != "json" {
		t.Errorf("fields %v", fields)
	}
	if fname != "dictation.webm" || ftype != "audio/webm;codecs=opus" && ftype != "audio/webm" || !bytes.Equal(fdata, audio) {
		t.Errorf("file %q %q %d bytes", fname, ftype, len(fdata))
	}
}

func TestTranscribeMapsProviderFailuresWithoutLeakingTheKey(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   Code
	}{
		{"unauthorized", 401, `{"error":{"message":"Incorrect API key provided: ` + testKey + `.","type":"invalid_request_error","code":"invalid_api_key"}}`, CodeAuth},
		{"forbidden", 403, `{"error":{"message":"Forbidden for ` + testKey + `"}}`, CodeAuth},
		{"rate limited", 429, `{"error":{"message":"Rate limit reached for ` + testKey + `","type":"requests","code":"rate_limit_exceeded"}}`, CodeRateLimit},
		{"out of quota", 429, `{"error":{"message":"You exceeded your current quota, ` + testKey + `","type":"insufficient_quota","code":"insufficient_quota"}}`, CodeQuota},
		{"server error", 500, `{"error":{"message":"boom ` + testKey + `"}}`, CodeServer},
		{"refused", 400, `{"error":{"message":"Invalid file format for ` + testKey + `","type":"invalid_request_error"}}`, CodeRejected},
		{"too large upstream", 413, `{"error":{"message":"Maximum content size limit exceeded"}}`, CodeTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { reply(w, tc.status, tc.body) })
			_, err := s.Transcribe(context.Background(), request(audio))
			if err == nil {
				t.Fatal("want an error")
			}
			if got := code(t, err); got != tc.want {
				t.Errorf("code = %q, want %q (%v)", got, tc.want, err)
			}
			noSecret(t, err)
		})
	}
}

func TestTranscribeNetworkFailureDoesNotLeakTheKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	store := credentials.NewMemory()
	store.Set(llm.CredentialKey(llm.ProviderOpenAI), []byte(testKey))
	s, _ := New(Config{Credentials: store, HTTP: srv.Client(), BaseURL: srv.URL + "/v1"})
	srv.Close()
	_, err := s.Transcribe(context.Background(), request(audio))
	if code(t, err) != CodeNetwork {
		t.Fatalf("got %v", err)
	}
	noSecret(t, err)
}

func TestTranscribeRejectsOversizedAndOverlongRecordingsBeforeAnyRequest(t *testing.T) {
	var calls atomic.Int32
	s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); reply(w, 200, `{"text":"x"}`) })

	big := request(make([]byte, MaxAudioBytes+1))
	if _, err := s.Transcribe(context.Background(), big); code(t, err) != CodeTooLarge {
		t.Errorf("oversized: %v", err)
	}
	huge := TranscribeRequest{MimeType: "audio/webm", DataBase64: strings.Repeat("A", base64.StdEncoding.EncodedLen(MaxAudioBytes)+4)}
	if _, err := s.Transcribe(context.Background(), huge); code(t, err) != CodeTooLarge {
		t.Errorf("oversized base64: %v", err)
	}
	long := request(audio)
	long.DurationMs = (MaxSeconds + 30) * 1000
	if _, err := s.Transcribe(context.Background(), long); code(t, err) != CodeTooLong {
		t.Errorf("too long: %v", err)
	}
	atCap := request(make([]byte, MaxAudioBytes))
	atCap.DurationMs = MaxSeconds * 1000
	if _, err := s.Transcribe(context.Background(), atCap); err != nil {
		t.Errorf("a recording exactly at the caps must pass: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("only the in-limit recording may reach OpenAI; calls = %d", calls.Load())
	}
}

func TestTranscribeRejectsUnusableAudio(t *testing.T) {
	var calls atomic.Int32
	s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); reply(w, 200, `{"text":"x"}`) })
	cases := map[string]TranscribeRequest{
		"empty":      {MimeType: "audio/webm", DataBase64: ""},
		"bad base64": {MimeType: "audio/webm", DataBase64: "***not base64***"},
		"no mime":    {MimeType: "", DataBase64: base64.StdEncoding.EncodeToString(audio)},
		"text mime":  {MimeType: "text/plain", DataBase64: base64.StdEncoding.EncodeToString(audio)},
	}
	want := map[string]Code{"empty": CodeNoAudio, "bad base64": CodeNoAudio, "no mime": CodeUnsupported, "text mime": CodeUnsupported}
	for name, req := range cases {
		_, err := s.Transcribe(context.Background(), req)
		if err == nil || code(t, err) != want[name] {
			t.Errorf("%s: %v", name, err)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("unusable audio reached OpenAI %d times", calls.Load())
	}
}

func TestTranscribeEmptyTranscriptIsNotAnError(t *testing.T) {
	s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { reply(w, 200, `{"text":"  \n"}`) })
	got, err := s.Transcribe(context.Background(), request(audio))
	if err != nil || got.Text != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestTranscribeFallsBackToWhisperWhenTheModelIsUnavailable(t *testing.T) {
	unavailable := map[string]struct {
		status int
		body   string
	}{
		"not found":      {404, `{"error":{"message":"The model gpt-4o-mini-transcribe does not exist","code":"model_not_found"}}`},
		"no access":      {403, `{"error":{"message":"Project proj_x does not have access to model gpt-4o-mini-transcribe","code":"model_not_found"}}`},
		"does not exist": {400, `{"error":{"message":"The model does not exist or you do not have access to it."}}`},
		"bare 404":       {404, ``},
	}
	for name, u := range unavailable {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var models []string
			s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				m := r.FormValue("model")
				mu.Lock()
				models = append(models, m)
				mu.Unlock()
				if m == ModelPrimary {
					reply(w, u.status, u.body)
					return
				}
				reply(w, 200, `{"text":"from whisper"}`)
			})
			got, err := s.Transcribe(context.Background(), request(audio))
			if err != nil || got.Text != "from whisper" || got.Model != ModelFallback {
				t.Fatalf("got %+v, %v", got, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if fmt.Sprint(models) != fmt.Sprint([]string{ModelPrimary, ModelFallback}) {
				t.Errorf("models tried: %v", models)
			}
		})
	}
}

func TestTranscribeDoesNotFallBackOnOtherFailures(t *testing.T) {
	var calls atomic.Int32
	s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		reply(w, 401, `{"error":{"message":"bad key"}}`)
	})
	if _, err := s.Transcribe(context.Background(), request(audio)); code(t, err) != CodeAuth {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestTranscribeFailsWhenNeitherModelIsAvailable(t *testing.T) {
	s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { reply(w, 404, `{"error":{"code":"model_not_found"}}`) })
	_, err := s.Transcribe(context.Background(), request(audio))
	if err == nil || code(t, err) != CodeRejected {
		t.Fatalf("got %v", err)
	}
}

func TestTranscribeWithoutAKeyNeverCallsOpenAI(t *testing.T) {
	var calls atomic.Int32
	s, store := setup(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); reply(w, 200, `{"text":"x"}`) })
	if err := store.Delete(llm.CredentialKey(llm.ProviderOpenAI)); err != nil {
		t.Fatal(err)
	}
	_, err := s.Transcribe(context.Background(), request(audio))
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "Connect OpenAI in Settings > AI providers") {
		t.Errorf("message = %q", err)
	}
	if calls.Load() != 0 {
		t.Errorf("calls = %d", calls.Load())
	}
	store.Set(llm.CredentialKey(llm.ProviderOpenAI), []byte("   "))
	if _, err := s.Transcribe(context.Background(), request(audio)); !errors.Is(err, ErrNotConnected) {
		t.Errorf("blank key: %v", err)
	}
}

func TestStatusReflectsTheStoredKeyAndReportsLimits(t *testing.T) {
	s, store := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("status must not contact OpenAI") })
	a, err := s.Status()
	if err != nil || !a.Ready || a.Provider != "OpenAI" || a.MaxSeconds != MaxSeconds || a.MaxBytes != MaxAudioBytes {
		t.Fatalf("connected: %+v %v", a, err)
	}
	store.Delete(llm.CredentialKey(llm.ProviderOpenAI))
	if a, err := s.Status(); err != nil || a.Ready {
		t.Fatalf("disconnected: %+v %v", a, err)
	}
}

type failingStore struct{ credentials.Store }

func (failingStore) Get(string) ([]byte, error) { return nil, errors.New("vault exploded") }

func TestCredentialStoreFailureIsReportedWithoutDetail(t *testing.T) {
	s, _ := New(Config{Credentials: failingStore{}})
	_, err := s.Transcribe(context.Background(), request(audio))
	if code(t, err) != CodeCredentials || strings.Contains(err.Error(), "vault exploded") {
		t.Fatalf("got %v", err)
	}
	if _, err := s.Status(); err == nil {
		t.Error("Status must surface a broken credential store")
	}
}

func TestTranscribeStopsWhenTheCallerCancels(t *testing.T) {
	release := make(chan struct{})
	s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := s.Transcribe(ctx, request(audio))
	if err == nil || code(t, err) != CodeCanceled {
		t.Fatalf("got %v", err)
	}
}

func TestTranscribeLimitsConcurrentRecordings(t *testing.T) {
	started := make(chan struct{}, maxInFlight)
	release := make(chan struct{})
	s, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		reply(w, 200, `{"text":"ok"}`)
	})
	var wg sync.WaitGroup
	for range maxInFlight {
		wg.Add(1)
		go func() { defer wg.Done(); s.Transcribe(context.Background(), request(audio)) }()
	}
	for range maxInFlight {
		<-started
	}
	if _, err := s.Transcribe(context.Background(), request(audio)); code(t, err) != CodeBusy {
		t.Errorf("got %v", err)
	}
	close(release)
	wg.Wait()
}

func TestNewRequiresACredentialStore(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("want an error")
	}
}

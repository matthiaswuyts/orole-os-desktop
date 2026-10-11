package daemon_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/dictation"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
	"github.com/rayorole/orole-os-desktop/internal/llm"
)

func TestDictationIPCRequiresAKeyAndAuditsWithoutSecretsOrText(t *testing.T) {
	const key = "sk-proj-0123456789abcdefSECRET1234"
	const spoken = "my very private sentence"
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text":"`+spoken+`"}`)
	}))
	defer srv.Close()
	store, restore := daemon.UseDictationBackend(srv.URL, srv.Client())
	defer restore()

	dir := t.TempDir()
	d, err := daemon.New(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()
	defer func() {
		d.Shutdown()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := control.NewClient(dir, "dictation-test")
	defer c.Close()
	for {
		if err = c.Connect(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(20 * time.Millisecond):
		}
	}

	req := dictation.TranscribeRequest{MimeType: "audio/webm", DataBase64: base64.StdEncoding.EncodeToString([]byte("audio bytes")), DurationMs: 2000}
	var st dictation.Availability
	if err := c.Call(ctx, "dictation.status", struct{}{}, &st); err != nil || st.Ready || st.MaxSeconds != dictation.MaxSeconds {
		t.Fatalf("status without a key: %+v %v", st, err)
	}
	var tr dictation.Transcript
	err = c.Call(ctx, "dictation.transcribe", req, &tr)
	var ie *ipc.Error
	if !errors.As(err, &ie) || ie.Code != "dictation_not_connected" || !strings.Contains(ie.Message, "Settings > AI providers") {
		t.Fatalf("transcribe without a key: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("request reached OpenAI without a key")
	}

	if err := store.Set(llm.CredentialKey(llm.ProviderOpenAI), []byte(key)); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "dictation.status", struct{}{}, &st); err != nil || !st.Ready {
		t.Fatalf("status with a key: %+v %v", st, err)
	}
	if err := c.Call(ctx, "dictation.transcribe", req, &tr); err != nil || tr.Text != spoken {
		t.Fatalf("transcribe: %+v %v", tr, err)
	}
	req.DurationMs = 10 * 60 * 1000
	err = c.Call(ctx, "dictation.transcribe", req, &tr)
	if !errors.As(err, &ie) || ie.Code != "dictation_too_long" {
		t.Fatalf("too long: %v", err)
	}

	rows, err := d.DB.Query(`SELECT action, target, outcome, detail FROM audit_events WHERE action='dictation.transcribe' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var action, target, outcome, detail string
		if err := rows.Scan(&action, &target, &outcome, &detail); err != nil {
			t.Fatal(err)
		}
		line := outcome + "|" + detail
		if strings.Contains(line+target, key) || strings.Contains(line, spoken) {
			t.Errorf("audit leaks a secret or the transcript: %q", line)
		}
		got = append(got, outcome)
	}
	if strings.Join(got, ",") != "denied,allowed,denied" {
		t.Errorf("audit outcomes = %v", got)
	}
	_ = credentials.Store(store)
}

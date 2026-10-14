package daemon_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/daemon"
	"github.com/rayorole/orole-os-desktop/internal/daemon/control"
	"github.com/rayorole/orole-os-desktop/internal/dictation"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

// Dictation failures cross the IPC boundary as "dictation_<code>" errors carrying the user-ready message,
// and preferences set over IPC survive in the daemon's settings.
func TestDictationIPCErrorsAndPreferences(t *testing.T) {
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

	var st dictation.VoiceStatus
	if err := c.Call(ctx, "dictation.language", dictation.LanguageRequest{Language: "nl"}, &st); err != nil || st.Language != "nl" {
		t.Fatalf("language: %+v %v", st.Language, err)
	}
	if err := c.Call(ctx, "dictation.status", struct{}{}, &st); err != nil || st.Ready || len(st.Models) != len(dictation.Catalog) || st.Language != "nl" {
		t.Fatalf("status: %+v %v", st, err)
	}

	pcm := base64.StdEncoding.EncodeToString(make([]byte, 3200))
	for _, tc := range []struct {
		method string
		params any
		code   string
	}{
		{"dictation.use", dictation.ModelRef{ID: "no-such-model"}, "dictation_unknown"},
		{"dictation.use", dictation.ModelRef{ID: dictation.ModelParakeet}, "dictation_not_ready"},
		{"dictation.language", dictation.LanguageRequest{Language: "xx"}, "dictation_unknown"},
		{"dictation.upload", dictation.UploadRequest{ID: "bad id", PCMBase64: pcm}, "dictation_bad_audio"},
		{"dictation.upload", dictation.UploadRequest{ID: "recording-1", PCMBase64: pcm}, ""},
		{"dictation.transcribe", dictation.RecordingRef{ID: "recording-1"}, "dictation_not_ready"},
		{"dictation.transcribe", dictation.RecordingRef{ID: "recording-1"}, "dictation_bad_audio"},
	} {
		err := c.Call(ctx, tc.method, tc.params, nil)
		var ie *ipc.Error
		switch {
		case tc.code == "" && err != nil:
			t.Errorf("%s: %v", tc.method, err)
		case tc.code != "" && (!errors.As(err, &ie) || ie.Code != tc.code || ie.Message == ""):
			t.Errorf("%s %+v: got %v, want %s", tc.method, tc.params, err, tc.code)
		}
	}
	var stored string
	if err := d.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, dictation.SettingKey).Scan(&stored); err != nil || stored != `{"model":"","language":"nl"}` {
		t.Fatalf("stored preferences %q %v", stored, err)
	}
}

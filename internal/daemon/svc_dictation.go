package daemon

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/dictation"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

// Test seam: production sends audio to OpenAI with the key from the OS credential store.
var newDictation = func(store credentials.Store) (*dictation.Service, error) {
	return dictation.New(dictation.Config{Credentials: store})
}

// registerDictation wires voice dictation for the composers.
//
// IPC methods:
//
//	dictation.status      struct{}                     -> Availability  (is an OpenAI key stored; the limits)
//	dictation.transcribe  TranscribeRequest            -> Transcript    (sends the recording to OpenAI)
//
// Transcribing sends the user's audio to OpenAI, so every attempt is audited with its outcome,
// the model and the audio size. The audit never contains the transcript or the key. Failures
// are IPC errors coded "dictation_<code>" (see dictation.Code) whose message is user-facing.
func (d *Daemon) registerDictation() {
	svc, err := newDictation(llmCredentialStore())
	if err != nil {
		d.Log.Error("dictation unavailable", "err", err)
		return
	}
	handle(d, "dictation.status", func(ctx context.Context, _ none) (dictation.Availability, error) {
		a, err := svc.Status()
		return a, dictationError(err)
	})
	handle(d, "dictation.transcribe", func(ctx context.Context, p dictation.TranscribeRequest) (dictation.Transcript, error) {
		size := base64.StdEncoding.DecodedLen(len(p.DataBase64))
		res, err := svc.Transcribe(ctx, p)
		if err != nil {
			var de *dictation.Error
			outcome, detail := "error", "transcription failed"
			if errors.As(err, &de) {
				detail = string(de.Code)
				if !sentAudio(de.Code) {
					outcome = "denied"
				}
			}
			d.audit("dictation.transcribe", "dictation/openai", outcome, detail)
			return dictation.Transcript{}, dictationError(err)
		}
		d.audit("dictation.transcribe", "dictation/openai", "allowed", fmt.Sprintf("%s, %d KB audio", res.Model, (size+1023)/1024))
		return res, nil
	})
}

// sentAudio reports whether a failure happened after the recording left the machine.
func sentAudio(c dictation.Code) bool {
	switch c {
	case dictation.CodeNotConnected, dictation.CodeTooLarge, dictation.CodeTooLong, dictation.CodeNoAudio,
		dictation.CodeUnsupported, dictation.CodeBusy, dictation.CodeCredentials:
		return false
	}
	return true
}

// dictationError exposes a dictation failure as a coded IPC error so the UI can show its message
// and recognise "not connected".
func dictationError(err error) error {
	var de *dictation.Error
	if errors.As(err, &de) {
		return &ipc.Error{Code: "dictation_" + string(de.Code), Message: de.Message}
	}
	return err
}

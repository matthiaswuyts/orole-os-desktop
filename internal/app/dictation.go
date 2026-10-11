package app

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/dictation"
)

// Dictation is a stateless bridge to the daemon's voice transcription. The recording is sent to
// OpenAI with the user's own OpenAI key, which stays in the daemon and the Windows credential store.
type Dictation struct{ L *Link }

// Status says whether an OpenAI key is connected and what recording limits apply.
func (d Dictation) Status(ctx context.Context) (dictation.Availability, error) {
	var out dictation.Availability
	err := d.L.call(ctx, "dictation.status", struct{}{}, &out)
	return out, err
}

// Transcribe sends one recording (base64 of the recorded container) to OpenAI and returns the
// text, which is empty when nothing intelligible was said.
func (d Dictation) Transcribe(ctx context.Context, req dictation.TranscribeRequest) (dictation.Transcript, error) {
	var out dictation.Transcript
	err := d.L.call(ctx, "dictation.transcribe", req, &out)
	return out, err
}

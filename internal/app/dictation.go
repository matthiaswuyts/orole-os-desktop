package app

import (
	"context"
	"encoding/json"

	"github.com/egoist/mygo"
	"github.com/rayorole/orole-os-desktop/internal/dictation"
)

// DictationChanged carries the voice status after every change in the daemon (downloads report progress).
var DictationChanged = mygo.NewEvent[dictation.VoiceStatus]("dictation:changed")

func forwardDictation(event string, data json.RawMessage) bool {
	if event != "dictation.changed" {
		return false
	}
	var st dictation.VoiceStatus
	if json.Unmarshal(data, &st) == nil {
		DictationChanged.Broadcast(st)
	}
	return true
}

// Dictation is a stateless bridge to the daemon's local speech recognition. The engine and the models are
// downloaded and run by the daemon; audio never leaves this computer.
type Dictation struct{ L *Link }

// Status reports the engine, the models, the chosen model and language, and the recording limits.
func (d Dictation) Status(ctx context.Context) (dictation.VoiceStatus, error) {
	var out dictation.VoiceStatus
	err := d.L.call(ctx, "dictation.status", struct{}{}, &out)
	return out, err
}

// Download starts downloading a model (and the speech engine when missing) in the background.
func (d Dictation) Download(ctx context.Context, ref dictation.ModelRef) (dictation.VoiceStatus, error) {
	return d.status(ctx, "dictation.download", ref)
}

// Cancel stops a download; what was received is kept so the next download continues from there.
func (d Dictation) Cancel(ctx context.Context, ref dictation.ModelRef) (dictation.VoiceStatus, error) {
	return d.status(ctx, "dictation.cancel", ref)
}

// Remove deletes a model from this computer.
func (d Dictation) Remove(ctx context.Context, ref dictation.ModelRef) (dictation.VoiceStatus, error) {
	return d.status(ctx, "dictation.remove", ref)
}

// Use makes a downloaded model the one dictation runs with.
func (d Dictation) Use(ctx context.Context, ref dictation.ModelRef) (dictation.VoiceStatus, error) {
	return d.status(ctx, "dictation.use", ref)
}

// SetLanguage sets the spoken language for models that accept one ("" = detect automatically).
func (d Dictation) SetLanguage(ctx context.Context, req dictation.LanguageRequest) (dictation.VoiceStatus, error) {
	return d.status(ctx, "dictation.language", req)
}

// Prepare starts loading the model in use so the next transcription starts at once. It returns at once.
func (d Dictation) Prepare(ctx context.Context) error {
	return d.L.call(ctx, "dictation.prepare", struct{}{}, nil)
}

// Upload sends one part of a recording (16 kHz mono 16-bit PCM, base64).
func (d Dictation) Upload(ctx context.Context, req dictation.UploadRequest) error {
	return d.L.call(ctx, "dictation.upload", req, nil)
}

// Transcribe turns an uploaded recording into text. The text is empty when nothing intelligible was said.
func (d Dictation) Transcribe(ctx context.Context, ref dictation.RecordingRef) (dictation.Transcript, error) {
	var out dictation.Transcript
	err := d.L.call(ctx, "dictation.transcribe", ref, &out)
	return out, err
}

// Discard drops an uploaded recording that will not be transcribed.
func (d Dictation) Discard(ctx context.Context, ref dictation.RecordingRef) error {
	return d.L.call(ctx, "dictation.discard", ref, nil)
}

func (d Dictation) status(ctx context.Context, method string, params any) (dictation.VoiceStatus, error) {
	var out dictation.VoiceStatus
	err := d.L.call(ctx, method, params, &out)
	return out, err
}

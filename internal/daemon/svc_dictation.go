package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rayorole/orole-os-desktop/internal/dictation"
	"github.com/rayorole/orole-os-desktop/internal/dictation/engine"
	"github.com/rayorole/orole-os-desktop/internal/ipc"
)

// newVoiceEngine is a test seam: production recognizes speech in a helper process (this executable run as
// "voice-engine") so a native fault never takes the daemon down.
var newVoiceEngine = func(log *slog.Logger) dictation.Recognizer {
	exe, _ := os.Executable()
	return engine.New(engine.Config{Exe: exe, Log: log})
}

// registerDictation wires local voice dictation for the composers. Speech recognition runs on this computer;
// the engine and the models are downloaded on request into <DataDir>/voice.
//
// IPC methods (all return the VoiceStatus unless noted):
//
//	dictation.status      struct{}                                   (engine, models, preferences, limits)
//	dictation.download    ModelRef        starts a download in the background (the engine first when missing)
//	dictation.cancel      ModelRef        stops a download; its partial files are kept for resuming
//	dictation.remove      ModelRef        deletes a model (and the engine once no model is left)
//	dictation.use         ModelRef        makes a downloaded model the one dictation runs with
//	dictation.language    LanguageRequest sets the spoken language ("" = automatic)
//	dictation.prepare     struct{} -> struct{}     starts loading the model in the background
//	dictation.upload      UploadRequest -> struct{} one part of a recording (16 kHz mono PCM)
//	dictation.transcribe  RecordingRef -> Transcript
//	dictation.discard     RecordingRef -> struct{}
//
// Event "dictation.changed" carries the VoiceStatus after every change. Downloads and removals are audited;
// audio and transcripts are neither logged, audited nor stored. Failures are IPC errors coded
// "dictation_<code>" (see dictation.Code) whose message is user-facing.
func (d *Daemon) registerDictation() {
	log := d.Log.With("module", "dictation")
	svc, err := dictation.NewService(dictation.Config{
		Dir:        filepath.Join(d.DataDir, "voice"),
		Settings:   settingsKV{d.DB},
		Recognizer: newVoiceEngine(log),
		Publish:    func(st dictation.VoiceStatus) { d.IPC.Publish("dictation.changed", st) },
		Audit:      d.audit,
		Log:        log,
	})
	if err != nil {
		d.Log.Error("dictation unavailable", "err", err)
		return
	}
	d.Dictation = svc
	handle(d, "dictation.status", func(ctx context.Context, _ none) (dictation.VoiceStatus, error) {
		return svc.Status(ctx), nil
	})
	models := map[string]func(context.Context, string) (dictation.VoiceStatus, error){
		"dictation.download": svc.Download,
		"dictation.cancel":   svc.Cancel,
		"dictation.remove":   svc.Remove,
		"dictation.use":      svc.Use,
	}
	for name, f := range models {
		handle(d, name, func(ctx context.Context, p dictation.ModelRef) (dictation.VoiceStatus, error) {
			st, err := f(ctx, p.ID)
			return st, dictationError(err)
		})
	}
	handle(d, "dictation.language", func(ctx context.Context, p dictation.LanguageRequest) (dictation.VoiceStatus, error) {
		st, err := svc.SetLanguage(ctx, p.Language)
		return st, dictationError(err)
	})
	handle(d, "dictation.prepare", func(ctx context.Context, _ none) (none, error) {
		return none{}, dictationError(svc.Prepare(ctx))
	})
	handle(d, "dictation.upload", func(ctx context.Context, p dictation.UploadRequest) (none, error) {
		return none{}, dictationError(svc.Upload(ctx, p))
	})
	handle(d, "dictation.transcribe", func(ctx context.Context, p dictation.RecordingRef) (dictation.Transcript, error) {
		tr, err := svc.Transcribe(ctx, p)
		return tr, dictationError(err)
	})
	handle(d, "dictation.discard", func(ctx context.Context, p dictation.RecordingRef) (none, error) {
		return none{}, dictationError(svc.Discard(ctx, p))
	})
}

// dictationError exposes a dictation failure as a coded IPC error so the UI can show its message.
func dictationError(err error) error {
	var de *dictation.Error
	if errors.As(err, &de) {
		return &ipc.Error{Code: "dictation_" + string(de.Code), Message: de.Message}
	}
	return err
}

// Package dictation turns a short microphone recording into text for the composers, on this computer.
//
// Speech recognition runs locally with sherpa-onnx (an ONNX Runtime speech toolkit) and one of the models in
// Catalog. The daemon downloads the engine and the models on the user's request, checks every file against a
// pinned SHA-256, and keeps them under <DataDir>/voice. Recognition itself runs in a helper process (the daemon
// executable started as "voice-engine", see package dictation/engine) so a native fault never takes the daemon
// down and the model's memory is returned when dictation goes idle.
//
// The webview records with MediaRecorder, decodes the recording to 16 kHz mono 16-bit PCM itself, and uploads
// it in parts (one recording is larger than one IPC frame). Neither audio nor transcript leaves the computer,
// is logged, or is stored.
package dictation

import (
	"context"
	"time"
)

const (
	// SampleRate is the only PCM rate Upload accepts: 16 kHz mono, signed 16-bit little-endian.
	SampleRate = 16000
	// MaxSeconds bounds one recording. The frontend stops recording there.
	MaxSeconds = 120
	// MaxPCMBytes bounds one recording's PCM (MaxSeconds plus a little grace for the recorder's clock).
	MaxPCMBytes = (MaxSeconds + 5) * SampleRate * 2
	// PartBytes bounds one uploaded part's raw PCM so its base64 stays far inside the 4 MiB IPC frame.
	PartBytes = 1 << 20
	// UploadTTL is how long an unfinished upload is kept before it is dropped.
	UploadTTL = 2 * time.Minute
	// MaxUploads bounds recordings being uploaded or transcribed at once.
	MaxUploads = 2
	// TranscribeTimeout bounds one transcription, model loading included. The daemon's own per-call deadline
	// (30 s) is too short for two minutes of speech on a slow CPU, so the handler uses this instead.
	TranscribeTimeout = 3 * time.Minute
	// IdleUnload is how long the helper keeps a model in memory after the last use.
	IdleUnload = 10 * time.Minute
	// SettingKey is the daemon settings key holding Preferences as JSON.
	SettingKey = "dictation"
)

// Code classifies a failure for the UI.
type Code string

const (
	CodeNotReady    Code = "not_ready"   // no model is in use or its files are missing
	CodeUnsupported Code = "unsupported" // this computer cannot run the speech engine
	CodeUnknown     Code = "unknown"     // no such model or language
	CodeTooLong     Code = "too_long"    // over MaxSeconds
	CodeNoAudio     Code = "no_audio"    // nothing was recorded
	CodeBadAudio    Code = "bad_audio"   // malformed PCM, out-of-order part or expired upload
	CodeBusy        Code = "busy"        // MaxUploads already in progress, or the model is being changed
	CodeCanceled    Code = "canceled"
	CodeTimeout     Code = "timeout"
	CodeEngine      Code = "engine" // the speech engine failed or stopped
	CodeDownloading Code = "downloading"
)

// Error is the only error type the service returns to callers. Message is safe to show as is.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }

// Fail builds an *Error.
func Fail(code Code, msg string) *Error { return &Error{Code: code, Message: msg} }

// ModelState is where a model is in its life on this computer.
type ModelState string

const (
	StateAbsent      ModelState = "absent"      // not downloaded (a partial download may be kept for resuming)
	StateDownloading ModelState = "downloading" // a download is running
	StateReady       ModelState = "ready"       // every file is present and was verified when downloaded
	StateFailed      ModelState = "failed"      // the last download failed; Error says why
)

// ModelStatus is one model as the Voice settings page shows it.
type ModelStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// LanguageChoice reports that the spoken language can be set for this model (Whisper). Without it the model
	// finds the language itself.
	LanguageChoice bool `json:"languageChoice"`
	// SizeBytes is the model's files on disk.
	SizeBytes int64 `json:"sizeBytes"`
	// DownloadBytes is what a download fetches now: the model's files plus the speech engine when it is not
	// installed yet. Bytes already kept from an interrupted download are not subtracted.
	DownloadBytes int64      `json:"downloadBytes"`
	State         ModelState `json:"state"`
	// ReceivedBytes and TotalBytes describe the running download (State == downloading).
	ReceivedBytes int64 `json:"receivedBytes"`
	TotalBytes    int64 `json:"totalBytes"`
	// Error is the last download's failure, user-ready (State == failed).
	Error   string `json:"error,omitempty"`
	License string `json:"license"`
}

// Language is one spoken language a model can be told to expect.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// VoiceStatus is everything the composers and the Voice settings page need. (Not "Status": the generated
// TypeScript bindings already have one.)
type VoiceStatus struct {
	// Supported is false on computers the speech engine has no build for; nothing can be downloaded there.
	Supported bool `json:"supported"`
	// Ready reports that dictation can run now: a model is in use and it and the engine are on disk.
	Ready bool `json:"ready"`
	// Model is the model in use ("" = none chosen yet).
	Model string `json:"model"`
	// Language is the spoken language for models with LanguageChoice ("" = detect automatically).
	Language   string        `json:"language"`
	Models     []ModelStatus `json:"models"`
	Languages  []Language    `json:"languages"`
	MaxSeconds int           `json:"maxSeconds"`
	SampleRate int           `json:"sampleRate"`
	PartBytes  int           `json:"partBytes"`
}

// Preferences is what the user chose, stored as JSON under SettingKey.
type Preferences struct {
	Model    string `json:"model"`
	Language string `json:"language"`
}

// ModelRef names a model.
type ModelRef struct {
	ID string `json:"id"`
}

// LanguageRequest sets the spoken language ("" = automatic).
type LanguageRequest struct {
	Language string `json:"language"`
}

// UploadRequest carries one part of a recording. Parts are numbered from 0 and must arrive in order.
type UploadRequest struct {
	// ID is chosen by the client for this recording (8-64 characters of [A-Za-z0-9_-]).
	ID   string `json:"id"`
	Part int    `json:"part"`
	// PCMBase64 is base64 of signed 16-bit little-endian mono samples at SampleRate, at most PartBytes raw.
	PCMBase64 string `json:"pcmBase64"`
}

// RecordingRef names an uploaded recording.
type RecordingRef struct {
	ID string `json:"id"`
}

// Transcript is the recognized text. Text is empty when nothing intelligible was said.
type Transcript struct {
	Text  string `json:"text"`
	Model string `json:"model"`
}

// Spec says which model the engine loads. Paths are absolute.
type Spec struct {
	// EngineDir holds EngineBuild.DLLs.
	EngineDir                        string
	Kind                             Kind
	Encoder, Decoder, Joiner, Tokens string
	// Language is the spoken language for KindWhisper ("" = detect).
	Language string
}

// Recognizer runs speech recognition (engine.Process in production). Calls are serialised by the caller.
type Recognizer interface {
	// Load makes spec's model ready so the next Recognize starts at once. It is a no-op when already loaded.
	Load(ctx context.Context, spec Spec) error
	// Recognize returns the text spoken in samples (mono float32 in [-1, 1] at SampleRate), loading spec first
	// when another model (or language) is loaded.
	Recognize(ctx context.Context, spec Spec, samples []float32) (string, error)
	// Close stops the engine and frees the model's memory. A later call starts it again.
	Close() error
}

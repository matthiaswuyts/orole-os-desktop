package dictation

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var recordingID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// upload is a recording being received or transcribed. Its PCM is never logged or stored.
type upload struct {
	pcm     []byte
	next    int // the part expected next
	touched time.Time
	busy    bool // being transcribed
}

var (
	errBadRecording = Fail(CodeBadAudio, "The recording could not be read. Try again.")
	errIncomplete   = Fail(CodeBadAudio, "The recording arrived incomplete. Try again.")
	errExpired      = Fail(CodeBadAudio, "The recording expired before it was transcribed. Try again.")
	errNotReady     = Fail(CodeNotReady, "Choose and download a dictation model in Settings > Voice.")
)

// Upload receives one part of a recording. Parts must arrive in order from 0.
func (s *Service) Upload(_ context.Context, req UploadRequest) error {
	if !recordingID.MatchString(req.ID) {
		return errBadRecording
	}
	raw, err := base64.StdEncoding.DecodeString(req.PCMBase64)
	if err != nil || len(raw)%2 != 0 || len(raw) > PartBytes {
		return errBadRecording
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepUploads(now)
	u := s.uploads[req.ID]
	switch {
	case u == nil && req.Part != 0:
		return errIncomplete
	case u == nil:
		if len(s.uploads) >= MaxUploads {
			return Fail(CodeBusy, "Another recording is still being transcribed. Try again in a moment.")
		}
		u = &upload{}
		s.uploads[req.ID] = u
	case u.busy:
		return errIncomplete
	case req.Part != u.next:
		delete(s.uploads, req.ID)
		return errIncomplete
	}
	if len(u.pcm)+len(raw) > MaxPCMBytes {
		delete(s.uploads, req.ID)
		return Fail(CodeTooLong, fmt.Sprintf("A recording can be at most %d minutes long.", MaxSeconds/60))
	}
	u.pcm = append(u.pcm, raw...)
	u.next++
	u.touched = now
	return nil
}

// Discard drops an uploaded recording.
func (s *Service) Discard(_ context.Context, ref RecordingRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.uploads[ref.ID]; u != nil && !u.busy {
		delete(s.uploads, ref.ID)
	}
	return nil
}

// sweepUploads drops recordings untouched for UploadTTL. Callers hold mu.
func (s *Service) sweepUploads(now time.Time) {
	for id, u := range s.uploads {
		if !u.busy && now.Sub(u.touched) > UploadTTL {
			delete(s.uploads, id)
		}
	}
}

// Transcribe turns an uploaded recording into text with the model in use, then drops the recording.
//
// It runs for up to TranscribeTimeout regardless of ctx's deadline (the daemon's per-call deadline is shorter
// than a long recording takes on a slow CPU); only an explicit cancellation of ctx stops it early.
func (s *Service) Transcribe(ctx context.Context, ref RecordingRef) (Transcript, error) {
	s.mu.Lock()
	s.sweepUploads(s.now())
	u := s.uploads[ref.ID]
	if u == nil || u.busy {
		s.mu.Unlock()
		return Transcript{}, errExpired
	}
	u.busy = true
	pcm, prefs := u.pcm, s.prefs
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.uploads, ref.ID)
		s.mu.Unlock()
	}()

	if len(pcm) == 0 {
		return Transcript{}, Fail(CodeNoAudio, "Nothing was recorded.")
	}
	m, ok := s.model(prefs.Model)
	if !s.supported || !ok || !s.modelReady(m) || !s.engineInstalled() {
		return Transcript{}, errNotReady
	}
	samples := make([]float32, len(pcm)/2)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768
	}
	pieces := [][]float32{samples}
	if m.ChunkSeconds > 0 {
		pieces = Chunks(samples, SampleRate, m.ChunkSeconds)
	}

	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), TranscribeTimeout)
	defer cancel()
	stopCaller := context.AfterFunc(ctx, func() {
		if errors.Is(ctx.Err(), context.Canceled) {
			cancel()
		}
	})
	defer stopCaller()
	stopService := context.AfterFunc(s.ctx, cancel)
	defer stopService()

	spec := s.spec(m, prefs.Language)
	s.recMu.Lock()
	defer s.recMu.Unlock()
	texts := make([]string, 0, len(pieces))
	for _, p := range pieces {
		text, err := s.rec.Recognize(tctx, spec, p)
		if err != nil {
			return Transcript{}, s.recognizeErr(tctx, err)
		}
		if t := strings.TrimSpace(text); t != "" {
			texts = append(texts, t)
		}
	}
	return Transcript{Text: strings.Join(texts, " "), Model: m.Name}, nil
}

// recognizeErr turns a Recognizer failure into an *Error. Neither audio nor text is logged.
func (s *Service) recognizeErr(tctx context.Context, err error) error {
	switch {
	case errors.Is(tctx.Err(), context.DeadlineExceeded):
		return Fail(CodeTimeout, "Transcribing took too long. Try a shorter recording.")
	case tctx.Err() != nil:
		return Fail(CodeCanceled, "Dictation was canceled.")
	}
	var de *Error
	if errors.As(err, &de) {
		return de
	}
	s.log.Warn("dictation engine failed", "err", err)
	return Fail(CodeEngine, "The speech engine stopped unexpectedly. Try again.")
}

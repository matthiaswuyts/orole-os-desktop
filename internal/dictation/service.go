package dictation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Settings stores the user's preferences (the daemon settings table).
type Settings interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
}

// Config wires a Service.
type Config struct {
	// Dir holds the engine, the models and the downloads (<DataDir>/voice).
	Dir        string
	Settings   Settings
	Recognizer Recognizer
	// HTTP downloads the engine and the models (nil = a client without an overall timeout).
	HTTP *http.Client
	// Publish receives the status after every change (nil = none).
	Publish func(VoiceStatus)
	// Audit records downloads and removals (nil = none).
	Audit func(action, target, outcome, detail string)
	Log   *slog.Logger
	// Catalog, Engine and Supported default to the package's Catalog, EngineBuild and EngineSupported().
	Catalog   []Model
	Engine    *Engine
	Supported *bool
}

// Service manages the speech engine and models on disk and turns uploaded recordings into text.
type Service struct {
	dir       string
	settings  Settings
	rec       Recognizer
	http      *http.Client
	publishFn func(VoiceStatus)
	auditFn   func(action, target, outcome, detail string)
	log       *slog.Logger
	catalog   []Model
	engine    Engine
	supported bool
	stall     time.Duration    // a download receiving nothing this long fails
	now       func() time.Time // upload expiry clock

	ctx    context.Context // cancelled by Close
	cancel context.CancelFunc
	wg     sync.WaitGroup

	prefMu sync.Mutex // serialises preference writes (taken before mu)
	pubMu  sync.Mutex // orders published statuses (taken before mu)
	last   time.Time  // last publish, guarded by pubMu
	recMu  sync.Mutex // one Recognizer call at a time
	// engineSem lets one job at a time install the engine.
	engineSem chan struct{}

	mu        sync.Mutex
	prefs     Preferences
	jobs      map[string]*job
	failed    map[string]string // model id -> last download failure, user-ready
	uploads   map[string]*upload
	preparing bool
	closed    bool
}

// NewService prepares the voice directory and reads the stored preferences.
func NewService(cfg Config) (*Service, error) {
	if cfg.Dir == "" || cfg.Settings == nil || cfg.Recognizer == nil {
		return nil, errors.New("dictation: Dir, Settings and Recognizer are required")
	}
	s := &Service{
		dir: cfg.Dir, settings: cfg.Settings, rec: cfg.Recognizer, http: cfg.HTTP,
		publishFn: cfg.Publish, auditFn: cfg.Audit, log: cfg.Log,
		catalog: cfg.Catalog, engine: EngineBuild, supported: EngineSupported(),
		stall: 60 * time.Second, now: time.Now,
		engineSem: make(chan struct{}, 1),
		jobs:      map[string]*job{}, failed: map[string]string{}, uploads: map[string]*upload{},
	}
	if s.catalog == nil {
		s.catalog = Catalog
	}
	if cfg.Engine != nil {
		s.engine = *cfg.Engine
	}
	if cfg.Supported != nil {
		s.supported = *cfg.Supported
	}
	if s.http == nil {
		s.http = &http.Client{}
	}
	if s.log == nil {
		s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	abs, err := filepath.Abs(s.dir) // the engine helper needs absolute paths
	if err != nil {
		return nil, fmt.Errorf("dictation: %w", err)
	}
	s.dir = abs
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, fmt.Errorf("dictation: %w", err)
	}
	s.sweepStale()
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.prefs = s.loadPrefs(s.ctx)
	return s, nil
}

// Status reports the engine, the models and the preferences.
func (s *Service) Status(context.Context) VoiceStatus {
	engine := s.engineInstalled()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := VoiceStatus{
		Supported: s.supported, Model: s.prefs.Model, Language: s.prefs.Language,
		Models: make([]ModelStatus, 0, len(s.catalog)), Languages: Languages(),
		MaxSeconds: MaxSeconds, SampleRate: SampleRate, PartBytes: PartBytes,
	}
	for _, m := range s.catalog {
		ms := ModelStatus{
			ID: m.ID, Name: m.Name, Description: m.Description, LanguageChoice: m.LanguageChoice,
			SizeBytes: m.SizeBytes(), DownloadBytes: m.SizeBytes(), License: m.License, State: StateAbsent,
		}
		if !engine {
			ms.DownloadBytes += s.engine.Archive.Size
		}
		ready := s.modelReady(m)
		switch j := s.jobs[m.ID]; {
		case j != nil:
			ms.State, ms.ReceivedBytes, ms.TotalBytes = StateDownloading, j.received.Load(), j.total.Load()
		case ready:
			ms.State = StateReady
		case s.failed[m.ID] != "":
			ms.State, ms.Error = StateFailed, s.failed[m.ID]
		}
		if m.ID == s.prefs.Model && ready && engine && s.supported {
			st.Ready = true
		}
		st.Models = append(st.Models, ms)
	}
	return st
}

// Use makes id the model dictation runs with. It must be downloaded.
func (s *Service) Use(ctx context.Context, id string) (VoiceStatus, error) {
	m, ok := s.model(id)
	if !ok {
		return VoiceStatus{}, errUnknownModel
	}
	if !s.modelReady(m) {
		return VoiceStatus{}, Fail(CodeNotReady, "Download "+m.Name+" before using it.")
	}
	changed := false
	err := s.updatePrefs(ctx, func(p *Preferences) bool {
		changed = p.Model != id
		p.Model = id
		return changed
	})
	if err != nil {
		return VoiceStatus{}, err
	}
	if changed {
		s.closeRecognizer() // frees the previous model's memory
		s.publish()
	}
	return s.Status(ctx), nil
}

// SetLanguage sets the spoken language for models with LanguageChoice ("" = automatic).
func (s *Service) SetLanguage(ctx context.Context, code string) (VoiceStatus, error) {
	if !ValidLanguage(code) {
		return VoiceStatus{}, Fail(CodeUnknown, "That language is not available for dictation.")
	}
	changed := false
	err := s.updatePrefs(ctx, func(p *Preferences) bool {
		changed = p.Language != code
		p.Language = code
		return changed
	})
	if err != nil {
		return VoiceStatus{}, err
	}
	if changed {
		s.publish()
	}
	return s.Status(ctx), nil
}

// Remove deletes a model (stopping its download first). When no model is left, the engine goes too.
func (s *Service) Remove(ctx context.Context, id string) (VoiceStatus, error) {
	m, ok := s.model(id)
	if !ok {
		return VoiceStatus{}, errUnknownModel
	}
	s.stopJob(id)
	s.mu.Lock()
	inUse := s.prefs.Model == id
	s.mu.Unlock()
	if inUse {
		s.closeRecognizer() // the helper holds the model files open
	}
	freed := s.dirSize(s.modelDir(id))
	errModel := s.removeAll(s.modelDir(id))
	errParts := s.removeAll(s.downloadDir(id))
	s.mu.Lock()
	delete(s.failed, id)
	s.mu.Unlock()
	if inUse {
		err := s.updatePrefs(ctx, func(p *Preferences) bool {
			if p.Model != id {
				return false
			}
			p.Model = ""
			for _, other := range s.catalog {
				if other.ID != id && s.modelReady(other) {
					p.Model = other.ID
					break
				}
			}
			return true
		})
		if err != nil {
			s.log.Warn("dictation preference not updated after remove", "err", err)
		}
	}
	var errEngine error
	if !s.modelsLeft() {
		s.closeRecognizer() // the helper holds the engine DLLs open
		s.mu.Lock()
		// Checked again under mu: no job can start while it is held, so nothing needs the engine any more.
		if len(s.jobs) == 0 && !s.modelsLeft() {
			errEngine = s.removeAll(filepath.Join(s.dir, "engine"))
			s.removeAll(s.engineStaging())
		}
		s.mu.Unlock()
	}
	if err := errors.Join(errModel, errParts, errEngine); err != nil {
		s.audit("dictation.remove", id, "failed", "disk")
		s.publish()
		return VoiceStatus{}, Fail(CodeBusy, "Couldn't delete all of "+m.Name+"'s files. Try again in a moment.")
	}
	s.audit("dictation.remove", id, "allowed", fmt.Sprintf("%d bytes", freed))
	s.publish()
	return s.Status(ctx), nil
}

// Prepare starts loading the model in use in the background so the next transcription starts at once.
func (s *Service) Prepare(ctx context.Context) error {
	st := s.Status(ctx)
	if !st.Ready {
		return nil
	}
	m, _ := s.model(st.Model)
	spec := s.spec(m, st.Language)
	s.mu.Lock()
	if s.preparing || s.closed {
		s.mu.Unlock()
		return nil
	}
	s.preparing = true
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			s.preparing = false
			s.mu.Unlock()
		}()
		lctx, cancel := context.WithTimeout(s.ctx, TranscribeTimeout)
		defer cancel()
		s.recMu.Lock()
		defer s.recMu.Unlock()
		if err := s.rec.Load(lctx, spec); err != nil && s.ctx.Err() == nil {
			s.log.Warn("dictation model not preloaded", "model", m.ID, "err", err)
		}
	}()
	return nil
}

// Close stops the downloads and the speech engine.
func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
	s.recMu.Lock()
	defer s.recMu.Unlock()
	return s.rec.Close()
}

func (s *Service) closeRecognizer() {
	s.recMu.Lock()
	defer s.recMu.Unlock()
	if err := s.rec.Close(); err != nil {
		s.log.Warn("dictation engine close failed", "err", err)
	}
}

// publish sends the current status. Statuses go out in the order they were taken.
func (s *Service) publish() {
	if s.publishFn == nil {
		return
	}
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	s.last = time.Now()
	s.publishFn(s.Status(context.Background()))
}

// progress publishes download progress at most about four times a second.
func (s *Service) progress() {
	if s.publishFn == nil {
		return
	}
	s.pubMu.Lock()
	if time.Since(s.last) < 250*time.Millisecond {
		s.pubMu.Unlock()
		return
	}
	s.pubMu.Unlock()
	s.publish()
}

func (s *Service) audit(action, target, outcome, detail string) {
	if s.auditFn != nil {
		s.auditFn(action, target, outcome, detail)
	}
}

func (s *Service) dirSize(dir string) int64 {
	var n int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() {
			n += fi.Size()
		}
	}
	return n
}

// modelsLeft reports whether any catalog model is ready on disk.
func (s *Service) modelsLeft() bool {
	for _, m := range s.catalog {
		if s.modelReady(m) {
			return true
		}
	}
	return false
}

var errUnknownModel = Fail(CodeUnknown, "There is no such dictation model.")

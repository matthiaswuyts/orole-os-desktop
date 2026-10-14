package dictation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Disk layout under Config.Dir:
//
//	engine/<version>/<EngineBuild.DLLs>   the installed speech engine
//	models/<id>/<files>                   verified models
//	downloads/<id>/<file>.part            model downloads in progress (kept for resuming)
//	downloads/engine-<version>/           engine download and extraction staging

func (s *Service) engineDir() string { return filepath.Join(s.dir, "engine", s.engine.Version) }
func (s *Service) engineStaging() string {
	return filepath.Join(s.dir, "downloads", "engine-"+s.engine.Version)
}
func (s *Service) modelDir(id string) string    { return filepath.Join(s.dir, "models", id) }
func (s *Service) downloadDir(id string) string { return filepath.Join(s.dir, "downloads", id) }

// engineInstalled reports whether every engine DLL is on disk.
func (s *Service) engineInstalled() bool {
	for _, name := range s.engine.DLLs {
		fi, err := os.Stat(filepath.Join(s.engineDir(), name))
		if err != nil || !fi.Mode().IsRegular() {
			return false
		}
	}
	return len(s.engine.DLLs) > 0
}

// modelReady reports whether every file of m is in models/<id> with its pinned size. Hashes were checked when
// the files were downloaded; they are not re-read here.
func (s *Service) modelReady(m Model) bool {
	for _, f := range m.Files {
		fi, err := os.Stat(filepath.Join(s.modelDir(m.ID), f.Name))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != f.Size {
			return false
		}
	}
	return len(m.Files) > 0
}

// model finds id in this service's catalog.
func (s *Service) model(id string) (Model, bool) {
	for _, m := range s.catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// spec is what the engine loads for m.
func (s *Service) spec(m Model, language string) Spec {
	path := func(name string) string {
		if name == "" {
			return ""
		}
		return filepath.Join(s.modelDir(m.ID), name)
	}
	sp := Spec{
		EngineDir: s.engineDir(), Kind: m.Kind,
		Encoder: path(m.Encoder), Decoder: path(m.Decoder), Joiner: path(m.Joiner), Tokens: path(m.Tokens),
	}
	if m.LanguageChoice {
		sp.Language = language
	}
	return sp
}

// loadPrefs reads the stored preferences, dropping values this build does not know.
func (s *Service) loadPrefs(ctx context.Context) Preferences {
	raw, ok, err := s.settings.Get(ctx, SettingKey)
	if err != nil {
		s.log.Warn("dictation preferences unreadable", "err", err)
		return Preferences{}
	}
	var p Preferences
	if !ok || json.Unmarshal([]byte(raw), &p) != nil {
		return Preferences{}
	}
	if _, known := s.model(p.Model); !known {
		p.Model = ""
	}
	if !ValidLanguage(p.Language) {
		p.Language = ""
	}
	return p
}

// updatePrefs applies change to the preferences and stores them when change reports a difference.
func (s *Service) updatePrefs(ctx context.Context, change func(p *Preferences) bool) error {
	s.prefMu.Lock()
	defer s.prefMu.Unlock()
	s.mu.Lock()
	p := s.prefs
	s.mu.Unlock()
	if !change(&p) {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := s.settings.Set(ctx, SettingKey, string(b)); err != nil {
		s.log.Error("dictation preferences not saved", "err", err)
		return Fail(CodeEngine, "Couldn't save the voice settings. Try again.")
	}
	s.mu.Lock()
	s.prefs = p
	s.mu.Unlock()
	return nil
}

// sweepStale removes engine staging left by an interrupted run and engines of other versions.
func (s *Service) sweepStale() {
	if entries, err := os.ReadDir(filepath.Join(s.dir, "downloads")); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "engine-") {
				s.removeAll(filepath.Join(s.dir, "downloads", e.Name()))
			}
		}
	}
	if entries, err := os.ReadDir(filepath.Join(s.dir, "engine")); err == nil {
		for _, e := range entries {
			if e.Name() != s.engine.Version {
				s.removeAll(filepath.Join(s.dir, "engine", e.Name()))
			}
		}
	}
}

func (s *Service) removeAll(path string) error {
	err := os.RemoveAll(path)
	if err != nil {
		s.log.Warn("dictation cleanup failed", "path", path, "err", err)
	}
	return err
}
